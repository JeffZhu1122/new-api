package dto

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiChatResponseUsageMetadataPresence(t *testing.T) {
	var missing GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[]}`), &missing))
	assert.False(t, missing.HasUsageMetadata)
	assert.Nil(t, missing.GetUsageMetadata())

	var empty GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[],"usageMetadata":{}}`), &empty))
	assert.True(t, empty.HasUsageMetadata)
	require.NotNil(t, empty.GetUsageMetadata())
	assert.False(t, HasGeminiUsageMetadataTokens(empty.GetUsageMetadata()))

	var populated GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[],"usageMetadata":{"promptTokenCount":3}}`), &populated))
	assert.True(t, populated.HasUsageMetadata)
	require.NotNil(t, populated.GetUsageMetadata())
	assert.True(t, HasGeminiUsageMetadataTokens(populated.GetUsageMetadata()))
}

func TestGeminiChatResponseMarshalKeepsUsageMetadataField(t *testing.T) {
	data, err := kitutil.Marshal(GeminiChatResponse{})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"usageMetadata"`)
}

// Gemini never sends explicit nulls, and SDKs that model these fields as
// optional values (google-genai for Java, for one) fail on them, so a response
// rebuilt from these types must leave unset fields out.
func TestGeminiChatResponseMarshalLeavesUnsetFieldsOut(t *testing.T) {
	var parsed GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"text":"5"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1,"totalTokenCount":4,"promptTokensDetails":[{"modality":"TEXT","tokenCount":3}]}}`), &parsed))

	tests := []struct {
		name     string
		response GeminiChatResponse
		want     string
	}{
		{
			name:     "parsed upstream response",
			response: parsed,
			want:     `{"candidates":[{"content":{"role":"model","parts":[{"text":"5"}]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":3,"toolUsePromptTokenCount":0,"candidatesTokenCount":1,"totalTokenCount":4,"thoughtsTokenCount":0,"cachedContentTokenCount":0,"promptTokensDetails":[{"modality":"TEXT","tokenCount":3}]}}`,
		},
		{
			name: "chunk without content, finish reason or ratings",
			response: GeminiChatResponse{
				Candidates:     []GeminiChatCandidate{{}},
				PromptFeedback: &GeminiChatPromptFeedback{},
			},
			want: `{"candidates":[{"content":{},"index":0}],"promptFeedback":{},"usageMetadata":{"promptTokenCount":0,"toolUsePromptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0,"thoughtsTokenCount":0,"cachedContentTokenCount":0}}`,
		},
		{
			name: "no candidates",
			want: `{"usageMetadata":{"promptTokenCount":0,"toolUsePromptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0,"thoughtsTokenCount":0,"cachedContentTokenCount":0}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := kitutil.Marshal(tt.response)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(data))
			assert.NotContains(t, string(data), "null")
		})
	}
}
