package gemini

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
)

func GeminiTextGenerationHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// 读取响应体
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	logger.LogDebug(c, "Gemini native response body: %s", responseBody)

	// 解析为 Gemini 原生响应格式
	var geminiResponse dto.GeminiChatResponse
	err = common.Unmarshal(responseBody, &geminiResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	info.ObserveResponseModel(ojson.GetBytes(responseBody, "modelVersion").Str)
	var grounding geminiGroundingCounter
	grounding.observe(&geminiResponse)
	grounding.commit(info)
	countGeminiBillableFunctionCalls(info, &geminiResponse)

	if len(geminiResponse.Candidates) == 0 && geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
		info.PerformanceBusinessRejection = true
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
	}

	// 计算使用量（优先上游 UsageMetadata，缺失时本地估算并保留 Gemini 计费语义）
	usage := buildUsageFromGeminiResponse(c, info, &geminiResponse)

	if info.ChannelSetting.GeminiOmitNullFields {
		if body, changed := omitGeminiNullFields(string(responseBody)); changed {
			responseBody = []byte(body)
		}
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)

	return &usage, nil
}

// geminiStructMembers name the Gemini fields of type google.protobuf.Struct
// (functionCall.args, functionResponse.response). They carry the caller's own
// JSON, where null is a value, so they are copied as sent.
var geminiStructMembers = []string{"args", "response"}

// omitGeminiNullFields removes the object members whose value is null from a
// native Gemini response and reports whether it rebuilt the document. Gemini
// sends no such members, so removing them changes nothing a Gemini client can
// rely on. A document without the text "null" is returned as it is. Otherwise
// array elements, member order and the text of every other value are kept,
// and the rebuilt document has no whitespace between tokens.
func omitGeminiNullFields(data string) (string, bool) {
	if !strings.Contains(data, "null") {
		return data, false
	}
	root := ojson.Parse(data)
	if root.Type != ojson.JSON {
		return data, false
	}
	var out strings.Builder
	out.Grow(len(data))
	writeGeminiValueWithoutNulls(&out, root)
	return out.String(), true
}

func writeGeminiValueWithoutNulls(out *strings.Builder, value ojson.Result) {
	if value.Type != ojson.JSON {
		out.WriteString(value.Raw)
		return
	}
	object := value.IsObject()
	closing := byte(']')
	if object {
		closing = '}'
	}
	out.WriteByte(value.Raw[0])
	first := true
	value.ForEach(func(key, member ojson.Result) bool {
		if object && member.Type == ojson.Null {
			return true
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		if !object {
			writeGeminiValueWithoutNulls(out, member)
			return true
		}
		out.WriteString(key.Raw)
		out.WriteByte(':')
		if slices.Contains(geminiStructMembers, key.Str) {
			out.WriteString(member.Raw)
		} else {
			writeGeminiValueWithoutNulls(out, member)
		}
		return true
	})
	out.WriteByte(closing)
}

func NativeGeminiEmbeddingHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	logger.LogDebug(c, "Gemini native embedding response body: %s", responseBody)

	var metadata *dto.GeminiEmbeddingUsageMetadata
	if info.IsGeminiBatchEmbedding {
		var geminiResponse dto.GeminiBatchEmbeddingResponse
		err = common.Unmarshal(responseBody, &geminiResponse)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		metadata = geminiResponse.UsageMetadata
	} else {
		var geminiResponse dto.GeminiEmbeddingResponse
		err = common.Unmarshal(responseBody, &geminiResponse)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		metadata = geminiResponse.UsageMetadata
	}

	var usage *dto.Usage
	if upstream := metadata.GeminiUsageMetadata(); upstream != nil {
		upstreamUsage := buildUsageFromGeminiMetadata(upstream, info.GetEstimatePromptTokens())
		usage = &upstreamUsage
	} else {
		usage = service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	return usage, nil
}

func GeminiTextGenerationStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	helper.SetEventStreamHeaders(c)

	return geminiStreamHandler(c, info, resp, func(data string, geminiResponse *dto.GeminiChatResponse) bool {
		if info.ChannelSetting.GeminiOmitNullFields {
			data, _ = omitGeminiNullFields(data)
		}
		err := helper.StringData(c, data)
		if err != nil {
			logger.LogError(c, common.LogText("failed to write stream data: %s", err.Error()))
			return false
		}
		info.SendResponseCount++
		return true
	})
}
