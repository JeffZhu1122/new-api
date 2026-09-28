package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func init() {
	model.ChannelQuotaLimitReachedHandler = notifyChannelQuotaLimitReached
}

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// notifyChannelQuotaLimitReached finishes a quota-limit auto-disable the same
// way DisableChannel does: close the channel's live WebSockets and tell the
// root user, including the figures needed to decide on a top-up.
func notifyChannelQuotaLimitReached(channel *model.Channel, settings dto.ChannelExtendSettings) {
	if shouldCloseActiveWebSocketsAfterDisable(channel.Id) {
		CloseActiveWebSocketsForChannel(channel.Id, ChannelDisabledCloseReason)
	}
	usedUSD := float64(channel.UsedQuota) / common.QuotaPerUnit
	limitUSD := float64(settings.QuotaLimit) / common.QuotaPerUnit
	subject := fmt.Sprintf("通道「%s」（#%d）已达额度上限，已被禁用", channel.Name, channel.Id)
	content := fmt.Sprintf("通道「%s」（#%d）累计消耗 $%.4f，已达到额度上限 $%.4f，已自动禁用。", channel.Name, channel.Id, usedUSD, limitUSD)
	if settings.CostRatio > 0 {
		content += fmt.Sprintf("按成本倍率 %g 折算成本 $%.4f。", settings.CostRatio, usedUSD*settings.CostRatio)
	}
	content += "调高该渠道的额度上限后可重新启用。"
	NotifyRootUser(formatNotifyType(channel.Id, common.ChannelStatusAutoDisabled), subject, content)
}

func shouldCloseActiveWebSocketsAfterDisable(channelId int) bool {
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.SysLog(common.LogText("failed to check channel status before closing active websockets: channel_id=%d, error=%v", channelId, err))
		return true
	}
	return channel.Status != common.ChannelStatusEnabled
}

// RootUserLanguage returns the saved language of the root user, who receives
// channel notices; it is empty when the root user saved none.
func RootUserLanguage() string {
	root := model.GetRootUser()
	if root == nil {
		return ""
	}
	return root.GetSetting().Language
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	common.SysLog(common.LogText("channel %q (#%d) failed, disabling it, reason: %s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		common.SysLog(common.LogText("channel %q (#%d) has automatic disabling turned off, skipping", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		if shouldCloseActiveWebSocketsAfterDisable(channelError.ChannelId) {
			CloseActiveWebSocketsForChannel(channelError.ChannelId, ChannelDisabledCloseReason)
		}
		lang := RootUserLanguage()
		params := map[string]any{"Name": channelError.ChannelName, "Id": channelError.ChannelId, "Reason": reason}
		subject := i18n.Translate(lang, i18n.MsgChannelNotifyDisabledSubject, params)
		content := i18n.Translate(lang, i18n.MsgChannelNotifyDisabledContent, params)
		NotifyRootUser(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
	}
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		lang := RootUserLanguage()
		params := map[string]any{"Name": channelName, "Id": channelId}
		subject := i18n.Translate(lang, i18n.MsgChannelNotifyEnabledSubject, params)
		content := i18n.Translate(lang, i18n.MsgChannelNotifyEnabledContent, params)
		NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
	}
}

func ShouldDisableChannel(err *types.NewAPIError) bool {
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	if err == nil {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if operation_setting.ShouldDisableByStatusCode(err.StatusCode) {
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	search, _ := AcSearch(lowerMessage, operation_setting.AutomaticDisableKeywords, true)
	return search
}

// ShouldEnableChannel reports whether an automatically disabled channel may be
// re-enabled after a successful test. A channel that exhausted its quota limit
// stays disabled until an administrator raises the limit.
func ShouldEnableChannel(newAPIError *types.NewAPIError, channel *model.Channel) bool {
	if !common.AutomaticEnableChannelEnabled {
		return false
	}
	if newAPIError != nil {
		return false
	}
	if channel == nil || channel.Status != common.ChannelStatusAutoDisabled {
		return false
	}
	return !channel.QuotaLimitReached()
}
