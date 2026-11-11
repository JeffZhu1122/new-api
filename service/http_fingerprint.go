package service

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/fingerprint_setting"

	"github.com/imroc/req/v3"
	"github.com/imroc/req/v3/http2"
	utls "github.com/refraction-networking/utls"
)

// fingerprintCacheKeyMark separates the fingerprint part of a client cache key
// from the transport policy part. Profile names cannot contain "|".
const fingerprintCacheKeyMark = "|fp|"

// The User-Agent values net/http sends when a request carries none. A
// fingerprint client sets one of them explicitly, because the transport it is
// built on would otherwise announce its own library name.
const (
	goHTTP1UserAgent = "Go-http-client/1.1"
	goHTTP2UserAgent = "Go-http-client/2.0"
)

// fingerprintTLSPresets maps the tls_preset of a profile to a built-in
// ClientHello. Randomized presets are left out: their fingerprint changes on
// every connection. So are presets that stop at TLS 1.2.
var fingerprintTLSPresets = map[string]utls.ClientHelloID{
	"chrome_120":    utls.HelloChrome_120,
	"chrome_120_pq": utls.HelloChrome_120_PQ,
	"chrome_131":    utls.HelloChrome_131,
	"chrome_133":    utls.HelloChrome_133,
	"firefox_120":   utls.HelloFirefox_120,
	"edge_106":      utls.HelloEdge_106,
	"safari_16_0":   utls.HelloSafari_16_0,
	"ios_14":        utls.HelloIOS_14,
}

func init() {
	fingerprint_setting.OnChange(func() {
		for _, client := range proxyClients.removeStaleFingerprintClients() {
			client.CloseIdleConnections()
		}
	})
}

// removeStaleFingerprintClients drops every cached client whose fingerprint
// profile was edited, removed, or switched off globally since it was built.
func (cache *proxyHTTPClientCache) removeStaleFingerprintClients() []*http.Client {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	var removed []*http.Client
	for key, client := range cache.clients {
		policyPart := key[strings.LastIndexByte(key, 0)+1:]
		_, fingerprint, ok := strings.Cut(policyPart, fingerprintCacheKeyMark)
		if !ok {
			continue
		}
		name, revision, _ := strings.Cut(fingerprint, "|")
		if fingerprint_setting.IsEnabled() && fingerprint_setting.Revision(name) == revision {
			continue
		}
		removed = append(removed, client)
		delete(cache.clients, key)
	}
	return removed
}

// fingerprintTLS performs TLS handshakes whose ClientHello follows one
// fingerprint profile.
type fingerprintTLS struct {
	profile string
	preset  utls.ClientHelloID
	// hello is a captured ClientHello record; when set it replaces preset.
	hello []byte
	// http1Only offers only HTTP/1.1 in ALPN, for channels forced to HTTP/1.1.
	http1Only bool
	// offersHTTP2 reports whether the ClientHello offers h2 in ALPN.
	offersHTTP2 bool
	// verification carries the certificate verification options of the channel.
	verification *tls.Config
}

