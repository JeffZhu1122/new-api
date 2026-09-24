package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetClaudeAuthHeader(t *testing.T) {
	const apiKey = "sk-ant-api03-abc"
	const oatKey = "sk-ant-oat01-xyz"

	tests := []struct {
		name       string
		mode       string
		key        string
		wantBearer bool
	}{
		{"default uses x-api-key", "", apiKey, false},
		{"default keeps x-api-key even for oat", "", oatKey, false},
		{"api_key mode", dto.ClaudeAuthModeApiKey, oatKey, false},
		{"oauth mode", dto.ClaudeAuthModeOAuth, apiKey, true},
		{"auto detects oat", dto.ClaudeAuthModeAuto, oatKey, true},
		{"auto keeps api key", dto.ClaudeAuthModeAuto, apiKey, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("anthropic-beta", "context-1m-2025-08-07, oauth-2025-04-20")
			SetClaudeAuthHeader(&h, tt.mode, tt.key)
			if tt.wantBearer {
				assert.Equal(t, "Bearer "+tt.key, h.Get("Authorization"))
				assert.Empty(t, h.Get("x-api-key"))
				assert.Equal(t, "oauth-2025-04-20,context-1m-2025-08-07", h.Get("anthropic-beta"))
			} else {
				assert.Equal(t, tt.key, h.Get("x-api-key"))
				assert.Empty(t, h.Get("Authorization"))
			}
		})
	}
}

func TestSetClaudeAuthHeaderAddsOAuthBetaWhenAbsent(t *testing.T) {
	h := http.Header{}
	SetClaudeAuthHeader(&h, dto.ClaudeAuthModeOAuth, "sk-ant-oat01-xyz")
	assert.Equal(t, ClaudeOAuthBeta, h.Get("anthropic-beta"))
}

func TestSetupRequestHeaderAppliesAuthModeOnlyForAnthropic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		channelType int
		wantBearer  bool
	}{
		{constant.ChannelTypeAnthropic, true},
		{constant.ChannelTypeDeepSeek, false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          tc.channelType,
			ApiKey:               "sk-ant-oat01-xyz",
			ChannelExtendSetting: dto.ChannelExtendSettings{ClaudeAuthMode: dto.ClaudeAuthModeOAuth},
		}}
		h := http.Header{}
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &h, info))
		assert.Equal(t, tc.wantBearer, h.Get("Authorization") != "", "channel type %d", tc.channelType)
		assert.Equal(t, !tc.wantBearer, h.Get("x-api-key") != "", "channel type %d", tc.channelType)
	}
}
