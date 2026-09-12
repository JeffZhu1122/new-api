package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPath2RelayMode(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{path: "/v1/alpha/search", want: RelayModeAlphaSearch},
		{path: "/v1/alpha/search?foo=1", want: RelayModeAlphaSearch},
		{path: "/v1/responses", want: RelayModeResponses},
		{path: "/v1/responses/compact", want: RelayModeResponsesCompact},
		{path: "/v1/responses/input_tokens", want: RelayModeResponsesInputTokens},
		{path: "/v1/messages/count_tokens", want: RelayModeClaudeCountTokens},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, Path2RelayMode(tt.path))
		})
	}
}
