package service

import (
	"bytes"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// EmptyOutputGuard holds a text generation response back from the client
// until it shows output, for channels with "error on empty output" on. A
// response that ends without any output becomes an empty_response error, so
// the relay retries it on another channel and refunds the pre-consumed
// quota, as for any upstream error.
//
// A non-stream body is kept whole and checked once. A stream is held event by
// event only until the first event with output, then flows straight through.
// Keepalive comments pass at once; after one has reached the client the
// response has started, so an empty stream ends with an error event instead
// of a retry.
type EmptyOutputGuard struct {
	c        *gin.Context
	format   types.RelayFormat
	original gin.ResponseWriter
	headers  http.Header

	mu        sync.Mutex
	decided   bool
	stream    bool
	status    int
	committed bool
	released  bool
	pending   []byte
	held      [][]byte
	body      bytes.Buffer
}

// GuardEmptyOutput starts guarding the response about to be written by
// DoResponse, or returns nil when the channel does not ask for it or the
// request is not text generation. Callers pass DoResponse's error through
// Finish, which is safe on a nil guard.
func GuardEmptyOutput(c *gin.Context, info *relaycommon.RelayInfo) *EmptyOutputGuard {
	if !info.ChannelExtendSetting.EmptyOutputError {
		return nil
	}
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if info.RelayMode != relayconstant.RelayModeChatCompletions && info.RelayMode != relayconstant.RelayModeCompletions {
			return nil
		}
	case types.RelayFormatOpenAIResponses:
		if info.RelayMode != relayconstant.RelayModeResponses {
			return nil
		}
	case types.RelayFormatClaude:
		if info.RelayMode == relayconstant.RelayModeClaudeCountTokens {
			return nil
		}
	case types.RelayFormatGemini:
	default:
		return nil
	}
	guard := &EmptyOutputGuard{c: c, format: info.RelayFormat, original: c.Writer, headers: c.Writer.Header().Clone()}
	c.Writer = &emptyOutputWriter{ResponseWriter: c.Writer, guard: guard}
	return guard
}

