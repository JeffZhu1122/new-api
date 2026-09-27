package dto

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelExtendSettingsValidate(t *testing.T) {
	tooManyHeaders := make([]string, MaxChannelResponseHeaderRules+1)
	for i := range tooManyHeaders {
		tooManyHeaders[i] = fmt.Sprintf("X-Rule-%d", i)
	}
	tests := []struct {
		name     string
		settings *ChannelExtendSettings
		wantErr  string
	}{
		{name: "nil settings valid", settings: nil},
		{name: "zero settings valid", settings: &ChannelExtendSettings{}},
		{name: "max boundary valid", settings: &ChannelExtendSettings{RelayTimeout: MaxChannelTimeoutSeconds, StreamingTimeout: MaxChannelTimeoutSeconds, MinInputTokens: MaxChannelInputTokensBound - 1, MaxInputTokens: MaxChannelInputTokensBound}},
		{name: "negative relay timeout rejected", settings: &ChannelExtendSettings{RelayTimeout: -1}, wantErr: "relay_timeout"},
		{name: "oversized relay timeout rejected", settings: &ChannelExtendSettings{RelayTimeout: MaxChannelTimeoutSeconds + 1}, wantErr: "relay_timeout"},
		{name: "negative streaming timeout rejected", settings: &ChannelExtendSettings{StreamingTimeout: -1}, wantErr: "streaming_timeout"},
		{name: "oversized streaming timeout rejected", settings: &ChannelExtendSettings{StreamingTimeout: MaxChannelTimeoutSeconds + 1}, wantErr: "streaming_timeout"},
		{name: "negative min input tokens rejected", settings: &ChannelExtendSettings{MinInputTokens: -1}, wantErr: "min_input_tokens"},
		{name: "oversized min input tokens rejected", settings: &ChannelExtendSettings{MinInputTokens: MaxChannelInputTokensBound + 1}, wantErr: "min_input_tokens"},
		{name: "negative max input tokens rejected", settings: &ChannelExtendSettings{MaxInputTokens: -1}, wantErr: "max_input_tokens"},
		{name: "oversized max input tokens rejected", settings: &ChannelExtendSettings{MaxInputTokens: MaxChannelInputTokensBound + 1}, wantErr: "max_input_tokens"},
		{name: "max equal to min rejected", settings: &ChannelExtendSettings{MinInputTokens: 1000, MaxInputTokens: 1000}, wantErr: "must be greater than min_input_tokens"},
		{name: "max below min rejected", settings: &ChannelExtendSettings{MinInputTokens: 1000, MaxInputTokens: 500}, wantErr: "must be greater than min_input_tokens"},
		{name: "max above min valid", settings: &ChannelExtendSettings{MinInputTokens: 1000, MaxInputTokens: 1001}},
		{name: "max alone valid", settings: &ChannelExtendSettings{MaxInputTokens: 500}},
		{name: "rate limits at max boundary valid", settings: &ChannelExtendSettings{RpmLimit: MaxChannelRateLimitValue, TpmLimit: MaxChannelRateLimitValue}},
		{name: "negative rpm limit rejected", settings: &ChannelExtendSettings{RpmLimit: -1}, wantErr: "rpm_limit"},
		{name: "oversized rpm limit rejected", settings: &ChannelExtendSettings{RpmLimit: MaxChannelRateLimitValue + 1}, wantErr: "rpm_limit"},
		{name: "negative tpm limit rejected", settings: &ChannelExtendSettings{TpmLimit: -1}, wantErr: "tpm_limit"},
		{name: "oversized tpm limit rejected", settings: &ChannelExtendSettings{TpmLimit: MaxChannelRateLimitValue + 1}, wantErr: "tpm_limit"},
		{name: "blacklist with header names valid", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"OpenAI-Organization", "x-ratelimit-remaining-requests"}}},
		{name: "whitelist with header names valid", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeWhitelist, ResponseHeaders: []string{"X-Request-Id"}}},
		{name: "unknown response header mode rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: "allow", ResponseHeaders: []string{"X-Foo"}}, wantErr: "invalid response_header_mode"},
		{name: "response headers without mode rejected", settings: &ChannelExtendSettings{ResponseHeaders: []string{"X-Foo"}}, wantErr: "requires response_header_mode"},
		{name: "response header mode without names rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist}, wantErr: "at least one header"},
		{name: "blank response header name rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{" "}}, wantErr: "invalid response header name"},
		{name: "response header name with space rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"X Foo"}}, wantErr: "token characters"},
		{name: "duplicate response header names rejected ignoring case", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeWhitelist, ResponseHeaders: []string{"X-Foo", "x-foo"}}, wantErr: "duplicate"},
		{name: "too many response header names rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: tooManyHeaders}, wantErr: "too many"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestChannelExtendSettingsIsZero(t *testing.T) {
	var nilSettings *ChannelExtendSettings
	assert.True(t, nilSettings.IsZero())
	assert.True(t, (&ChannelExtendSettings{}).IsZero())
	assert.False(t, (&ChannelExtendSettings{RelayTimeout: 1}).IsZero())
	assert.False(t, (&ChannelExtendSettings{StreamingTimeout: 1}).IsZero())
	// min/max_input_tokens 单独配置时不得被当作全零删除
	assert.False(t, (&ChannelExtendSettings{MinInputTokens: 1}).IsZero())
	assert.False(t, (&ChannelExtendSettings{MaxInputTokens: 1}).IsZero())
	// rpm/tpm_limit 单独配置时同样不得被当作全零删除
	assert.False(t, (&ChannelExtendSettings{RpmLimit: 1}).IsZero())
	assert.False(t, (&ChannelExtendSettings{TpmLimit: 1}).IsZero())
	// 响应头过滤单独配置时同样不得被当作全零删除
	assert.False(t, (&ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"X-Foo"}}).IsZero())
}

func TestChannelExtendSettingsAllowsResponseHeader(t *testing.T) {
	blacklist := &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"OpenAI-Organization", " x-ratelimit-remaining-requests "}}
	whitelist := &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeWhitelist, ResponseHeaders: []string{"X-Request-Id"}}
	tests := []struct {
		name     string
		settings *ChannelExtendSettings
		header   string
		want     bool
	}{
		{name: "nil settings copy every header", settings: nil, header: "OpenAI-Organization", want: true},
		{name: "no mode copies every header", settings: &ChannelExtendSettings{}, header: "OpenAI-Organization", want: true},
		{name: "blacklist drops a listed header ignoring case", settings: blacklist, header: "openai-organization", want: false},
		{name: "blacklist trims whitespace around rules", settings: blacklist, header: "X-RateLimit-Remaining-Requests", want: false},
		{name: "blacklist keeps an unlisted header", settings: blacklist, header: "X-Request-Id", want: true},
		{name: "whitelist keeps a listed header ignoring case", settings: whitelist, header: "x-request-id", want: true},
		{name: "whitelist drops an unlisted header", settings: whitelist, header: "OpenAI-Organization", want: false},
		{name: "whitelist always keeps Content-Type", settings: whitelist, header: "Content-Type", want: true},
		{name: "blacklist cannot drop Content-Encoding", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"Content-Encoding"}}, header: "content-encoding", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.settings.AllowsResponseHeader(tt.header))
		})
	}
}
