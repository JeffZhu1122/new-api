package claude

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	result, err := service.ConvertRequest(c, info, types.RelayFormatClaude, request)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request.MaxTokens != nil && *request.MaxTokens == 0 {
		request.MaxTokens = nil
	}
	if err := relayconvert.ApplyClaudeThinkingModel(request, info); err != nil {
		return nil, err
	}
	if request.MaxTokens == nil {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(request.Model))
		request.MaxTokens = &defaultMaxTokens
	}
	// ApplyClaudeThinkingModel no longer rewrites request.Model. Do not write
	// a still-suffixed name back over the entry-normalized UpstreamModelName
	// (AWS/Vertex look up getAwsModelID / claudeModelMap from that field).
	if info.UpstreamModelName == "" {
		info.UpstreamModelName = request.Model
	}
	return request, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	requestURL := fmt.Sprintf("%s/v1/messages", info.ChannelBaseUrl)
	if info.RelayMode == relayconstant.RelayModeClaudeCountTokens {
		requestURL = fmt.Sprintf("%s/v1/messages/count_tokens", info.ChannelBaseUrl)
	}
	if !shouldAppendClaudeBetaQuery(info) {
		return requestURL, nil
	}

	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	query := parsedURL.Query()
	query.Set("beta", "true")
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func shouldAppendClaudeBetaQuery(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if info.IsClaudeBetaQuery {
		return true
	}
	if info.ChannelOtherSettings.ClaudeBetaQuery {
		return true
	}
	return false
}

func CommonClaudeHeadersOperation(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) {
	// common headers operation
	anthropicBeta := c.Request.Header.Get("anthropic-beta")
	if anthropicBeta != "" {
		req.Set("anthropic-beta", anthropicBeta)
	}
	model_setting.GetClaudeSettings().WriteHeaders(info.OriginModelName, req)
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	anthropicVersion := c.Request.Header.Get("anthropic-version")
	if anthropicVersion == "" {
		anthropicVersion = "2023-06-01"
	}
	req.Set("anthropic-version", anthropicVersion)
	CommonClaudeHeadersOperation(c, req, info)
	// 在 CommonClaudeHeadersOperation 之后设置：它会用客户端的 anthropic-beta
	// 覆盖请求头，OAuth 模式需要把 oauth beta 标识合并进去。
	// 复用本适配器的其他渠道类型（DeepSeek、Moonshot 等）始终走 x-api-key。
	authMode := ""
	if info.ChannelType == constant.ChannelTypeAnthropic {
		authMode = info.ChannelExtendSetting.ClaudeAuthMode
	}
	SetClaudeAuthHeader(req, authMode, info.ApiKey)
	return nil
}

// ClaudeOAuthBeta is the anthropic-beta flag required for OAuth access tokens.
const ClaudeOAuthBeta = "oauth-2025-04-20"

// SetClaudeAuthHeader writes the upstream credential using the scheme the
// channel's claude_auth_mode resolves to for this key: x-api-key for standard
// API keys, or Authorization: Bearer plus the oauth anthropic-beta flag for
// OAuth access tokens. The two are mutually exclusive upstream.
func SetClaudeAuthHeader(req *http.Header, mode string, key string) {
	if dto.ResolveClaudeAuthMode(mode, key) != dto.ClaudeAuthModeOAuth {
		req.Del("Authorization")
		req.Set("x-api-key", key)
		return
	}
	req.Del("x-api-key")
	req.Set("Authorization", "Bearer "+key)
	betas := []string{ClaudeOAuthBeta}
	for _, value := range req.Values("anthropic-beta") {
		for item := range strings.SplitSeq(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" && item != ClaudeOAuthBeta {
				betas = append(betas, item)
			}
		}
	}
	req.Set("anthropic-beta", strings.Join(betas, ","))
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	result, err := service.ConvertRequest(c, info, types.RelayFormatClaude, request)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	result, err := service.ConvertRequest(c, info, types.RelayFormatClaude, &request)
	if err != nil {
		return nil, err
	}
	claudeRequest, ok := result.Value.(*dto.ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("expected Anthropic Messages request, got %T", result.Value)
	}
	return claudeRequest, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	info.FinalRequestRelayFormat = types.RelayFormatClaude
	if info.RelayFormat == types.RelayFormatOpenAIResponses && info.IsStream {
		return ClaudeResponsesStreamHandler(c, resp, info)
	}
	if info.IsStream {
		return ClaudeStreamHandler(c, resp, info)
	} else {
		return ClaudeHandler(c, resp, info)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