// newFingerprintTLS builds the TLS part of a profile and proves that its
// ClientHello can be generated. It returns nil when the profile has no TLS part.
func newFingerprintTLS(profile fingerprint_setting.Profile, http1Only bool, verification *tls.Config) (*fingerprintTLS, error) {
	if profile.TLSPreset == "" && profile.TLSClientHello == "" {
		return nil, nil
	}
	fingerprint := &fingerprintTLS{profile: profile.Name, http1Only: http1Only, verification: verification}
	if profile.TLSPreset != "" {
		preset, ok := fingerprintTLSPresets[profile.TLSPreset]
		if !ok {
			names := make([]string, 0, len(fingerprintTLSPresets))
			for name := range fingerprintTLSPresets {
				names = append(names, name)
			}
			slices.Sort(names)
			return nil, fmt.Errorf("fingerprint profile %q: unknown tls_preset %q (available: %s)", profile.Name, profile.TLSPreset, strings.Join(names, ", "))
		}
		fingerprint.preset = preset
	} else {
		compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", ":", "").Replace(profile.TLSClientHello)
		hello, err := hex.DecodeString(compact)
		if err != nil || len(hello) == 0 {
			return nil, fmt.Errorf("fingerprint profile %q: tls_client_hello must be the hex dump of a ClientHello", profile.Name)
		}
		// A capture may start at the handshake message (type 0x01) instead of
		// the TLS record (type 0x16); the parser needs the record header.
		if hello[0] == 0x01 {
			if len(hello) > 16384 {
				return nil, fmt.Errorf("fingerprint profile %q: tls_client_hello is larger than one TLS record", profile.Name)
			}
			hello = append([]byte{0x16, 0x03, 0x01, byte(len(hello) >> 8), byte(len(hello))}, hello...)
		}
		fingerprint.hello = hello
	}
	spec, err := fingerprint.spec()
	if err != nil {
		return nil, err
	}
	for _, extension := range spec.Extensions {
		if alpn, ok := extension.(*utls.ALPNExtension); ok && slices.Contains(alpn.AlpnProtocols, "h2") {
			fingerprint.offersHTTP2 = true
		}
	}
	probe := utls.UClient(nil, &utls.Config{ServerName: "fingerprint.invalid"}, utls.HelloCustom)
	if err := probe.ApplyPreset(spec); err != nil {
		return nil, fmt.Errorf("fingerprint profile %q: the ClientHello cannot be reproduced: %w", profile.Name, err)
	}
	if err := probe.BuildHandshakeState(); err != nil {
		return nil, fmt.Errorf("fingerprint profile %q: the ClientHello cannot be reproduced: %w", profile.Name, err)
	}
	return fingerprint, nil
}

// spec returns a fresh ClientHello description. A spec holds per-connection
// state such as key shares, so it must never be shared between connections.
func (f *fingerprintTLS) spec() (*utls.ClientHelloSpec, error) {
	var spec *utls.ClientHelloSpec
	if len(f.hello) > 0 {
		// Blunt mimicry keeps extensions the TLS library does not implement,
		// which OpenSSL-based clients such as Node.js and Python send.
		parsed, err := (&utls.Fingerprinter{AllowBluntMimicry: true}).RawClientHello(f.hello)
		if err != nil {
			return nil, fmt.Errorf("fingerprint profile %q: tls_client_hello cannot be parsed: %w", f.profile, err)
		}
		spec = parsed
	} else {
		preset, err := utls.UTLSIdToSpec(f.preset)
		if err != nil {
			return nil, fmt.Errorf("fingerprint profile %q: %w", f.profile, err)
		}
		spec = &preset
	}
	for _, extension := range spec.Extensions {
		switch extension := extension.(type) {
		case *utls.ALPNExtension:
			if f.http1Only {
				extension.AlpnProtocols = []string{"http/1.1"}
			}
		case utls.PreSharedKeyExtension:
			return nil, fmt.Errorf("fingerprint profile %q: the captured ClientHello resumes an earlier session; capture the first connection of a client instead", f.profile)
		}
	}
	return spec, nil
}

// handshake is the TLS handshake hook of the fingerprint transport. host is
// the upstream host name without a port.
func (f *fingerprintTLS) handshake(ctx context.Context, host string, plainConn net.Conn) (net.Conn, *tls.ConnectionState, error) {
	spec, err := f.spec()
	if err != nil {
		return nil, nil, err
	}
	config := &utls.Config{ServerName: host}
	if f.verification != nil {
		config.RootCAs = f.verification.RootCAs
		config.InsecureSkipVerify = f.verification.InsecureSkipVerify
		if f.verification.ServerName != "" {
			config.ServerName = f.verification.ServerName
		}
	}
	conn := utls.UClient(plainConn, config, utls.HelloCustom)
	if err := conn.ApplyPreset(spec); err != nil {
		return nil, nil, fmt.Errorf("fingerprint profile %q: %w", f.profile, err)
	}
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil, nil, fmt.Errorf("fingerprint profile %q: TLS handshake with %s failed: %w", f.profile, host, err)
	}
	state := conn.ConnectionState()
	return conn, &tls.ConnectionState{
		Version:                     state.Version,
		HandshakeComplete:           state.HandshakeComplete,
		DidResume:                   state.DidResume,
		CipherSuite:                 state.CipherSuite,
		NegotiatedProtocol:          state.NegotiatedProtocol,
		NegotiatedProtocolIsMutual:  state.NegotiatedProtocolIsMutual,
		ServerName:                  state.ServerName,
		PeerCertificates:            state.PeerCertificates,
		VerifiedChains:              state.VerifiedChains,
		SignedCertificateTimestamps: state.SignedCertificateTimestamps,
		OCSPResponse:                state.OCSPResponse,
		TLSUnique:                   state.TLSUnique,
	}, nil
}

