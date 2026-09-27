package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestShouldCopyUpstreamHeaderHonoursChannelResponseHeaderFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	whitelist := &dto.ChannelExtendSettings{ResponseHeaderMode: dto.ResponseHeaderModeWhitelist, ResponseHeaders: []string{"X-Request-Id", "Content-Length"}}
	tests := []struct {
		name     string
		settings *dto.ChannelExtendSettings
		header   string
		want     bool
	}{
		{name: "without a channel setting every upstream header is copied", settings: nil, header: "OpenAI-Organization", want: true},
		{name: "blacklist drops a listed header", settings: &dto.ChannelExtendSettings{ResponseHeaderMode: dto.ResponseHeaderModeBlacklist, ResponseHeaders: []string{"OpenAI-Organization"}}, header: "openai-organization", want: false},
		{name: "whitelist drops an unlisted header", settings: whitelist, header: "OpenAI-Organization", want: false},
		{name: "whitelist keeps a listed header", settings: whitelist, header: "X-Request-Id", want: true},
		{name: "whitelist never drops Content-Type", settings: whitelist, header: "Content-Type", want: true},
		{name: "Content-Length stays managed locally even when whitelisted", settings: whitelist, header: "Content-Length", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.settings != nil {
				common.SetContextKey(ctx, constant.ContextKeyChannelExtendSetting, *tt.settings)
			}
			assert.Equal(t, tt.want, ShouldCopyUpstreamHeader(ctx, tt.header, []string{"value"}))
		})
	}
}

func TestIOCopyBytesGracefullyAppliesChannelResponseHeaderFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	common.SetContextKey(ctx, constant.ContextKeyChannelExtendSetting, dto.ChannelExtendSettings{
		ResponseHeaderMode: dto.ResponseHeaderModeBlacklist,
		ResponseHeaders:    []string{"OpenAI-Organization", "Set-Cookie"},
	})

	upstream := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("OpenAI-Organization", "org-secret")
	upstream.Header.Set("Set-Cookie", "session=abc")
	upstream.Header.Set("X-Request-Id", "req-1")

	IOCopyBytesGracefully(ctx, upstream, []byte(`{"ok":true}`))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "req-1", recorder.Header().Get("X-Request-Id"))
	assert.Empty(t, recorder.Header().Get("OpenAI-Organization"))
	assert.Empty(t, recorder.Header().Get("Set-Cookie"))
	assert.Equal(t, `{"ok":true}`, recorder.Body.String())
}
