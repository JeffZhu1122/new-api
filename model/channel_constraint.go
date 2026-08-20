package model

import (
	"slices"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
)

var filterEvalOrder = []dto.ChannelFilterKind{
	dto.FilterRequestPath,
	dto.FilterTaskPluginIdentity,
	dto.FilterInputTokens,
	dto.FilterResponsesWebSocket,
}

// ChannelSatisfiesFilters reports whether ch passes every filter.
// On false, it returns the kind of the first violated filter (request_path
// then task_plugin_identity) for error attribution.
func ChannelSatisfiesFilters(ch *Channel, modelName string, filters []dto.ChannelFilter) (bool, dto.ChannelFilterKind) {
	if ch == nil {
		return false, ""
	}
	for _, kind := range filterEvalOrder {
		for _, filter := range filters {
			if filter.Kind != kind {
				continue
			}
			if !channelMatchesFilter(ch, modelName, filter) {
				return false, kind
			}
		}
	}
	return true, ""
}

// filterCandidateIDs applies filters to a cached candidate id list.
// Caller must hold channelSyncLock (read lock). The input slice is never mutated.
// A missing id in channelsIDM is kept for request_path (downstream consistency
// error) and dropped for task_plugin_identity, matching the previous filters.
func filterCandidateIDs(ids []int, modelName string, filters []dto.ChannelFilter) (kept []int, emptiedBy dto.ChannelFilterKind) {
	if len(ids) == 0 {
		return ids, ""
	}
	kept = ids
	for _, kind := range filterEvalOrder {
		kindFilters := filtersByKind(filters, kind)
		if len(kindFilters) == 0 {
			continue
		}
		next := make([]int, 0, len(kept))
		for _, id := range kept {
			channel, exists := channelsIDM[id]
			if candidatePassesKindFilters(channel, exists, modelName, kind, kindFilters) {
				next = append(next, id)
			}
		}
		if len(kept) > 0 && len(next) == 0 {
			return next, kind
		}
		kept = next
	}
	return kept, ""
}

func filtersByKind(filters []dto.ChannelFilter, kind dto.ChannelFilterKind) []dto.ChannelFilter {
	var matched []dto.ChannelFilter
	for _, filter := range filters {
		if filter.Kind == kind {
			matched = append(matched, filter)
		}
	}
	return matched
}

func candidatePassesKindFilters(ch *Channel, exists bool, modelName string, kind dto.ChannelFilterKind, filters []dto.ChannelFilter) bool {
	if kind == dto.FilterRequestPath && !exists {
		return true
	}
	if !exists || ch == nil {
		return false
	}
	for _, filter := range filters {
		if !channelMatchesFilter(ch, modelName, filter) {
			return false
		}
	}
	return true
}

// ChannelSupportsCountTokensPath reports whether ch may serve the free token
// counting endpoint at path: /v1/messages/count_tokens needs an Anthropic
// channel, /v1/responses/input_tokens needs an OpenAI channel, and both need
// count_tokens_enabled in the channel settings. Any other path returns false.
func ChannelSupportsCountTokensPath(ch *Channel, path string) bool {
	if ch == nil || !ch.GetOtherSettings().CountTokensEnabled {
		return false
	}
	switch {
	case constant.IsClaudeCountTokensPath(path):
		return ch.Type == constant.ChannelTypeAnthropic
	case constant.IsOpenAIInputTokensPath(path):
		return ch.Type == constant.ChannelTypeOpenAI
	default:
		return false
	}
}

func channelMatchesFilter(ch *Channel, modelName string, filter dto.ChannelFilter) bool {
	switch filter.Kind {
	case dto.FilterRequestPath:
		if filter.RequestPath == "" {
			return true
		}
		// 免费的 token 计数端点仅允许「对应厂商类型 + 开启 count_tokens 开关」的渠道
		if constant.IsCountTokensPath(filter.RequestPath) {
			return ChannelSupportsCountTokensPath(ch, filter.RequestPath)
		}
		if !constant.IsAdvancedCustomChannel(ch.Type) {
			return true
		}
		config := ch.GetOtherSettings().AdvancedCustom
		return config != nil && config.SupportsPathForModel(filter.RequestPath, modelName)
	case dto.FilterTaskPluginIdentity:
		if filter.TaskPluginKey == "" {
			return ch.Type != constant.ChannelTypeTaskPlugin
		}
		if ch.Type == constant.ChannelTypeTaskPlugin || ch.Type == constant.ChannelTypeNewAPI {
			// A New API channel serves every plugin it is extended with; the
			// pinned plugin or any shared-model candidate may execute there.
			setting := ch.GetSetting()
			return setting.BindsTaskPlugin(filter.TaskPluginKey) || slices.ContainsFunc(filter.TaskPluginKeys, setting.BindsTaskPlugin)
		}
		return slices.Contains(filter.TaskPluginChannelTypes, ch.Type)
	case dto.FilterResponsesWebSocket:
		if !ch.GetSetting().ResponsesWebSocketEnabled {
			return false
		}
		switch ch.Type {
		case constant.ChannelTypeOpenAI, constant.ChannelTypeCodex, constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI:
			return true
		case constant.ChannelTypeAdvancedCustom:
			// The session forwards native Responses events without protocol
			// conversion, so only a converter-free /v1/responses route qualifies.
			route, ok := ch.GetOtherSettings().AdvancedCustom.MatchPathForModel("/v1/responses", modelName)
			return ok && route.IsNative()
		default:
			return false
		}
	case dto.FilterInputTokens:
		// 只读 ch.ExtendConfig，禁止在此调用 GetChannelExtendSettings：
		// 内存缓存路径在持有 channelSyncLock.RLock 时执行本函数，再次 RLock
		// 会与周期性 InitChannelCache 的写锁排队形成死锁。ExtendConfig 由
		// InitChannelCache / filterAbilitiesByConstraints 预先填充。
		// min 为排他下界（须严格大于）、max 为包含上界（须小于等于）。
		if ch.ExtendConfig == nil {
			return true
		}
		if min := ch.ExtendConfig.MinInputTokens; min > 0 && filter.InputTokens <= min {
			return false
		}
		if max := ch.ExtendConfig.MaxInputTokens; max > 0 && filter.InputTokens > max {
			return false
		}
		return true
	default:
		return true
	}
}
