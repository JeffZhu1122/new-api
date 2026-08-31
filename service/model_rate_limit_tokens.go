package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
)

// RecordModelTokensUsed 把一次请求实际消耗的 token 记入 TPM 窗口,供限流检查:
// 用户侧记入 (用户, 分组, 模型) 窗口(middleware.ModelRpmTpmRateLimit 检查),
// 渠道侧记入渠道窗口(TakeChannelRateLimit 检查),两者独立开关、独立回退。
// 事后记账口径:只有进入计费结算的请求才会被记录,失败请求与免费探测类接口
// 不占 TPM 额度。记账失败只记日志,绝不影响计费主流程。
func RecordModelTokensUsed(ctx context.Context, relayInfo *relaycommon.RelayInfo, totalTokens int) {
	if relayInfo == nil || totalTokens <= 0 {
		return
	}
	group := relayInfo.TokenGroup
	if group == "" {
		group = relayInfo.UserGroup
	}

	userTpm := 0
	// playground 不在用户级限流范围内,不占用户的 TPM 额度
	if setting.ModelRateLimitEnabled && !relayInfo.IsPlayground {
		userOverride, err := model.GetUserRateLimitOverride(relayInfo.UserId)
		if err != nil {
			common.SysError(fmt.Sprintf("model rate limit: failed to load user override for recording (user=%d): %s", relayInfo.UserId, err.Error()))
		}
		_, userTpm = setting.ResolveModelRateLimit(group, relayInfo.OriginModelName, userOverride)
	}

	// 渠道级限额独立于用户级总开关;playground 请求同样消耗上游渠道容量,照记
	channelTpm := 0
	channelId := relayInfo.GetChannelID()
	if channelId > 0 && model.HasAnyChannelRateLimit() {
		if settings := model.GetChannelExtendSettings(channelId); settings.TpmLimit > 0 {
			channelTpm = settings.TpmLimit
		}
	}

	if userTpm <= 0 && channelTpm <= 0 {
		return
	}

	minute := common.UnixMinute()
	if common.RedisEnabled {
		pipe := common.RDB.Pipeline()
		if userTpm > 0 {
			key := common.ModelTpmRedisKey(relayInfo.UserId, group, relayInfo.OriginModelName, minute)
			pipe.IncrBy(ctx, key, int64(totalTokens))
			pipe.Expire(ctx, key, 3*time.Minute)
		}
		if channelTpm > 0 {
			key := common.ChannelTpmRedisKey(channelId, minute)
			pipe.IncrBy(ctx, key, int64(totalTokens))
			pipe.Expire(ctx, key, 3*time.Minute)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			common.SysError(fmt.Sprintf("model rate limit: failed to record %d tokens (user=%d, channel=%d): %s", totalTokens, relayInfo.UserId, channelId, err.Error()))
		}
		return
	}
	if userTpm > 0 {
		common.ModelTpmMemoryCounter.Add(common.ModelTpmKey(relayInfo.UserId, group, relayInfo.OriginModelName), minute, int64(totalTokens))
	}
	if channelTpm > 0 {
		common.ChannelTpmMemoryCounter.Add(common.ChannelTpmKey(channelId), minute, int64(totalTokens))
	}
}
