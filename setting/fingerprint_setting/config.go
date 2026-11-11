// Package fingerprint_setting stores the outbound fingerprint profiles a
// channel can select. A channel that selects none keeps the stock Go transport.
package fingerprint_setting

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	MaxProfiles          = 32
	MaxProfileNameLength = dto.MaxFingerprintProfileNameLength
	maxHeaderOrderLength = 64
	maxUserAgentLength   = 512
	maxClientHelloLength = 32 * 1024

	maxHTTP2FingerprintLength = 512
	maxHTTP2Settings          = 16
	maxHTTP2Priorities        = 16
	// The transport sizes its HPACK decoder from the advertised table size.
	maxHTTP2HeaderTableSize = 64 * 1024
	// The transport keeps a fixed receive window of this size per stream, so a
	// larger INITIAL_WINDOW_SIZE would let the upstream overrun it.
	maxHTTP2StreamWindow = 4 << 20
	// Below the protocol default a stream can stall: the transport holds back
	// window updates smaller than 4 KiB.
	minHTTP2StreamWindow = 65535
	// The transport writes frames of at most the size it advertises.
	http2FrameSize = 16384
	// Smaller limits reject ordinary response headers.
	minHTTP2HeaderListSize = 16384
	// The connection window starts at 65535 and cannot pass 2^31-1.
	maxHTTP2WindowUpdate = 1<<31 - 1 - 65535
)

// Profile describes how the outbound requests of a channel look on the wire.
// Every part is optional; an empty part keeps the Go default for that part.
type Profile struct {
	Name string `json:"name"`
	// TLSPreset names a built-in ClientHello.
	TLSPreset string `json:"tls_preset,omitempty"`
	// TLSClientHello is a captured ClientHello in hex, either the whole TLS
	// record or the bare handshake message.
	TLSClientHello string `json:"tls_client_hello,omitempty"`
	// HeaderOrder lists header names in the order they are written. Headers it
	// does not name follow in the order Go writes them.
	HeaderOrder []string `json:"header_order,omitempty"`
	// UserAgent is sent when a request carries no User-Agent of its own.
	UserAgent string `json:"user_agent,omitempty"`
	// HTTP2Fingerprint is how an HTTP/2 connection opens, in the notation of
	// HTTP/2 fingerprint reports: SETTINGS|WINDOW_UPDATE|PRIORITY|pseudo-header
	// order, for example "1:65536;2:0;4:4194304;6:262144|15663105|0|m,a,s,p".
	HTTP2Fingerprint string `json:"http2_fingerprint,omitempty"`
	// HTTP2HeaderPriority is the priority every HEADERS frame carries, as
	// exclusive:dependency:weight, for example "1:0:256".
	HTTP2HeaderPriority string `json:"http2_header_priority,omitempty"`
}

// HTTP2Setting is one entry of the SETTINGS frame a connection opens with.
type HTTP2Setting struct {
	ID    uint16
	Value uint32
}

// HTTP2Priority is a stream priority. Weight is the value on the wire, one
// less than the weight a fingerprint shows.
type HTTP2Priority struct {
	StreamID   uint32
	Exclusive  bool
	Dependency uint32
	Weight     uint8
}

// HTTP2Preface is the parsed HTTP2Fingerprint of a profile.
type HTTP2Preface struct {
	Settings     []HTTP2Setting
	WindowUpdate uint32
	Priorities   []HTTP2Priority
	// PseudoHeaderOrder holds the four pseudo-header names in sending order.
	PseudoHeaderOrder []string
}

// Setting is managed by config.GlobalConfig.Register. Profiles stays a JSON
// string so the option system replaces it in one assignment; readers use the
// snapshot built by Refresh.
type Setting struct {
	// Enabled is a global kill switch. Off sends every channel back to the stock
	// Go transport whatever profile it selects.
	Enabled  bool   `json:"enabled"`
	Profiles string `json:"profiles"`
}

var setting = Setting{
	Enabled:  true,
	Profiles: "[]",
}

type snapshot struct {
	enabled   bool
	raw       string
	names     []string
	profiles  map[string]Profile
	revisions map[string]string
}

