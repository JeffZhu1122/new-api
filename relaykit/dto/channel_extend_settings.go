package dto

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
)

// MaxChannelTimeoutSeconds bounds per-channel timeout overrides (24h).
const MaxChannelTimeoutSeconds = 86400

// MaxChannelRateLimitValue bounds the per-channel rpm/tpm limits.
const MaxChannelRateLimitValue = math.MaxInt32

// MaxChannelInputTokensBound bounds the per-channel min/max input thresholds.
const MaxChannelInputTokensBound = 10_000_000

// MaxChannelMinInputTokens bounds the per-channel minimum input threshold.
//
// Deprecated: use MaxChannelInputTokensBound, which covers both the minimum
// and maximum thresholds. Kept as an alias for relaykit API compatibility.
const MaxChannelMinInputTokens = MaxChannelInputTokensBound

// Anthropic channel credential modes (ChannelExtendSettings.ClaudeAuthMode).
// Standard API keys (sk-ant-api…) authenticate with x-api-key; organization
// OAuth access tokens (sk-ant-oat…) must use Authorization: Bearer plus the
// oauth anthropic-beta flag. Using the wrong scheme is always rejected.
const (
	ClaudeAuthModeApiKey = "api_key" // x-api-key (default, "" behaves the same)
	ClaudeAuthModeOAuth  = "oauth"   // Authorization: Bearer + anthropic-beta oauth
	ClaudeAuthModeAuto   = "auto"    // pick per key by its sk-ant-oat prefix
)

// ClaudeOAuthTokenPrefix identifies Anthropic OAuth access tokens.
const ClaudeOAuthTokenPrefix = "sk-ant-oat"

// Response header filter modes (ChannelExtendSettings.ResponseHeaderMode).
// They decide which upstream response headers are copied back to the client.
const (
	ResponseHeaderModeBlacklist = "blacklist" // drop the listed headers, copy everything else
	ResponseHeaderModeWhitelist = "whitelist" // copy only the listed headers
)

// MaxChannelResponseHeaderRules bounds the number of header names in a
// per-channel response header filter.
const MaxChannelResponseHeaderRules = 64

// MaxChannelResponseHeaderNameLength bounds a single header name in a
// per-channel response header filter.
const MaxChannelResponseHeaderNameLength = 128

// MaxChannelCostRatio bounds the per-channel cost ratio: how many USD the
// operator pays upstream for every $1 of quota billed through the channel.
const MaxChannelCostRatio = 1000

// MaxChannelQuotaLimit bounds the per-channel quota limit in quota units. It
// is the JavaScript-safe integer boundary so the value survives the web UI.
const MaxChannelQuotaLimit = 1<<53 - 1

// MaxChannelScheduleWindows bounds the number of weekly windows in a
// per-channel availability schedule.
const MaxChannelScheduleWindows = 16

