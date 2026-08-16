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
		{name: "max boundary valid", settings: &ChannelExtendSettings{RelayTimeout: MaxChannelTimeoutSeconds, StreamingTimeout: MaxChannelTimeoutSeconds}},
		{name: "negative relay timeout rejected", settings: &ChannelExtendSettings{RelayTimeout: -1}, wantErr: "relay_timeout"},
		{name: "oversized relay timeout rejected", settings: &ChannelExtendSettings{RelayTimeout: MaxChannelTimeoutSeconds + 1}, wantErr: "relay_timeout"},
		{name: "negative streaming timeout rejected", settings: &ChannelExtendSettings{StreamingTimeout: -1}, wantErr: "streaming_timeout"},
		{name: "oversized streaming timeout rejected", settings: &ChannelExtendSettings{StreamingTimeout: MaxChannelTimeoutSeconds + 1}, wantErr: "streaming_timeout"},
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
}