// fingerprintRoundTripper sends requests through a transport whose TLS
// handshake follows a fingerprint profile, and lays out the request headers
// the way the profile asks.
type fingerprintRoundTripper struct {
	transport *req.Transport
	// userAgent is the User-Agent of the profile, sent when a request carries
	// none. Empty announces what net/http would.
	userAgent string
	// http1Only is set when no connection of this transport can speak HTTP/2.
	http1Only bool
	// headerOrder holds canonical header names: the order of the profile
	// followed by the headers the transport adds itself. Empty keeps Go's order.
	headerOrder []string
	// pseudoHeaderOrder holds the HTTP/2 pseudo-header names in sending order.
	// Empty keeps Go's order.
	pseudoHeaderOrder []string
}

func (f *fingerprintRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	// A RoundTripper must not modify the request it is given.
	outbound := request.Clone(request.Context())
	if outbound.Header == nil {
		outbound.Header = make(http.Header)
	}
	// These keys steer the header layout of the transport and never reach the
	// wire; a client must not be able to set them through a forwarded header.
	delete(outbound.Header, req.HeaderOderKey)
	delete(outbound.Header, req.PseudoHeaderOderKey)

	dropEmptyRelayHeaders(outbound.Header)

	hasUserAgent := false
	names := make([]string, 0, len(outbound.Header)+2)
	for name := range outbound.Header {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if canonical == "User-Agent" {
			hasUserAgent = true
		}
		if !slices.Contains(f.headerOrder, canonical) {
			names = append(names, canonical)
		}
	}
	if !hasUserAgent {
		userAgent := f.userAgent
		if userAgent == "" {
			// net/http names the protocol version; plain HTTP is always HTTP/1.1.
			userAgent = goHTTP2UserAgent
			if f.http1Only || outbound.URL.Scheme != "https" {
				userAgent = goHTTP1UserAgent
			}
		}
		outbound.Header.Set("User-Agent", userAgent)
	}
	if len(f.headerOrder) > 0 {
		// The transport only orders the headers it is told about, so name every
		// header: the ones the profile leaves out follow in alphabetical order,
		// which is how Go writes them, and the gzip offer comes last.
		slices.Sort(names)
		if !slices.Contains(f.headerOrder, "Accept-Encoding") && !slices.Contains(names, "Accept-Encoding") {
			names = append(names, "Accept-Encoding")
		}
		outbound.Header[req.HeaderOderKey] = append(slices.Clone(f.headerOrder), slices.Compact(names)...)
	}
	if len(f.pseudoHeaderOrder) > 0 {
		outbound.Header[req.PseudoHeaderOderKey] = f.pseudoHeaderOrder
	}
	return f.transport.RoundTrip(outbound)
}

func (f *fingerprintRoundTripper) CloseIdleConnections() {
	f.transport.CloseIdleConnections()
}