// ChannelScheduleWindow is one weekly availability window. Start is
// inclusive and End exclusive, both "HH:MM" in the schedule's timezone. An
// End at or before Start crosses midnight and belongs to the day it starts on.
type ChannelScheduleWindow struct {
	// Days lists the weekdays the window starts on (0 = Sunday … 6 =
	// Saturday). Empty means every day.
	Days  []int  `json:"days,omitempty"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// ChannelSchedule restricts when a channel takes part in channel selection.
// Outside every window the channel is skipped as if it did not serve the
// model; its status is left untouched and it rejoins once a window opens.
type ChannelSchedule struct {
	// Timezone is the IANA zone the windows are expressed in.
	Timezone string                  `json:"timezone"`
	Windows  []ChannelScheduleWindow `json:"windows"`
}

var scheduleLocations sync.Map // timezone name -> *time.Location

// loadScheduleLocation resolves an IANA timezone once per process; selection
// evaluates schedules per candidate channel, so re-parsing tzdata on every
// call would be wasteful.
func loadScheduleLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if cached, ok := scheduleLocations.Load(name); ok {
		return cached.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	scheduleLocations.Store(name, loc)
	return loc, nil
}

// parseClockMinutes converts a strict "HH:MM" clock value to minutes since
// midnight.
func parseClockMinutes(value string) (int, bool) {
	hourText, minuteText, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || len(hourText) != 2 || len(minuteText) != 2 {
		return 0, false
	}
	for _, r := range hourText + minuteText {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	hour := int(hourText[0]-'0')*10 + int(hourText[1]-'0')
	minute := int(minuteText[0]-'0')*10 + int(minuteText[1]-'0')
	if hour > 23 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

func (s *ChannelSchedule) Validate() error {
	if s == nil {
		return nil
	}
	if strings.TrimSpace(s.Timezone) == "" {
		return errors.New("schedule timezone is required")
	}
	if _, err := loadScheduleLocation(s.Timezone); err != nil {
		return fmt.Errorf("invalid schedule timezone %q", s.Timezone)
	}
	if len(s.Windows) == 0 {
		return errors.New("schedule must list at least one window")
	}
	if len(s.Windows) > MaxChannelScheduleWindows {
		return fmt.Errorf("too many schedule windows: %d, at most %d", len(s.Windows), MaxChannelScheduleWindows)
	}
	for i, window := range s.Windows {
		start, ok := parseClockMinutes(window.Start)
		if !ok {
			return fmt.Errorf("invalid schedule window %d start %q: expected HH:MM", i+1, window.Start)
		}
		end, ok := parseClockMinutes(window.End)
		if !ok {
			return fmt.Errorf("invalid schedule window %d end %q: expected HH:MM", i+1, window.End)
		}
		if start == end {
			return fmt.Errorf("schedule window %d start and end must differ", i+1)
		}
		seen := make(map[int]struct{}, len(window.Days))
		for _, day := range window.Days {
			if day < 0 || day > 6 {
				return fmt.Errorf("invalid schedule window %d weekday %d: must be within [0, 6]", i+1, day)
			}
			if _, dup := seen[day]; dup {
				return fmt.Errorf("duplicate schedule window %d weekday %d", i+1, day)
			}
			seen[day] = struct{}{}
		}
	}
	return nil
}

func (w ChannelScheduleWindow) startsOn(weekday int) bool {
	return len(w.Days) == 0 || slices.Contains(w.Days, weekday)
}

// Contains reports whether the instant falls inside any window, evaluated in
// the schedule's timezone. A nil schedule or one without windows never
// restricts. An unloadable timezone also fails open: Validate rejects unknown
// zones on save, so this only happens when tzdata is missing at runtime, and
// silently hiding a channel would be the worse failure.
func (s *ChannelSchedule) Contains(at time.Time) bool {
	if s == nil || len(s.Windows) == 0 {
		return true
	}
	loc, err := loadScheduleLocation(s.Timezone)
	if err != nil {
		return true
	}
	local := at.In(loc)
	minutes := local.Hour()*60 + local.Minute()
	weekday := int(local.Weekday())
	previousWeekday := (weekday + 6) % 7
	for _, window := range s.Windows {
		start, okStart := parseClockMinutes(window.Start)
		end, okEnd := parseClockMinutes(window.End)
		if !okStart || !okEnd {
			continue
		}
		if start < end {
			if window.startsOn(weekday) && minutes >= start && minutes < end {
				return true
			}
			continue
		}
		// Crosses midnight: the part after Start belongs to the weekday it
		// starts on, the part before End to the following day.
		if window.startsOn(weekday) && minutes >= start {
			return true
		}
		if window.startsOn(previousWeekday) && minutes < end {
			return true
		}
	}
	return false
}

// ResolveClaudeAuthMode returns the concrete scheme (api_key or oauth) for a
// key under the configured mode. Auto mode decides per key, so a multi-key
// channel may mix both credential kinds.
func ResolveClaudeAuthMode(mode string, key string) string {
	switch mode {
	case ClaudeAuthModeOAuth:
		return ClaudeAuthModeOAuth
	case ClaudeAuthModeAuto:
		if strings.HasPrefix(strings.TrimSpace(key), ClaudeOAuthTokenPrefix) {
			return ClaudeAuthModeOAuth
		}
	}
	return ClaudeAuthModeApiKey
}

// ChannelExtendSettings carries per-channel overrides stored outside the
// channels table (see model.ChannelExtend). Zero values mean "inherit the
// global configuration".
type ChannelExtendSettings struct {
	// RelayTimeout is the overall upstream request deadline in seconds,
	// covering connection, response headers and full body read.
	// 0 = inherit the global RELAY_TIMEOUT.
	RelayTimeout int `json:"relay_timeout,omitempty"`
	// StreamingTimeout is the idle timeout between streaming events in
	// seconds. 0 = inherit the global STREAMING_TIMEOUT.
	StreamingTimeout int `json:"streaming_timeout,omitempty"`
	// MinInputTokens routes a request to this channel only when its estimated
	// input token count is strictly greater than this threshold.
	// 0 = no minimum.
	MinInputTokens int `json:"min_input_tokens,omitempty"`
	// MaxInputTokens routes a request to this channel only when its estimated
	// input token count is less than or equal to this threshold.
	// 0 = no maximum. The exclusive minimum and inclusive maximum let two
	// channels partition traffic without gap or overlap.
	MaxInputTokens int `json:"max_input_tokens,omitempty"`
	// RpmLimit caps how many requests per minute may be routed to this
	// channel (channel-wide, across all users and keys). A saturated channel
	// is skipped during selection so traffic fails over to other channels.
	// 0 = no limit.
	RpmLimit int `json:"rpm_limit,omitempty"`
	// TpmLimit caps the tokens per minute accounted to this channel. Like the
	// user-level TPM limit it is settled after billing, so the first requests
	// of a fresh minute may overshoot. 0 = no limit.
	TpmLimit int `json:"tpm_limit,omitempty"`
	// ClaudeAuthMode selects how Anthropic channels send the key upstream:
	// api_key (x-api-key), oauth (Authorization: Bearer) or auto (by key
	// prefix). "" = api_key. Ignored by other channel types.
	ClaudeAuthMode string `json:"claude_auth_mode,omitempty"`
	// ResponseHeaderMode filters which upstream response headers are copied
	// back to the client: blacklist drops the names in ResponseHeaders,
	// whitelist keeps only those names. "" = copy every upstream header.
	// Content-Type and Content-Encoding are always kept so the body stays
	// interpretable.
	ResponseHeaderMode string `json:"response_header_mode,omitempty"`
	// ResponseHeaders lists the header names (case-insensitive) that
	// ResponseHeaderMode applies to.
	ResponseHeaders []string `json:"response_headers,omitempty"`
	// CostRatio is the estimated USD paid upstream for every $1 of quota
	// billed through this channel (0.2 means a 20% reseller price). It only
	// drives cost reporting. 0 = not configured.
	CostRatio float64 `json:"cost_ratio,omitempty"`
	// QuotaLimit disables the channel automatically once its cumulative
	// used quota reaches this value, in quota units (QuotaPerUnit = $1).
	// 0 = no limit.
	QuotaLimit int64 `json:"quota_limit,omitempty"`
	// Schedule limits channel selection to weekly availability windows.
	// nil = always available.
	Schedule *ChannelSchedule `json:"schedule,omitempty"`
	// ForceRetry retries every relay error from this channel, skipping the
	// status code, error code and keyword retry rules. The global retry
	// count, pinned channels, strict sessions, started responses,
	// disconnected clients and content violations still stop the retry.
	// Async task submissions are not affected.
	ForceRetry bool `json:"force_retry,omitempty"`
}

func (s *ChannelExtendSettings) Validate() error {
	if s == nil {
		return nil
	}
	if s.RelayTimeout < 0 || s.RelayTimeout > MaxChannelTimeoutSeconds {
		return fmt.Errorf("invalid relay_timeout: %d, must be within [0, %d]", s.RelayTimeout, MaxChannelTimeoutSeconds)
	}
	if s.StreamingTimeout < 0 || s.StreamingTimeout > MaxChannelTimeoutSeconds {
		return fmt.Errorf("invalid streaming_timeout: %d, must be within [0, %d]", s.StreamingTimeout, MaxChannelTimeoutSeconds)
	}
	if s.MinInputTokens < 0 || s.MinInputTokens > MaxChannelInputTokensBound {
		return fmt.Errorf("invalid min_input_tokens: %d, must be within [0, %d]", s.MinInputTokens, MaxChannelInputTokensBound)
	}
	if s.MaxInputTokens < 0 || s.MaxInputTokens > MaxChannelInputTokensBound {
		return fmt.Errorf("invalid max_input_tokens: %d, must be within [0, %d]", s.MaxInputTokens, MaxChannelInputTokensBound)
	}
	// min 为排他下界、max 为包含上界：max <= min 时可接受区间为空，渠道永远不可选
	if s.MinInputTokens > 0 && s.MaxInputTokens > 0 && s.MaxInputTokens <= s.MinInputTokens {
		return fmt.Errorf("invalid max_input_tokens: %d, must be greater than min_input_tokens %d", s.MaxInputTokens, s.MinInputTokens)
	}
	if s.RpmLimit < 0 || s.RpmLimit > MaxChannelRateLimitValue {
		return fmt.Errorf("invalid rpm_limit: %d, must be within [0, %d]", s.RpmLimit, MaxChannelRateLimitValue)
	}
	if s.TpmLimit < 0 || s.TpmLimit > MaxChannelRateLimitValue {
		return fmt.Errorf("invalid tpm_limit: %d, must be within [0, %d]", s.TpmLimit, MaxChannelRateLimitValue)
	}
	switch s.ClaudeAuthMode {
	case "", ClaudeAuthModeApiKey, ClaudeAuthModeOAuth, ClaudeAuthModeAuto:
	default:
		return fmt.Errorf("invalid claude_auth_mode: %q, must be one of %s, %s, %s", s.ClaudeAuthMode, ClaudeAuthModeApiKey, ClaudeAuthModeOAuth, ClaudeAuthModeAuto)
	}
	switch s.ResponseHeaderMode {
	case "":
		if len(s.ResponseHeaders) > 0 {
			return fmt.Errorf("response_headers requires response_header_mode %s or %s", ResponseHeaderModeBlacklist, ResponseHeaderModeWhitelist)
		}
	case ResponseHeaderModeBlacklist, ResponseHeaderModeWhitelist:
		if len(s.ResponseHeaders) == 0 {
			return fmt.Errorf("response_headers must list at least one header for response_header_mode %s", s.ResponseHeaderMode)
		}
		if len(s.ResponseHeaders) > MaxChannelResponseHeaderRules {
			return fmt.Errorf("too many response_headers: %d, at most %d", len(s.ResponseHeaders), MaxChannelResponseHeaderRules)
		}
		seen := make(map[string]struct{}, len(s.ResponseHeaders))
		for _, name := range s.ResponseHeaders {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" || len(trimmed) > MaxChannelResponseHeaderNameLength {
				return fmt.Errorf("invalid response header name %q: must be 1-%d characters", name, MaxChannelResponseHeaderNameLength)
			}
			for _, r := range trimmed {
				// RFC 7230 token characters are the only ones legal in a field name.
				isTokenChar := r < 0x80 && (r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || strings.ContainsRune("!#$%&'*+-.^_`|~", r))
				if !isTokenChar {
					return fmt.Errorf("invalid response header name %q: only RFC 7230 token characters are allowed", name)
				}
			}
			lower := strings.ToLower(trimmed)
			if _, dup := seen[lower]; dup {
				return fmt.Errorf("duplicate response header name %q", name)
			}
			seen[lower] = struct{}{}
		}
	default:
		return fmt.Errorf("invalid response_header_mode: %q, must be one of %s, %s", s.ResponseHeaderMode, ResponseHeaderModeBlacklist, ResponseHeaderModeWhitelist)
	}
	if math.IsNaN(s.CostRatio) || math.IsInf(s.CostRatio, 0) || s.CostRatio < 0 || s.CostRatio > MaxChannelCostRatio {
		return fmt.Errorf("invalid cost_ratio: %v, must be within [0, %d]", s.CostRatio, MaxChannelCostRatio)
	}
	if s.QuotaLimit < 0 || s.QuotaLimit > MaxChannelQuotaLimit {
		return fmt.Errorf("invalid quota_limit: %d, must be within [0, %d]", s.QuotaLimit, MaxChannelQuotaLimit)
	}
	if err := s.Schedule.Validate(); err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}
	return nil
}

