package relay

import (
	"fmt"
	"io"
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ClaudeCountTokensHelper 将 /v1/messages/count_tokens 透传到 Anthropic 渠道。
// 请求体原样转发（仅在渠道配置了模型映射时重写 model 字段），上游 JSON 响应逐字返回；
// 不扣费，成功后写一条零额消费日志。
func ClaudeCountTokensHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)

	// 渠道选择各路径已按「Anthropic 类型 + count_tokens 开关」过滤，此处为纵深防御
	if info.ChannelType != constant.ChannelTypeAnthropic {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("channel type %d does not support count_tokens, only Anthropic channels are allowed", info.ChannelType),
			types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}

	// 仅解析映射后的模型名，请求体不经 DTO 反序列化重建，避免丢失未覆盖字段
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	var requestBody io.Reader
	if info.IsModelMapped {
		bodyBytes, err := storage.Bytes()
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		mappedBody, err := sjson.SetBytes(bodyBytes, "model", info.UpstreamModelName)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		body, closer, err := relaycommon.NewOutboundJSONBody(mappedBody)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		requestBody = body
	} else {
		requestBody = common.NewReplayableBodyReader(storage)
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	httpResp := resp.(*http.Response)
	if httpResp.StatusCode != http.StatusOK {
		newAPIError := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	defer service.CloseResponseBodyGracefully(httpResp)
	responseBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	// input_tokens 仅用于日志展示，上游值不可信，饱和到 int32 范围
	inputTokens := gjson.GetBytes(responseBody, "input_tokens").Int()
	if inputTokens < 0 {
		inputTokens = 0
	} else if inputTokens > math.MaxInt32 {
		inputTokens = math.MaxInt32
	}

	service.IOCopyBytesGracefully(c, httpResp, responseBody)

	service.PostClaudeCountTokensLog(c, info, int(inputTokens))
	return nil
}
