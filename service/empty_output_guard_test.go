package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmptyOutputDetectionPerClientFormat(t *testing.T) {
	streams := []struct {
		name   string
		format types.RelayFormat
		event  string
		output bool
	}{
		{"openai role only", types.RelayFormatOpenAI, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}` + "\n\n", false},
		{"openai usage chunk", types.RelayFormatOpenAI, `data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":0}}` + "\n\n", false},
		{"openai done", types.RelayFormatOpenAI, "data: [DONE]\n\n", false},
		{"openai content", types.RelayFormatOpenAI, `data: {"choices":[{"delta":{"content":"Hi"}}]}` + "\n\n", true},
		{"openai reasoning", types.RelayFormatOpenAI, `data: {"choices":[{"delta":{"reasoning_content":"thinking"}}]}` + "\n\n", true},
		{"openai tool call", types.RelayFormatOpenAI, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"f"}}]}}]}` + "\n\n", true},
		{"openai legacy completion", types.RelayFormatOpenAI, `data: {"choices":[{"text":"Hi"}]}` + "\n\n", true},
		{"claude message start", types.RelayFormatClaude, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\n", false},
		{"claude empty text block", types.RelayFormatClaude, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n", false},
		{"claude signature", types.RelayFormatClaude, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"signature_delta\",\"signature\":\"abc\"}}\n\n", false},
		{"claude text delta", types.RelayFormatClaude, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n", true},
		{"claude thinking delta", types.RelayFormatClaude, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"hm\"}}\n\n", true},
		{"claude tool use", types.RelayFormatClaude, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"tool_use\",\"id\":\"t\",\"name\":\"f\",\"input\":{}}}\n\n", true},
		{"gemini finish only", types.RelayFormatGemini, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":"STOP"}]}` + "\n\n", false},
		{"gemini text", types.RelayFormatGemini, `data: {"candidates":[{"content":{"parts":[{"text":"Hi"}]}}]}` + "\n\n", true},
		{"gemini function call", types.RelayFormatGemini, `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"f","args":{}}}]}}]}` + "\n\n", true},
		{"responses created", types.RelayFormatOpenAIResponses, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"output\":[]}}\n\n", false},
		{"responses empty message added", types.RelayFormatOpenAIResponses, "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\",\"content\":[]}}\n\n", false},
		{"responses text delta", types.RelayFormatOpenAIResponses, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi\"}\n\n", true},
		{"responses function call", types.RelayFormatOpenAIResponses, "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"name\":\"f\"}}\n\n", true},
		{"responses completed with text", types.RelayFormatOpenAIResponses, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hi\"}]}]}}\n\n", true},
	}
	for _, test := range streams {
		t.Run("stream "+test.name, func(t *testing.T) {
			assert.Equal(t, test.output, streamEventHasOutput(test.format, []byte(test.event)))
		})
	}

	bodies := []struct {
		name   string
		format types.RelayFormat
		body   string
		output bool
	}{
		{"openai empty message", types.RelayFormatOpenAI, `{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"completion_tokens":0}}`, false},
		{"openai null content with refusal null", types.RelayFormatOpenAI, `{"choices":[{"message":{"role":"assistant","content":null,"refusal":null}}]}`, false},
		{"openai tool calls without content", types.RelayFormatOpenAI, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c","type":"function"}]}}]}`, true},
		{"openai refusal", types.RelayFormatOpenAI, `{"choices":[{"message":{"role":"assistant","refusal":"I can't"}}]}`, true},
		{"claude empty content", types.RelayFormatClaude, `{"type":"message","content":[],"stop_reason":"end_turn"}`, false},
		{"claude text", types.RelayFormatClaude, `{"type":"message","content":[{"type":"text","text":"Hi"}]}`, true},
		{"gemini no parts", types.RelayFormatGemini, `{"candidates":[{"content":{"role":"model"},"finishReason":"STOP"}]}`, false},
		{"gemini thought only", types.RelayFormatGemini, `{"candidates":[{"content":{"parts":[{"text":"plan","thought":true}]}}]}`, true},
		{"responses empty", types.RelayFormatOpenAIResponses, `{"object":"response","output":[{"type":"reasoning","summary":[]}]}`, false},
		{"responses reasoning summary", types.RelayFormatOpenAIResponses, `{"object":"response","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"plan"}]}]}`, true},
		{"not json", types.RelayFormatOpenAI, `upstream said nothing`, false},
	}
	for _, test := range bodies {
		t.Run("body "+test.name, func(t *testing.T) {
			assert.Equal(t, test.output, responseHasOutput(test.format, []byte(test.body)))
		})
	}
}

// guardedRequest returns a request context whose channel has "error on empty
// output" on, for the given client format and relay mode.
func guardedRequest(t *testing.T, format types.RelayFormat, mode int) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{RelayFormat: format, RelayMode: mode}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelExtendSetting: dto.ChannelExtendSettings{EmptyOutputError: true}}
	return c, recorder, info
}

// writeSSE writes like the relay stream helpers: event-stream headers once,
// then each event followed by a flush.
func writeSSE(c *gin.Context, events ...string) {
	if _, set := c.Get("event_stream_headers_set"); !set {
		c.Set("event_stream_headers_set", true)
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
	}
	for _, event := range events {
		_, _ = c.Writer.Write([]byte(event))
		c.Writer.Flush()
	}
}

const (
	openAIRoleChunk    = `data: {"choices":[{"delta":{"role":"assistant","content":""}}]}` + "\n\n"
	openAIContentChunk = `data: {"choices":[{"delta":{"content":"Hello"}}]}` + "\n\n"
	openAIUsageChunk   = `data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":0}}` + "\n\n"
	sseDone            = "data: [DONE]\n\n"
	ssePing            = ": PING\n\n"
)