// newFingerprintHTTPClient builds the client of a channel that selected a
// fingerprint profile. Every connection setting is taken from the stock relay
// transport, so only the fingerprint differs from a channel without a profile.
// It fails instead of falling back when the profile is missing or broken.
func newFingerprintHTTPClient(policy HTTPTransportPolicy, proxyURL *url.URL, tlsConfig *tls.Config) (*http.Client, error) {
	profile, ok := fingerprint_setting.Lookup(policy.Fingerprint)
	if !ok {
		return nil, fmt.Errorf("fingerprint profile %q does not exist", policy.Fingerprint)
	}
	http1Only := policy.Protocol == dto.HTTPProtocolHTTP1

	base := newRelayHTTPTransport()
	if proxyURL != nil {
		if err := configureProxyTransport(base, proxyURL); err != nil {
			return nil, err
		}
	} else {
		base.Proxy = http.ProxyFromEnvironment
	}
	if tlsConfig != nil {
		base.TLSClientConfig = tlsConfig.Clone()
	}
	tlsFingerprint, err := newFingerprintTLS(profile, http1Only, base.TLSClientConfig)
	if err != nil {
		return nil, err
	}

	// This config serves the profiles without a TLS part and the TLS session to
	// an HTTPS proxy. Its ALPN list is the one net/http offers.
	standardTLS := &tls.Config{}
	if base.TLSClientConfig != nil {
		standardTLS = base.TLSClientConfig.Clone()
	}
	standardTLS.NextProtos = []string{"h2", "http/1.1"}

	// A captured ClientHello that does not offer h2 keeps every connection on HTTP/1.1.
	noHTTP2 := http1Only || (tlsFingerprint != nil && !tlsFingerprint.offersHTTP2)
	var headerOrder []string
	if len(profile.HeaderOrder) > 0 {
		for _, name := range profile.HeaderOrder {
			headerOrder = append(headerOrder, textproto.CanonicalMIMEHeaderKey(name))
		}
		for _, name := range []string{"Host", "User-Agent", "Connection", "Content-Length", "Transfer-Encoding", "Trailer"} {
			if !slices.Contains(headerOrder, name) {
				headerOrder = append(headerOrder, name)
			}
		}
	}

	var (
		pseudoHeaderOrder []string
		http2Settings     []http2.Setting
		http2Priorities   []http2.PriorityFrame
		http2WindowUpdate uint32
		headerPriority    http2.PriorityParam
	)
	if !http1Only && profile.HTTP2Fingerprint != "" {
		preface, err := fingerprint_setting.ParseHTTP2Fingerprint(profile.HTTP2Fingerprint)
		if err != nil {
			return nil, fmt.Errorf("fingerprint profile %q: %w", profile.Name, err)
		}
		pseudoHeaderOrder = preface.PseudoHeaderOrder
		http2WindowUpdate = preface.WindowUpdate
		for _, setting := range preface.Settings {
			http2Settings = append(http2Settings, http2.Setting{ID: http2.SettingID(setting.ID), Val: setting.Value})
		}
		for _, priority := range preface.Priorities {
			http2Priorities = append(http2Priorities, http2.PriorityFrame{
				StreamID:      priority.StreamID,
				PriorityParam: http2.PriorityParam{StreamDep: priority.Dependency, Exclusive: priority.Exclusive, Weight: priority.Weight},
			})
		}
	}
	if !http1Only && profile.HTTP2HeaderPriority != "" {
		priority, err := fingerprint_setting.ParseHTTP2HeaderPriority(profile.HTTP2HeaderPriority)
		if err != nil {
			return nil, fmt.Errorf("fingerprint profile %q: %w", profile.Name, err)
		}
		headerPriority = http2.PriorityParam{StreamDep: priority.Dependency, Exclusive: priority.Exclusive, Weight: priority.Weight}
	}

	shards := 1
	if !http1Only {
		shards = max(policy.Shards, 1)
	}
	roundTrippers := make([]http.RoundTripper, shards)
	for i := range shards {
		transport := req.NewTransport()
		transport.Proxy = base.Proxy
		transport.OnProxyConnectResponse = base.OnProxyConnectResponse
		transport.ProxyConnectHeader = base.ProxyConnectHeader
		transport.GetProxyConnectHeader = base.GetProxyConnectHeader
		transport.DialContext = base.DialContext
		transport.TLSClientConfig = standardTLS.Clone()
		transport.TLSHandshakeTimeout = base.TLSHandshakeTimeout
		transport.DisableKeepAlives = base.DisableKeepAlives
		transport.DisableCompression = base.DisableCompression
		transport.MaxIdleConns = base.MaxIdleConns
		transport.MaxIdleConnsPerHost = base.MaxIdleConnsPerHost
		if shards > 1 {
			// Same split as the stock sharded transport.
			transport.MaxIdleConns = max(1, base.MaxIdleConns/shards)
			transport.MaxIdleConnsPerHost = max(1, base.MaxIdleConnsPerHost/shards)
		}
		transport.MaxConnsPerHost = base.MaxConnsPerHost
		transport.IdleConnTimeout = base.IdleConnTimeout
		transport.ResponseHeaderTimeout = base.ResponseHeaderTimeout
		transport.ExpectContinueTimeout = base.ExpectContinueTimeout
		transport.MaxResponseHeaderBytes = base.MaxResponseHeaderBytes
		transport.WriteBufferSize = base.WriteBufferSize
		transport.ReadBufferSize = base.ReadBufferSize
		if base.HTTP2 != nil {
			transport.SetHTTP2ReadIdleTimeout(base.HTTP2.SendPingTimeout)
			transport.SetHTTP2PingTimeout(base.HTTP2.PingTimeout)
		}
		// net/http hands the response body over untouched; keep it that way
		// instead of transcoding non-UTF-8 bodies.
		transport.DisableAutoDecode()
		if http1Only {
			transport.EnableForceHTTP1()
		}
		if tlsFingerprint != nil {
			transport.SetTLSHandshake(tlsFingerprint.handshake)
		}
		if len(http2Settings) > 0 {
			transport.SetHTTP2SettingsFrame(http2Settings...)
			transport.SetHTTP2ConnectionFlow(http2WindowUpdate)
			transport.SetHTTP2PriorityFrames(http2Priorities...)
		}
		transport.SetHTTP2HeaderPriority(headerPriority)
		roundTrippers[i] = &fingerprintRoundTripper{transport: transport, userAgent: profile.UserAgent, http1Only: noHTTP2, headerOrder: headerOrder, pseudoHeaderOrder: pseudoHeaderOrder}
	}
	if shards == 1 {
		return newRelayHTTPClient(roundTrippers[0]), nil
	}
	return newRelayHTTPClient(&shardedRoundTripper{shards: roundTrippers, n: uint32(shards), policy: policy}), nil
}

