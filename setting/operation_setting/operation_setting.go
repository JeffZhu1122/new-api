package operation_setting

import "strings"

var DemoSiteEnabled = false
var SelfUseModeEnabled = false

var AutomaticDisableKeywords = []string{
	"Your credit balance is too low",
	"This organization has been disabled.",
	"You exceeded your current quota",
	"Permission denied",
	"The security token included in the request is invalid",
	"Operation not allowed",
	"Your account is not authorized",
}

var AutomaticRetryKeywordsEnabled = false
var AutomaticRetryKeywords = []string{}

// RetryAvoidFailedChannelsEnabled 开启后，重试时排除本次请求中已失败的渠道（多 key 渠道除外，保留换 key 重试）
var RetryAvoidFailedChannelsEnabled = false

const defaultRetryAvoidFailedChannelsStatusCode = 429
const defaultRetryAvoidFailedChannelsMessage = "all available channels for model {model} have failed in this request, no channels left to retry"

// 渠道耗尽（排除后无渠道可选）时返回的 HTTP 状态码与错误信息，均可在运营设置中配置
var RetryAvoidFailedChannelsStatusCode = defaultRetryAvoidFailedChannelsStatusCode
var RetryAvoidFailedChannelsErrorMessage = defaultRetryAvoidFailedChannelsMessage

func RetryAvoidFailedChannelsHTTPStatusCode() int {
	if RetryAvoidFailedChannelsStatusCode < 100 || RetryAvoidFailedChannelsStatusCode > 599 {
		return defaultRetryAvoidFailedChannelsStatusCode
	}
	return RetryAvoidFailedChannelsStatusCode
}

// RetryAvoidFailedChannelsMessage 渲染耗尽错误信息，{model} 占位符替换为模型名；空配置回退到默认信息
func RetryAvoidFailedChannelsMessage(modelName string) string {
	message := strings.TrimSpace(RetryAvoidFailedChannelsErrorMessage)
	if message == "" {
		message = defaultRetryAvoidFailedChannelsMessage
	}
	return strings.ReplaceAll(message, "{model}", modelName)
}

func AutomaticDisableKeywordsToString() string {
	return strings.Join(AutomaticDisableKeywords, "\n")
}

func AutomaticDisableKeywordsFromString(s string) {
	AutomaticDisableKeywords = parseKeywordLines(s)
}

func AutomaticRetryKeywordsToString() string {
	return strings.Join(AutomaticRetryKeywords, "\n")
}

func AutomaticRetryKeywordsFromString(s string) {
	AutomaticRetryKeywords = parseKeywordLines(s)
}

func parseKeywordLines(s string) []string {
	keywords := []string{}
	for _, k := range strings.Split(s, "\n") {
		k = strings.TrimSpace(k)
		k = strings.ToLower(k)
		if k != "" {
			keywords = append(keywords, k)
		}
	}
	return keywords
}