// Finish ends the guard after DoResponse and returns the error the relay
// should report: DoResponse's own error, nil when the response had output
// (or the client left), or an empty_response error.
func (g *EmptyOutputGuard) Finish(respErr *types.NewAPIError) *types.NewAPIError {
	if g == nil {
		return respErr
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.c.Writer = g.original
	clientGone := g.c.Request != nil && g.c.Request.Context().Err() != nil
	switch {
	case g.released:
		return respErr
	case respErr != nil:
		g.drop()
		return respErr
	case clientGone || (!g.stream && responseHasOutput(g.format, g.body.Bytes())):
		g.flushHeld()
		return nil
	}
	if !g.committed {
		g.drop()
		return types.NewOpenAIError(errors.New("upstream returned no output"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}
	// The client already holds part of this response: another channel's
	// reply cannot follow it.
	emptyErr := types.NewOpenAIError(errors.New("upstream returned no output"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	g.writeStreamError(emptyErr)
	common.SetContextKey(g.c, constant.ContextKeyRelayErrorStreamed, true)
	return emptyErr
}

func (g *EmptyOutputGuard) write(data []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.released {
		return g.original.Write(data)
	}
	if !g.decided {
		g.decided = true
		g.stream = strings.HasPrefix(g.original.Header().Get("Content-Type"), "text/event-stream")
	}
	if !g.stream {
		return g.body.Write(data)
	}
	g.pending = append(g.pending, data...)
	for {
		end := sseEventEnd(g.pending)
		if end < 0 {
			return len(data), nil
		}
		event := slices.Clone(g.pending[:end])
		g.pending = g.pending[end:]
		switch {
		case sseIsComment(event):
			// A keepalive keeps the connection open; it carries no output.
			if err := g.writeThrough(event); err != nil {
				return 0, err
			}
		case streamEventHasOutput(g.format, event):
			return len(data), g.release(event)
		default:
			g.held = append(g.held, event)
		}
	}
}

// writeThrough sends bytes to the client while the response is still held.
func (g *EmptyOutputGuard) writeThrough(data []byte) error {
	if !g.committed {
		g.committed = true
		if g.status != 0 {
			g.original.WriteHeader(g.status)
		}
	}
	_, err := g.original.Write(data)
	return err
}

// release sends the held events and the event with the first output, then
// lets every later write through.
func (g *EmptyOutputGuard) release(event []byte) error {
	held := append(g.held, event, g.pending)
	g.held, g.pending = nil, nil
	for _, data := range held {
		if err := g.writeThrough(data); err != nil {
			return err
		}
	}
	g.released = true
	return nil
}

// flushHeld sends what was held, unchanged.
func (g *EmptyOutputGuard) flushHeld() {
	parts := append(g.held, g.pending)
	if !g.stream {
		parts = [][]byte{g.body.Bytes()}
	}
	for _, data := range parts {
		if len(data) == 0 && g.committed {
			continue
		}
		if err := g.writeThrough(data); err != nil {
			return
		}
	}
	g.original.Flush()
}

// drop discards what was held. A response that never reached the client gets
// its headers back, so an error or a retry on another channel starts clean.
func (g *EmptyOutputGuard) drop() {
	g.held, g.pending = nil, nil
	g.body.Reset()
	if g.committed {
		return
	}
	header := g.original.Header()
	for key := range header {
		if _, kept := g.headers[key]; !kept {
			header.Del(key)
		}
	}
	for key, values := range g.headers {
		header[key] = values
	}
	// SetEventStreamHeaders skips a response it already marked; the retry
	// must set them again.
	delete(g.c.Keys, "event_stream_headers_set")
}

// writeStreamError ends a started stream with an error event in the client's
// format.
func (g *EmptyOutputGuard) writeStreamError(err *types.NewAPIError) {
	var event string
	switch g.format {
	case types.RelayFormatClaude:
		data, _ := common.Marshal(map[string]any{"type": "error", "error": err.ToClaudeError()})
		event = "event: error\ndata: " + string(data) + "\n\n"
	case types.RelayFormatOpenAIResponses:
		data, _ := common.Marshal(map[string]any{"type": "error", "code": string(err.GetErrorCode()), "message": err.Error(), "param": nil})
		event = "event: error\ndata: " + string(data) + "\n\n"
	case types.RelayFormatGemini:
		data, _ := common.Marshal(map[string]any{"error": map[string]any{"code": err.StatusCode, "message": err.Error(), "status": "INTERNAL"}})
		event = "data: " + string(data) + "\n\n"
	default:
		data, _ := common.Marshal(map[string]any{"error": err.ToOpenAIError()})
		event = "data: " + string(data) + "\n\n"
	}
	if _, writeErr := g.original.Write([]byte(event)); writeErr == nil {
		g.original.Flush()
	}
}

// emptyOutputWriter routes the response writes of DoResponse through the
// guard. Headers, hijacking and the underlying connection stay the client's.
type emptyOutputWriter struct {
	gin.ResponseWriter
	guard *EmptyOutputGuard
}

func (w *emptyOutputWriter) Write(data []byte) (int, error) {
	return w.guard.write(data)
}

func (w *emptyOutputWriter) WriteString(s string) (int, error) {
	return w.guard.write([]byte(s))
}

func (w *emptyOutputWriter) WriteHeader(code int) {
	w.guard.mu.Lock()
	defer w.guard.mu.Unlock()
	if w.guard.committed {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.guard.status = code
}

func (w *emptyOutputWriter) WriteHeaderNow() {
	w.guard.mu.Lock()
	defer w.guard.mu.Unlock()
	if w.guard.committed {
		w.ResponseWriter.WriteHeaderNow()
	}
}

func (w *emptyOutputWriter) Status() int {
	w.guard.mu.Lock()
	defer w.guard.mu.Unlock()
	if !w.guard.committed && w.guard.status != 0 {
		return w.guard.status
	}
	return w.ResponseWriter.Status()
}

// Written reports false while the response is held, so the relay still sees
// a response it may retry.
func (w *emptyOutputWriter) Written() bool {
	w.guard.mu.Lock()
	defer w.guard.mu.Unlock()
	return w.guard.committed && w.ResponseWriter.Written()
}

func (w *emptyOutputWriter) Flush() {
	w.guard.mu.Lock()
	defer w.guard.mu.Unlock()
	if w.guard.committed {
		w.ResponseWriter.Flush()
	}
}

// Unwrap lets http.ResponseController reach the connection (write deadlines).
func (w *emptyOutputWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// sseEventEnd returns the length of the first complete SSE event in buf,
// separator included, or -1.
func sseEventEnd(buf []byte) int {
	for i := range buf {
		if buf[i] != '\n' {
			continue
		}
		if i+1 < len(buf) && buf[i+1] == '\n' {
			return i + 2
		}
		if i+2 < len(buf) && buf[i+1] == '\r' && buf[i+2] == '\n' {
			return i + 3
		}
	}
	return -1
}

// sseIsComment reports an event made only of ":" comment lines, such as a
// keepalive ping.
func sseIsComment(event []byte) bool {
	comment := false
	for line := range strings.SplitSeq(string(event), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, ":") {
			return false
		}
		comment = true
	}
	return comment
}

// streamEventHasOutput reports whether one SSE event the client receives
// carries model output.
func streamEventHasOutput(format types.RelayFormat, event []byte) bool {
	var data strings.Builder
	for line := range strings.SplitSeq(string(event), "\n") {
		value, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "data:")
		if !ok {
			continue
		}
		if data.Len() > 0 {
			data.WriteByte('\n')
		}
		data.WriteString(strings.TrimPrefix(value, " "))
	}
	payload := strings.TrimSpace(data.String())
	if payload == "" || payload == "[DONE]" || !gjson.Valid(payload) {
		return false
	}
	chunk := gjson.Parse(payload)
	switch format {
	case types.RelayFormatClaude:
		switch chunk.Get("type").String() {
		case "content_block_start":
			return claudeBlockHasOutput(chunk.Get("content_block"))
		case "content_block_delta":
			delta := chunk.Get("delta")
			return delta.Get("text").String() != "" || delta.Get("thinking").String() != "" || delta.Get("partial_json").String() != ""
		case "message_start":
			return claudeContentHasOutput(chunk.Get("message.content"))
		}
		return false
	case types.RelayFormatGemini:
		return geminiHasOutput(chunk)
	case types.RelayFormatOpenAIResponses:
		eventType := chunk.Get("type").String()
		switch {
		case strings.HasSuffix(eventType, ".delta"):
			return chunk.Get("delta").String() != ""
		case eventType == "response.output_item.added" || eventType == "response.output_item.done":
			return responsesItemHasOutput(chunk.Get("item"))
		case eventType == "response.completed" || eventType == "response.incomplete":
			return responsesOutputHasOutput(chunk.Get("response.output"))
		}
		return false
	default:
		return openAIChoicesHaveOutput(chunk, "delta")
	}
}

// responseHasOutput reports whether a whole non-stream response body the
// client receives carries model output.
func responseHasOutput(format types.RelayFormat, body []byte) bool {
	if !gjson.ValidBytes(body) {
		return false
	}
	response := gjson.ParseBytes(body)
	switch format {
	case types.RelayFormatClaude:
		return claudeContentHasOutput(response.Get("content"))
	case types.RelayFormatGemini:
		return geminiHasOutput(response)
	case types.RelayFormatOpenAIResponses:
		return responsesOutputHasOutput(response.Get("output"))
	default:
		return openAIChoicesHaveOutput(response, "message")
	}
}

// openAIChoicesHaveOutput checks Chat Completions choices: any field of the
// delta or message besides its role (content, reasoning, tool calls,
// refusal, audio, images), or the text of a legacy completion.
func openAIChoicesHaveOutput(response gjson.Result, field string) bool {
	for _, choice := range response.Get("choices").Array() {
		if choice.Get("text").String() != "" {
			return true
		}
		found := false
		choice.Get(field).ForEach(func(key, value gjson.Result) bool {
			if key.String() != "role" && key.String() != "annotations" && jsonValueHasContent(value) {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func jsonValueHasContent(value gjson.Result) bool {
	switch value.Type {
	case gjson.String:
		return value.String() != ""
	case gjson.JSON:
		if value.IsArray() {
			return len(value.Array()) > 0
		}
		return len(value.Map()) > 0
	}
	return false
}

func claudeContentHasOutput(content gjson.Result) bool {
	return slices.ContainsFunc(content.Array(), claudeBlockHasOutput)
}

// claudeBlockHasOutput: a text or thinking block counts once it has text;
// any other block (tool use, redacted thinking, server tool results) is
// output by itself.
func claudeBlockHasOutput(block gjson.Result) bool {
	switch block.Get("type").String() {
	case "":
		return false
	case "text":
		return block.Get("text").String() != ""
	case "thinking":
		return block.Get("thinking").String() != ""
	}
	return true
}

// geminiHasOutput checks candidate parts: text (thoughts included), function
// calls, inline or file data and code execution. A non-SSE stream is an
// array of such responses.
func geminiHasOutput(response gjson.Result) bool {
	if response.IsArray() {
		return slices.ContainsFunc(response.Array(), geminiHasOutput)
	}
	for _, part := range response.Get("candidates.#.content.parts|@flatten").Array() {
		if part.Get("text").String() != "" {
			return true
		}
		for _, key := range []string{"functionCall", "inlineData", "fileData", "executableCode", "codeExecutionResult"} {
			if part.Get(key).Exists() {
				return true
			}
		}
	}
	return false
}

func responsesOutputHasOutput(output gjson.Result) bool {
	return slices.ContainsFunc(output.Array(), responsesItemHasOutput)
}

// responsesItemHasOutput: a message counts once it has text or a refusal, a
// reasoning item once it has summary or reasoning text; any other item (tool
// calls, image generation, searches) is output by itself.
func responsesItemHasOutput(item gjson.Result) bool {
	switch item.Get("type").String() {
	case "":
		return false
	case "message":
		for _, part := range item.Get("content").Array() {
			if part.Get("text").String() != "" || part.Get("refusal").String() != "" {
				return true
			}
		}
		return false
	case "reasoning":
		for _, path := range []string{"summary", "content"} {
			for _, part := range item.Get(path).Array() {
				if part.Get("text").String() != "" {
					return true
				}
			}
		}
		return false
	}
	return true
}