// IsZero reports whether every override inherits the global configuration.
func (s *ChannelExtendSettings) IsZero() bool {
	return s == nil || (s.RelayTimeout == 0 && s.StreamingTimeout == 0 &&
		s.MinInputTokens == 0 && s.MaxInputTokens == 0 &&
		s.RpmLimit == 0 && s.TpmLimit == 0 &&
		(s.ClaudeAuthMode == "" || s.ClaudeAuthMode == ClaudeAuthModeApiKey) &&
		s.ResponseHeaderMode == "" && len(s.ResponseHeaders) == 0 &&
		s.CostRatio == 0 && s.QuotaLimit == 0 && s.Schedule == nil && !s.ForceRetry)
}

// AvailableAt reports whether the channel may be selected at the given
// instant under its availability schedule. Channels without a schedule are
// always available.
func (s *ChannelExtendSettings) AvailableAt(at time.Time) bool {
	return s == nil || s.Schedule.Contains(at)
}

// QuotaLimitReached reports whether a channel's cumulative used quota has hit
// its configured limit. Channels without a limit never reach it.
func (s *ChannelExtendSettings) QuotaLimitReached(usedQuota int64) bool {
	return s != nil && s.QuotaLimit > 0 && usedQuota >= s.QuotaLimit
}

// AllowsResponseHeader reports whether an upstream response header may be
// copied to the client under the channel's response header filter. Without a
// mode every header passes. Content-Type and Content-Encoding always pass
// because dropping them would leave the body uninterpretable.
func (s *ChannelExtendSettings) AllowsResponseHeader(name string) bool {
	if s == nil || s.ResponseHeaderMode == "" {
		return true
	}
	if strings.EqualFold(name, "Content-Type") || strings.EqualFold(name, "Content-Encoding") {
		return true
	}
	listed := slices.ContainsFunc(s.ResponseHeaders, func(rule string) bool {
		return strings.EqualFold(strings.TrimSpace(rule), name)
	})
	switch s.ResponseHeaderMode {
	case ResponseHeaderModeWhitelist:
		return listed
	case ResponseHeaderModeBlacklist:
		return !listed
	}
	return true
}