// FingerprintProfileOptions is what the channel editor and the profile editor
// offer for selection. It holds names only, never the content of a profile.
type FingerprintProfileOptions struct {
	Enabled    bool     `json:"enabled"`
	Profiles   []string `json:"profiles"`
	TLSPresets []string `json:"tls_presets"`
}

func GetFingerprintProfileOptions() FingerprintProfileOptions {
	profiles := fingerprint_setting.Names()
	if profiles == nil {
		profiles = []string{}
	}
	return FingerprintProfileOptions{
		Enabled:    fingerprint_setting.IsEnabled(),
		Profiles:   profiles,
		TLSPresets: slices.Sorted(maps.Keys(fingerprintTLSPresets)),
	}
}

// ValidateFingerprintOption checks a fingerprint_setting option before it is
// saved: every profile must be reproducible, and a profile a channel still
// selects cannot be removed.
func ValidateFingerprintOption(key string, value string) error {
	switch strings.TrimPrefix(key, "fingerprint_setting.") {
	case "enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("fingerprint_setting.enabled must be true or false")
		}
		return nil
	case "profiles":
		profiles, err := fingerprint_setting.ParseProfiles(value)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(profiles))
		for _, profile := range profiles {
			if _, err := newFingerprintTLS(profile, false, nil); err != nil {
				return err
			}
			names = append(names, profile.Name)
		}
		channels, err := model.GetAllChannels(0, 0, true, false)
		if err != nil {
			return err
		}
		for _, channel := range channels {
			selected := channel.GetSetting().FingerprintProfile
			if selected != "" && !slices.Contains(names, selected) {
				return fmt.Errorf("fingerprint profile %q is still selected by channel #%d (%s); clear it on the channel first", selected, channel.Id, channel.Name)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown fingerprint setting: %s", key)
	}
}
