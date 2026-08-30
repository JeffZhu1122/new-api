package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// minInputTextFields are the request fields whose text participates in the
// min_input_tokens estimate, covering OpenAI chat/responses, Claude messages
// and Gemini generateContent bodies. Unknown fields are simply absent.
var minInputTextFields = []string{
	"messages",
	"system",
	"instructions",
	"input",
	"contents",
	"systemInstruction",
	"system_instruction",
}

// minInputEstimateSupportedPath limits min_input_tokens routing to text
// relay formats where the input can be estimated from the JSON body. Other
// paths (multipart audio, images, tasks, count_tokens probes) fail open and
// ignore channel minimums.
func minInputEstimateSupportedPath(path string) bool {
	if strings.HasSuffix(path, "/chat/completions") {
		return true
	}
	if strings.HasSuffix(path, "/v1/messages") {
		return true
	}
	if strings.HasSuffix(path, "/v1/responses") {
		return true
	}
	return strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent")
}

// EstimateMinInputTokens returns the estimated input token count used by the
// min_input_tokens channel filter, and whether an estimate is available for
// this request. It runs once per request in the distributor, before channel
// selection; the result is reused across retries via the channel constraints.
func EstimateMinInputTokens(c *gin.Context, modelName string) (int, bool) {
	if c == nil || c.Request == nil || modelName == "" {
		return 0, false
	}
	if !minInputEstimateSupportedPath(c.Request.URL.Path) {
		return 0, false
	}
	if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		return 0, false
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return 0, false
	}
	body, err := storage.Bytes()
	if err != nil {
		return 0, false
	}
	return estimateInputTokensFromJSON(body, modelName)
}

func estimateInputTokensFromJSON(body []byte, modelName string) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}
	var sb strings.Builder
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return 0, false
	}
	for _, field := range minInputTextFields {
		if value := root.Get(field); value.Exists() {
			collectRelayText(value, &sb, 0)
		}
	}
	return EstimateTokenByModel(modelName, sb.String()), true
}

// collectRelayText gathers user-visible input text: plain strings, message
// arrays, and nested content/parts structures ({type:"text", text:"..."} and
// Gemini parts). Tool definitions and non-text parts are intentionally not
// counted; the estimate covers the input body only.
func collectRelayText(value gjson.Result, sb *strings.Builder, depth int) {
	if depth > 8 {
		return
	}
	switch {
	case value.Type == gjson.String:
		sb.WriteString(value.String())
		sb.WriteByte('\n')
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectRelayText(item, sb, depth+1)
			return true
		})
	case value.IsObject():
		for _, key := range []string{"text", "content", "parts"} {
			if sub := value.Get(key); sub.Exists() {
				collectRelayText(sub, sb, depth+1)
			}
		}
	}
}
