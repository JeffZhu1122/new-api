package helper

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /v1/responses/input_tokens 与 Responses 请求同构，但 input 在上游是可选的
// （previous_response_id / conversation 也可作为输入来源），校验只要求 model。
func TestGetAndValidateResponsesInputTokensRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		body      string
		wantErr   string
		wantModel string
	}{
		{name: "model_and_input", body: `{"model":"gpt-4o","input":"hello"}`, wantModel: "gpt-4o"},
		{name: "model_without_input", body: `{"model":"gpt-4o","previous_response_id":"resp_123"}`, wantModel: "gpt-4o"},
		{name: "missing_model", body: `{"input":"hello"}`, wantErr: "model is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/input_tokens", bytes.NewBufferString(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			req, err := GetAndValidateResponsesInputTokensRequest(c)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantModel, req.Model)
		})
	}
}
