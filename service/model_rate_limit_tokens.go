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

// RecordModelTokensUsed 把一次请求实际消耗的 token 记入 (用户, 分组, 模型) 的
// 当前分钟 TPM 窗口,供 middleware.ModelRpmTpmRateLimit 检查。事后记账口径:
// 只有进入计费结算的请求才会被记录,失败请求与免费探测类接口不占 TPM 额度。
// 记账失败只记日志,绝不影响计费主流程。
func RecordModelTokensUsed(ctx context.Context, relayInfo *relaycommon.RelayInfo, totalTokens int) {
	if !setting.ModelRateLimitEnabled || relayInfo == nil || totalTokens <= 0 {
		return
	}
	// playground 不在限流范围内,也不占用户的 TPM 额度
	if relayInfo.IsPlayground {
		return
	}
	group := relayInfo.TokenGroup
	if group == "" {
		group = relayInfo.UserGroup
	}
	userOverride, err := model.GetUserRateLimitOverride(relayInfo.UserId)
	if err != nil {
		common.SysError(fmt.Sprintf("model rate limit: failed to load user override for recording (user=%d): %s", relayInfo.UserId, err.Error()))
	}
	_, tpm := setting.ResolveModelRateLimit(group, relayInfo.OriginModelName, userOverride)
	if tpm <= 0 {
		return
	}

	minute := common.UnixMinute()
	if common.RedisEnabled {
		key := common.ModelTpmRedisKey(relayInfo.UserId, group, relayInfo.OriginModelName, minute)
		pipe := common.RDB.Pipeline()
		pipe.IncrBy(ctx, key, int64(totalTokens))
		pipe.Expire(ctx, key, 3*time.Minute)
		if _, err := pipe.Exec(ctx); err != nil {
			common.SysError(fmt.Sprintf("model rate limit: failed to record %d tokens (user=%d): %s", totalTokens, relayInfo.UserId, err.Error()))
		}
		return
	}
	common.ModelTpmMemoryCounter.Add(common.ModelTpmKey(relayInfo.UserId, group, relayInfo.OriginModelName), minute, int64(totalTokens))
}
