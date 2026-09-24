package dto

import (
	"fmt"
	"math"
	"strings"
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
	return nil
}

// IsZero reports whether every override inherits the global configuration.
func (s *ChannelExtendSettings) IsZero() bool {
	return s == nil || (s.RelayTimeout == 0 && s.StreamingTimeout == 0 &&
		s.MinInputTokens == 0 && s.MaxInputTokens == 0 &&
		s.RpmLimit == 0 && s.TpmLimit == 0 &&
		(s.ClaudeAuthMode == "" || s.ClaudeAuthMode == ClaudeAuthModeApiKey))
}