var (
	current        atomic.Pointer[snapshot]
	refreshMutex   sync.Mutex
	changeListener func()
)

func init() {
	config.GlobalConfig.Register("fingerprint_setting", &setting)
	Refresh()
}

// OnChange registers the function called after the enabled switch or any
// profile changes. The HTTP client cache uses it to drop clients built from an
// older revision.
func OnChange(listener func()) {
	refreshMutex.Lock()
	defer refreshMutex.Unlock()
	changeListener = listener
}

// Refresh rebuilds the read snapshot from the registered setting. The option
// system calls it after it updates a fingerprint_setting key.
func Refresh() {
	refreshMutex.Lock()
	defer refreshMutex.Unlock()
	old := current.Load()
	if old != nil && old.enabled == setting.Enabled && old.raw == setting.Profiles {
		return
	}
	next := &snapshot{
		enabled:   setting.Enabled,
		raw:       setting.Profiles,
		profiles:  make(map[string]Profile),
		revisions: make(map[string]string),
	}
	profiles, err := ParseProfiles(setting.Profiles)
	if err != nil {
		// Saving validates the value, so this only happens to a row edited by
		// hand. Channels that select a profile then fail closed.
		common.SysError(common.LogText("invalid fingerprint profiles: %s", err.Error()))
	}
	for _, profile := range profiles {
		encoded, err := common.Marshal(profile)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(encoded)
		next.names = append(next.names, profile.Name)
		next.profiles[profile.Name] = profile
		next.revisions[profile.Name] = hex.EncodeToString(sum[:6])
	}
	current.Store(next)
	if old != nil && changeListener != nil {
		changeListener()
	}
}

// IsEnabled reports the global kill switch.
func IsEnabled() bool {
	return current.Load().enabled
}

// Names lists the stored profile names in the order they were saved.
func Names() []string {
	return slices.Clone(current.Load().names)
}

// Lookup returns the profile stored under name.
func Lookup(name string) (Profile, bool) {
	profile, ok := current.Load().profiles[name]
	return profile, ok
}

// Revision identifies the content of the profile stored under name. It is
// empty when no such profile exists.
func Revision(name string) string {
	return current.Load().revisions[name]
}

