package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// channelRpmMemoryLimiter is the no-Redis fallback for channel RPM limits;
// like the middleware limiter it then applies per instance, not cluster-wide.
var channelRpmMemoryLimiter common.InMemoryRateLimiter

// TakeChannelRateLimit enforces the per-channel RPM/TPM limits for one
// dispatch attempt of the given channel. It returns true when the channel may
// be used; false means the channel is saturated and the caller should fail
// over to another channel (or report no available channel). Saturation is a
// routing concern, never surfaced to API users; the reason is only logged.
//
// RPM uses the shared fixed one-minute window (atomic take). TPM is checked
// read-only against the current minute's accounted tokens; actual usage is
// recorded at billing settlement by RecordModelTokensUsed, so window edges may
// overshoot. Redis failures fail open: channel rate limiting is quota
// management, not a security boundary, and must not make Redis a hard relay
// dependency.
func TakeChannelRateLimit(c *gin.Context, channel *model.Channel) bool {
	if channel == nil {
		return true
	}
	rpm, tpm := 0, 0
	if channel.ExtendConfig != nil {
		rpm = channel.ExtendConfig.RpmLimit
		tpm = channel.ExtendConfig.TpmLimit
	} else if !common.MemoryCacheEnabled && model.HasAnyChannelRateLimit() {
		// DB 模式下渠道对象不预填 ExtendConfig，按需查表；
		// 无任何渠道配置限额时跳过查询，保持未启用零开销
		settings := model.GetChannelExtendSettings(channel.Id)
		rpm = settings.RpmLimit
		tpm = settings.TpmLimit
	}
	if rpm <= 0 && tpm <= 0 {
		return true
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}

	// TPM 先查：纯读取无副作用，TPM 饱和时不浪费该渠道的 RPM 计数
	if tpm > 0 {
		minute := common.UnixMinute()
		var usedTokens int64
		if common.RedisEnabled {
			used, err := common.RDB.Get(ctx, common.ChannelTpmRedisKey(channel.Id, minute)).Int64()
			if err != nil && err != redis.Nil {
				common.SysError(fmt.Sprintf("channel rate limit: tpm check failed (channel=%d): %s", channel.Id, err.Error()))
			} else if err == nil {
				usedTokens = used
			}
		} else {
			usedTokens = common.ChannelTpmMemoryCounter.Get(common.ChannelTpmKey(channel.Id), minute)
		}
		if usedTokens >= int64(tpm) {
			logger.LogWarn(ctx, fmt.Sprintf("channel %d saturated: tpm used %d >= limit %d, failing over", channel.Id, usedTokens, tpm))
			return false
		}
	}

	if rpm > 0 {
		if common.RedisEnabled {
			allowed, count, _, err := common.RedisFixedWindowTake(ctx, common.ChannelRpmKey(channel.Id), rpm, 60)
			if err != nil {
				common.SysError(fmt.Sprintf("channel rate limit: rpm check failed (channel=%d): %s", channel.Id, err.Error()))
				return true
			}
			if !allowed {
				logger.LogWarn(ctx, fmt.Sprintf("channel %d saturated: rpm count %d > limit %d, failing over", channel.Id, count, rpm))
				return false
			}
		} else {
			channelRpmMemoryLimiter.Init(common.RateLimitKeyExpirationDuration)
			if !channelRpmMemoryLimiter.Request(common.ChannelRpmKey(channel.Id), rpm, 60) {
				logger.LogWarn(ctx, fmt.Sprintf("channel %d saturated: rpm limit %d reached, failing over", channel.Id, rpm))
				return false
			}
		}
	}
	return true
}
