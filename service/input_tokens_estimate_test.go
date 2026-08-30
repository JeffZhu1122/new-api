package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 断言各中转格式的正文文本被完整提取：估算值必须等于对期望文本串的估算，
// 提取遗漏会导致请求被配置了输入范围的渠道误拒。
func TestEstimateInputTokensFromJSONExtraction(t *testing.T) {
	const model = "gpt-4o"
	tests := []struct {
		name     string
		body     string
		wantText string
	}{
		{
			name:     "openai chat string and text parts",
			body:     `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello world"},{"role":"user","content":[{"type":"text","text":"part one"},{"type":"image_url","image_url":{"url":"data:image/png;base64,xxx"}}]}]}`,
			wantText: "Hello world\npart one\n",
		},
		{
			name:     "claude system and message parts",
			body:     `{"model":"claude-3","system":"be terse","messages":[{"role":"user","content":[{"type":"text","text":"question"}]}]}`,
			wantText: "question\nbe terse\n",
		},
		{
			name:     "gemini contents and system instruction",
			body:     `{"contents":[{"parts":[{"text":"gemini input"}]}],"systemInstruction":{"parts":[{"text":"sys"}]}}`,
			wantText: "gemini input\nsys\n",
		},
		{
			name:     "responses instructions and input items",
			body:     `{"model":"gpt-4o","instructions":"follow rules","input":[{"role":"user","content":[{"type":"input_text","text":"do it"}]}]}`,
			wantText: "follow rules\ndo it\n",
		},
		{
			name:     "responses compaction body",
			body:     `{"model":"gpt-5.3-codex","instructions":"compact context","input":[{"role":"user","content":[{"type":"input_text","text":"long history"}]}],"previous_response_id":"resp_1"}`,
			wantText: "compact context\nlong history\n",
		},
		{
			name:     "no text fields",
			body:     `{"model":"gpt-4o"}`,
			wantText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			estimate, ok := estimateInputTokensFromJSON([]byte(tt.body), model)
			require.True(t, ok)
			assert.Equal(t, EstimateTokenByModel(model, tt.wantText), estimate)
		})
	}
}

func TestEstimateInputTokensFromJSONUnavailable(t *testing.T) {
	_, ok := estimateInputTokensFromJSON(nil, "gpt-4o")
	assert.False(t, ok)
	_, ok = estimateInputTokensFromJSON([]byte(`[]`), "gpt-4o")
	assert.False(t, ok)
}

func TestInputTokensEstimateSupportedPath(t *testing.T) {
	supported := []string{
		"/v1/chat/completions",
		"/pg/chat/completions",
		"/v1/messages",
		"/v1/responses",
		"/v1/responses/compact",
		"/v1beta/models/gemini-2.0-flash:generateContent",
		"/v1beta/models/gemini-2.0-flash:streamGenerateContent",
	}
	unsupported := []string{
		"/v1/messages/count_tokens",
		"/v1/embeddings",
		"/v1/audio/transcriptions",
		"/v1/images/generations",
		"/mj/submit/imagine",
	}
	for _, path := range supported {
		assert.True(t, inputTokensEstimateSupportedPath(path), path)
	}
	for _, path := range unsupported {
		assert.False(t, inputTokensEstimateSupportedPath(path), path)
	}
}
