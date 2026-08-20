package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

// count_tokens 路径的渠道可用性判定（亲和渠道 / 令牌指定渠道共用 ChannelSatisfiesFilters）：
// 必须是 Anthropic 类型且开启 count_tokens_enabled；普通路径不受影响。
func TestChannelSatisfiesFiltersCountTokens(t *testing.T) {
	anthropicEnabled := &model.Channel{Type: constant.ChannelTypeAnthropic, OtherSettings: `{"count_tokens_enabled":true}`}
	anthropicDisabled := &model.Channel{Type: constant.ChannelTypeAnthropic}
	openAIEnabled := &model.Channel{Type: constant.ChannelTypeOpenAI, OtherSettings: `{"count_tokens_enabled":true}`}

	tests := []struct {
		name    string
		channel *model.Channel
		path    string
		want    bool
	}{
		{name: "anthropic_enabled_count_tokens", channel: anthropicEnabled, path: constant.ClaudeCountTokensPath, want: true},
		{name: "anthropic_disabled_count_tokens", channel: anthropicDisabled, path: constant.ClaudeCountTokensPath, want: false},
		{name: "openai_enabled_count_tokens", channel: openAIEnabled, path: constant.ClaudeCountTokensPath, want: false},
		{name: "nil_channel", channel: nil, path: constant.ClaudeCountTokensPath, want: false},
		{name: "anthropic_disabled_messages", channel: anthropicDisabled, path: "/v1/messages", want: true},
		{name: "openai_enabled_chat", channel: openAIEnabled, path: "/v1/chat/completions", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, _ := model.ChannelSatisfiesFilters(tt.channel, "claude-test-model", []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: tt.path}})
			assert.Equal(t, tt.want, ok)
		})
	}
}
