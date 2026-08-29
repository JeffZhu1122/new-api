package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// ModelRpmTpmRateLimit 按 (用户, 分组, 模型) 组合粒度执行 RPM/TPM 限流。
// 规则解析优先级:用户覆盖 > 分组×模型 > 分组默认 > 全局模型 > 全局默认,
// RPM 与 TPM 各自独立回退;0 或未设置表示不限。
// TPM 采用事后记账口径:此处只检查当前分钟窗口累计值,实际 token 用量在计费
// 结算时由 service.RecordModelTokensUsed 记入,窗口边缘允许超冲。
// 限流是配额管理而非安全边界,因此 Redis 故障时 fail-open 放行(仅记日志),
// 有意区别于 redisRateLimiter 的 500 策略,避免 Redis 成为转发硬依赖。
func ModelRpmTpmRateLimit() func(c *gin.Context) {
	// 内存回退提前就绪,避免运行中 Redis 故障时与首次初始化竞态。
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		if !setting.ModelRateLimitEnabled {
			c.Next()
			return
		}

		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}
		// 解析失败时按 model 为空继续,分组/全局默认规则仍然生效,
		// 避免构造无法解析的请求绕过计数(这类请求随后也会被 Distribute 拒绝)。
		modelName := ""
		if modelRequest, _, err := getModelRequest(c); err == nil && modelRequest != nil {
			modelName = modelRequest.Model
		}
		userOverride, err := model.GetUserRateLimitOverride(userId)
		if err != nil {
			common.SysError(fmt.Sprintf("model rate limit: failed to load user override (user=%d): %s", userId, err.Error()))
		}

		rpm, tpm := setting.ResolveModelRateLimit(group, modelName, userOverride)
		if rpm <= 0 && tpm <= 0 {
			c.Next()
			return
		}

		if rpm > 0 {
			if common.RedisEnabled {
				allowed, _, ttlSeconds, err := redisFixedWindowTake(
					c.Request.Context(),
					common.ModelRpmKey(userId, group, modelName),
					rpm,
					60,
				)
				if err != nil {
					common.SysError(fmt.Sprintf("model rate limit: rpm check failed (user=%d): %s", userId, err.Error()))
				} else if !allowed {
					abortWithModelRateLimited(c, ttlSeconds,
						fmt.Sprintf("您已达到模型请求频率限制(RPM=%d),请 %d 秒后重试", rpm, ttlSeconds))
					return
				}
			} else if !inMemoryRateLimiter.Request(common.ModelRpmKey(userId, group, modelName), rpm, 60) {
				abortWithModelRateLimited(c, 60,
					fmt.Sprintf("您已达到模型请求频率限制(RPM=%d),请稍后重试", rpm))
				return
			}
		}

		if tpm > 0 {
			minute := common.UnixMinute()
			var usedTokens int64
			if common.RedisEnabled {
				used, err := common.RDB.Get(c.Request.Context(), common.ModelTpmRedisKey(userId, group, modelName, minute)).Int64()
				if err != nil && err != redis.Nil {
					common.SysError(fmt.Sprintf("model rate limit: tpm check failed (user=%d): %s", userId, err.Error()))
				} else if err == nil {
					usedTokens = used
				}
			} else {
				usedTokens = common.ModelTpmMemoryCounter.Get(common.ModelTpmKey(userId, group, modelName), minute)
			}
			if usedTokens >= int64(tpm) {
				retryAfter := 60 - time.Now().Unix()%60
				abortWithModelRateLimited(c, retryAfter,
					fmt.Sprintf("您已达到模型 token 用量限制(TPM=%d),请 %d 秒后重试", tpm, retryAfter))
				return
			}
		}

		c.Next()
	}
}

func abortWithModelRateLimited(c *gin.Context, retryAfterSeconds int64, message string) {
	if retryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
	}
	abortWithOpenAiMessage(c, http.StatusTooManyRequests, message)
}
