package claude

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLCountTokens(t *testing.T) {
	tests := []struct {
		name      string
		relayMode int
		betaQuery bool
		want      string
	}{
		{
			name:      "messages",
			relayMode: relayconstant.RelayModeUnknown,
			want:      "https://api.anthropic.com/v1/messages",
		},
		{
			name:      "count_tokens",
			relayMode: relayconstant.RelayModeClaudeCountTokens,
			want:      "https://api.anthropic.com/v1/messages/count_tokens",
		},
		{
			name:      "count_tokens_with_beta_query",
			relayMode: relayconstant.RelayModeClaudeCountTokens,
			betaQuery: true,
			want:      "https://api.anthropic.com/v1/messages/count_tokens?beta=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayMode: tt.relayMode,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelBaseUrl: "https://api.anthropic.com",
					ChannelOtherSettings: dto.ChannelOtherSettings{
						ClaudeBetaQuery: tt.betaQuery,
					},
				},
			}
			adaptor := &Adaptor{}
			got, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
