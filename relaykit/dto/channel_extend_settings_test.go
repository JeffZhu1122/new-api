package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelExtendSettingsValidate(t *testing.T) {
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
}
