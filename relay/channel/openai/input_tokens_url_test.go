package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /v1/responses/input_tokens 必须原样透传到上游同名路径，不能被默认分支改写成 chat/completions。
func TestGetRequestURLResponsesInputTokens(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "official", baseURL: "https://api.openai.com", want: "https://api.openai.com/v1/responses/input_tokens"},
		{name: "custom_base_url", baseURL: "https://gateway.example.com", want: "https://gateway.example.com/v1/responses/input_tokens"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayMode:      relayconstant.RelayModeResponsesInputTokens,
				RelayFormat:    types.RelayFormatOpenAIResponsesInputTokens,
				RequestURLPath: constant.OpenAIInputTokensPath,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    constant.ChannelTypeOpenAI,
					ChannelBaseUrl: tt.baseURL,
				},
			}
			adaptor := &Adaptor{}
			got, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