// ValidProfileName reports whether name is usable as a profile name. The same
// rule is applied to the channel setting that selects a profile.
func ValidProfileName(name string) bool {
	if name == "" || len(name) > MaxProfileNameLength {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// ParseProfiles decodes and validates the stored profile list. It checks the
// structure only; the ClientHello itself is validated where it is built.
func ParseProfiles(raw string) ([]Profile, error) {
	var profiles []Profile
	if strings.TrimSpace(raw) == "" {
		return profiles, nil
	}
	if err := common.UnmarshalJsonStr(raw, &profiles); err != nil {
		return nil, fmt.Errorf("profiles must be a JSON array of fingerprint profiles")
	}
	if len(profiles) > MaxProfiles {
		return nil, fmt.Errorf("at most %d fingerprint profiles are allowed", MaxProfiles)
	}
	names := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		if !ValidProfileName(profile.Name) {
			return nil, fmt.Errorf("fingerprint profile name %q must be 1-%d letters, digits, dots, underscores or hyphens", profile.Name, MaxProfileNameLength)
		}
		if _, ok := names[profile.Name]; ok {
			return nil, fmt.Errorf("fingerprint profile name %q is used more than once", profile.Name)
		}
		names[profile.Name] = struct{}{}
		if profile.TLSPreset != "" && profile.TLSClientHello != "" {
			return nil, fmt.Errorf("fingerprint profile %q sets both tls_preset and tls_client_hello", profile.Name)
		}
		if len(profile.TLSClientHello) > maxClientHelloLength {
			return nil, fmt.Errorf("fingerprint profile %q: tls_client_hello is too long", profile.Name)
		}
		if len(profile.HeaderOrder) > maxHeaderOrderLength {
			return nil, fmt.Errorf("fingerprint profile %q: header_order lists more than %d headers", profile.Name, maxHeaderOrderLength)
		}
		headers := make(map[string]struct{}, len(profile.HeaderOrder))
		for _, header := range profile.HeaderOrder {
			if !validHeaderName(header) {
				return nil, fmt.Errorf("fingerprint profile %q: %q is not a header name", profile.Name, header)
			}
			canonical := textproto.CanonicalMIMEHeaderKey(header)
			if _, ok := headers[canonical]; ok {
				return nil, fmt.Errorf("fingerprint profile %q: header_order names %q more than once", profile.Name, header)
			}
			headers[canonical] = struct{}{}
		}
		if len(profile.UserAgent) > maxUserAgentLength {
			return nil, fmt.Errorf("fingerprint profile %q: user_agent is longer than %d bytes", profile.Name, maxUserAgentLength)
		}
		for _, r := range profile.UserAgent {
			if r < 0x20 || r == 0x7f {
				return nil, fmt.Errorf("fingerprint profile %q: user_agent contains a control character", profile.Name)
			}
		}
		if profile.HTTP2Fingerprint != "" {
			if _, err := ParseHTTP2Fingerprint(profile.HTTP2Fingerprint); err != nil {
				return nil, fmt.Errorf("fingerprint profile %q: %w", profile.Name, err)
			}
		}
		if profile.HTTP2HeaderPriority != "" {
			if _, err := ParseHTTP2HeaderPriority(profile.HTTP2HeaderPriority); err != nil {
				return nil, fmt.Errorf("fingerprint profile %q: %w", profile.Name, err)
			}
		}
	}
	return profiles, nil
}

// ParseHTTP2Fingerprint decodes an HTTP2Fingerprint and rejects the values the
// transport cannot honour, so a saved profile never breaks a connection.
func ParseHTTP2Fingerprint(raw string) (*HTTP2Preface, error) {
	if len(raw) > maxHTTP2FingerprintLength {
		return nil, fmt.Errorf("http2_fingerprint is too long")
	}
	parts := strings.Split(raw, "|")
	if len(parts) != 4 {
		return nil, fmt.Errorf("http2_fingerprint must be SETTINGS|WINDOW_UPDATE|PRIORITY|pseudo-header order")
	}
	preface := &HTTP2Preface{}

	for entry := range strings.SplitSeq(strings.ReplaceAll(parts[0], ",", ";"), ";") {
		rawID, rawValue, _ := strings.Cut(entry, ":")
		id, idErr := strconv.ParseUint(strings.TrimSpace(rawID), 10, 16)
		value, valueErr := strconv.ParseUint(strings.TrimSpace(rawValue), 10, 32)
		if idErr != nil || valueErr != nil || id == 0 {
			return nil, fmt.Errorf("http2_fingerprint: %q is not a setting written as id:value", entry)
		}
		if slices.ContainsFunc(preface.Settings, func(setting HTTP2Setting) bool { return setting.ID == uint16(id) }) {
			return nil, fmt.Errorf("http2_fingerprint: setting %d appears more than once", id)
		}
		switch {
		case id == 1 && value > maxHTTP2HeaderTableSize:
			return nil, fmt.Errorf("http2_fingerprint: setting 1 (HEADER_TABLE_SIZE) must be at most %d", maxHTTP2HeaderTableSize)
		case id == 2 && value != 0:
			return nil, fmt.Errorf("http2_fingerprint: setting 2 (ENABLE_PUSH) must be 0, server push is not supported")
		case id == 4 && (value < minHTTP2StreamWindow || value > maxHTTP2StreamWindow):
			return nil, fmt.Errorf("http2_fingerprint: setting 4 (INITIAL_WINDOW_SIZE) must be between %d and %d", minHTTP2StreamWindow, maxHTTP2StreamWindow)
		case id == 5 && value != http2FrameSize:
			return nil, fmt.Errorf("http2_fingerprint: setting 5 (MAX_FRAME_SIZE) must be %d", http2FrameSize)
		case id == 6 && value < minHTTP2HeaderListSize:
			return nil, fmt.Errorf("http2_fingerprint: setting 6 (MAX_HEADER_LIST_SIZE) must be at least %d", minHTTP2HeaderListSize)
		}
		preface.Settings = append(preface.Settings, HTTP2Setting{ID: uint16(id), Value: uint32(value)})
	}
	if len(preface.Settings) > maxHTTP2Settings {
		return nil, fmt.Errorf("http2_fingerprint lists more than %d settings", maxHTTP2Settings)
	}

	windowUpdate, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
	if err != nil || windowUpdate < 1 || windowUpdate > maxHTTP2WindowUpdate {
		return nil, fmt.Errorf("http2_fingerprint: WINDOW_UPDATE must be between 1 and %d, a connection without one cannot be reproduced", maxHTTP2WindowUpdate)
	}
	preface.WindowUpdate = uint32(windowUpdate)

	if priorities := strings.TrimSpace(parts[2]); priorities != "0" && priorities != "" {
		for entry := range strings.SplitSeq(priorities, ",") {
			rawStream, rest, _ := strings.Cut(entry, ":")
			stream, streamErr := strconv.ParseUint(strings.TrimSpace(rawStream), 10, 31)
			priority, priorityErr := ParseHTTP2HeaderPriority(rest)
			if streamErr != nil || priorityErr != nil {
				return nil, fmt.Errorf("http2_fingerprint: %q is not a priority written as stream:exclusive:dependency:weight", entry)
			}
			priority.StreamID = uint32(stream)
			previous := uint32(0)
			if len(preface.Priorities) > 0 {
				previous = preface.Priorities[len(preface.Priorities)-1].StreamID
			}
			if priority.StreamID%2 == 0 || priority.StreamID <= previous || priority.StreamID > 255 {
				return nil, fmt.Errorf("http2_fingerprint: priority streams must be odd, increasing and at most 255")
			}
			if priority.Dependency == priority.StreamID {
				return nil, fmt.Errorf("http2_fingerprint: priority stream %d depends on itself", priority.StreamID)
			}
			preface.Priorities = append(preface.Priorities, priority)
		}
		if len(preface.Priorities) > maxHTTP2Priorities {
			return nil, fmt.Errorf("http2_fingerprint lists more than %d priorities", maxHTTP2Priorities)
		}
	}

	names := map[string]string{"m": ":method", "a": ":authority", "s": ":scheme", "p": ":path"}
	for letter := range strings.SplitSeq(parts[3], ",") {
		name, ok := names[strings.TrimSpace(letter)]
		if !ok || slices.Contains(preface.PseudoHeaderOrder, name) {
			return nil, fmt.Errorf("http2_fingerprint: the pseudo-header order must name m, a, s and p once each")
		}
		preface.PseudoHeaderOrder = append(preface.PseudoHeaderOrder, name)
	}
	if len(preface.PseudoHeaderOrder) != len(names) {
		return nil, fmt.Errorf("http2_fingerprint: the pseudo-header order must name m, a, s and p once each")
	}
	return preface, nil
}

// ParseHTTP2HeaderPriority decodes a priority written as
// exclusive:dependency:weight, with the weight between 1 and 256.
func ParseHTTP2HeaderPriority(raw string) (HTTP2Priority, error) {
	fields := strings.Split(raw, ":")
	if len(fields) != 3 {
		return HTTP2Priority{}, fmt.Errorf("http2_header_priority must be exclusive:dependency:weight")
	}
	exclusive, exclusiveErr := strconv.ParseUint(strings.TrimSpace(fields[0]), 10, 1)
	dependency, dependencyErr := strconv.ParseUint(strings.TrimSpace(fields[1]), 10, 31)
	weight, weightErr := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 16)
	if exclusiveErr != nil || dependencyErr != nil || weightErr != nil || weight < 1 || weight > 256 {
		return HTTP2Priority{}, fmt.Errorf("http2_header_priority must be exclusive (0 or 1), a stream id and a weight between 1 and 256")
	}
	return HTTP2Priority{Exclusive: exclusive == 1, Dependency: uint32(dependency), Weight: uint8(weight - 1)}, nil
}

// validHeaderName reports whether name is an RFC 9110 token.
func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}
