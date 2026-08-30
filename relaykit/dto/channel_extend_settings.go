package dto

import "fmt"

// MaxChannelTimeoutSeconds bounds per-channel timeout overrides (24h).
const MaxChannelTimeoutSeconds = 86400

// MaxChannelMinInputTokens bounds the per-channel minimum input threshold.
const MaxChannelMinInputTokens = 10_000_000

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
	if s.MinInputTokens < 0 || s.MinInputTokens > MaxChannelMinInputTokens {
		return fmt.Errorf("invalid min_input_tokens: %d, must be within [0, %d]", s.MinInputTokens, MaxChannelMinInputTokens)
	}
	return nil
}

// IsZero reports whether every override inherits the global configuration.
func (s *ChannelExtendSettings) IsZero() bool {
	return s == nil || (s.RelayTimeout == 0 && s.StreamingTimeout == 0 && s.MinInputTokens == 0)
}
