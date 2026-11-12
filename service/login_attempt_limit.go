package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// loginAttemptMemoryLimiter is the no-Redis fallback for the per-account login
// limit; like the middleware limiter it then applies per instance.
var loginAttemptMemoryLimiter common.InMemoryRateLimiter

// TakeLoginAttempt spends one password attempt on every account the login
// identifier resolved to, or on the identifier itself when it resolved to
// none. The per-address limit in front of the login route does not stop
// guesses that arrive from many addresses; this one follows the account. It
// reports whether the attempt may proceed and otherwise how many seconds to
// wait.
//
// The budget is the critical rate limit and successful sign-ins count towards
// it. Accounts are keyed by id so that spellings the database treats as equal
// share one budget. A saturated account can still use password recovery.
func TakeLoginAttempt(ctx context.Context, identifier string, candidates []model.User) (bool, int64, error) {
	if !common.CriticalRateLimitEnable {
		return true, 0, nil
	}
	subjects := make([]string, 0, max(len(candidates), 1))
	for i := range candidates {
		subjects = append(subjects, "user:"+strconv.Itoa(candidates[i].Id))
	}
	if len(subjects) == 0 {
		digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(identifier))))
		subjects = append(subjects, "name:"+hex.EncodeToString(digest[:]))
	}

	allowed := true
	var retryAfter int64
	for _, subject := range subjects {
		key := "loginAttempt:v1:" + subject
		if !common.RedisEnabled {
			loginAttemptMemoryLimiter.Init(common.RateLimitKeyExpirationDuration)
			if !loginAttemptMemoryLimiter.Request(key, common.CriticalRateLimitNum, common.CriticalRateLimitDuration) {
				allowed = false
				retryAfter = common.CriticalRateLimitDuration
			}
			continue
		}
		taken, _, ttlSeconds, err := common.RedisFixedWindowTake(ctx, key, common.CriticalRateLimitNum, common.CriticalRateLimitDuration)
		if err != nil {
			return false, 0, err
		}
		if !taken {
			allowed = false
			retryAfter = max(retryAfter, ttlSeconds)
		}
	}
	return allowed, retryAfter, nil
}