func TestEmptyOutputGuardStreams(t *testing.T) {
	t.Run("a stream with output reaches the client unchanged", func(t *testing.T) {
		c, recorder, info := guardedRequest(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
		guard := GuardEmptyOutput(c, info)
		require.NotNil(t, guard)
		writeSSE(c, openAIRoleChunk)
		assert.Empty(t, recorder.Body.String(), "events before the first output are held")
		assert.False(t, c.Writer.Written(), "a held response may still be retried")
		writeSSE(c, openAIContentChunk, openAIUsageChunk, sseDone)

		assert.Nil(t, guard.Finish(nil))
		assert.Equal(t, openAIRoleChunk+openAIContentChunk+openAIUsageChunk+sseDone, recorder.Body.String())
		assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	})

	t.Run("a stream without output becomes a retryable error and nothing is sent", func(t *testing.T) {
		c, recorder, info := guardedRequest(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
		guard := GuardEmptyOutput(c, info)
		writeSSE(c, openAIRoleChunk, openAIUsageChunk, sseDone)

		err := guard.Finish(nil)
		require.NotNil(t, err)
		assert.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode())
		assert.Equal(t, http.StatusInternalServerError, err.StatusCode)
		assert.False(t, types.IsSkipRetryError(err), "another channel may answer instead")
		assert.False(t, c.Writer.Written())
		assert.Empty(t, recorder.Body.String())
		assert.Empty(t, c.Writer.Header().Get("Content-Type"), "the error is not sent as an event stream")
		_, flagged := c.Get("event_stream_headers_set")
		assert.False(t, flagged, "a retry on another channel sets the stream headers again")
		assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyRelayErrorStreamed))
	})

	t.Run("after a keepalive reached the client an empty stream ends with an error event", func(t *testing.T) {
		c, recorder, info := guardedRequest(t, types.RelayFormatClaude, relayconstant.RelayModeUnknown)
		guard := GuardEmptyOutput(c, info)
		writeSSE(c, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\n", ssePing,
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

		err := guard.Finish(nil)
		require.NotNil(t, err)
		assert.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode())
		assert.True(t, types.IsSkipRetryError(err), "a started response is never followed by another channel's")
		body := recorder.Body.String()
		assert.True(t, len(body) > len(ssePing) && body[:len(ssePing)] == ssePing, body)
		assert.Contains(t, body, "event: error\ndata: {\"error\":")
		assert.NotContains(t, body, "message_stop", "held events never reach the client")
		assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyRelayErrorStreamed), "the relay must not append a JSON error")
	})

	t.Run("a client that left keeps the old behaviour", func(t *testing.T) {
		c, recorder, info := guardedRequest(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
		ctx, cancel := context.WithCancel(context.Background())
		c.Request = c.Request.WithContext(ctx)
		guard := GuardEmptyOutput(c, info)
		writeSSE(c, openAIRoleChunk)
		cancel()

		assert.Nil(t, guard.Finish(nil))
		assert.Equal(t, openAIRoleChunk, recorder.Body.String())
	})

	t.Run("an upstream error is returned as it was, with nothing sent", func(t *testing.T) {
		c, recorder, info := guardedRequest(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
		guard := GuardEmptyOutput(c, info)
		writeSSE(c, openAIRoleChunk)
		upstreamErr := types.NewOpenAIError(assert.AnError, types.ErrorCodeBadResponse, http.StatusBadGateway)

		assert.Same(t, upstreamErr, guard.Finish(upstreamErr))
		assert.Empty(t, recorder.Body.String())
	})
}

func TestEmptyOutputGuardBodies(t *testing.T) {
	for _, test := range []struct {
		name   string
		body   string
		output bool
	}{
		{"with output", `{"choices":[{"message":{"role":"assistant","content":"Hello"}}]}`, true},
		{"without output", `{"choices":[{"message":{"role":"assistant","content":""}}],"usage":{"completion_tokens":0}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, recorder, info := guardedRequest(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
			guard := GuardEmptyOutput(c, info)
			c.Data(http.StatusOK, "application/json", []byte(test.body))
			assert.Empty(t, recorder.Body.String(), "a body is checked whole before it is sent")

			err := guard.Finish(nil)
			if test.output {
				assert.Nil(t, err)
				assert.Equal(t, test.body, recorder.Body.String())
				assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
				return
			}
			require.NotNil(t, err)
			assert.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode())
			assert.Empty(t, recorder.Body.String())
			assert.Empty(t, c.Writer.Header().Get("Content-Type"))
		})
	}
}

func TestEmptyOutputGuardOnlyCoversTextGenerationWhenEnabled(t *testing.T) {
	c, _, info := guardedRequest(t, types.RelayFormatOpenAI, relayconstant.RelayModeEmbeddings)
	assert.Nil(t, GuardEmptyOutput(c, info), "embeddings have no output to check")
	c, _, info = guardedRequest(t, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponsesCompact)
	assert.Nil(t, GuardEmptyOutput(c, info))
	c, _, info = guardedRequest(t, types.RelayFormatClaude, relayconstant.RelayModeClaudeCountTokens)
	assert.Nil(t, GuardEmptyOutput(c, info))
	c, _, info = guardedRequest(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	info.ChannelExtendSetting.EmptyOutputError = false
	original := c.Writer
	guard := GuardEmptyOutput(c, info)
	assert.Nil(t, guard)
	assert.Same(t, original, c.Writer, "a channel without the switch keeps its writer")
	assert.Nil(t, guard.Finish(nil))
}
