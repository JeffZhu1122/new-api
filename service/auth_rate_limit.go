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

const (
	// mailboxMailLimit is how many mails of one kind a single address can be
	// sent within mailboxMailWindow seconds, the lifetime of a code.
	mailboxMailLimit        = 3
	mailboxMailWindow int64 = 10 * 60
)

// authRateMemoryLimiter backs the per-account and per-mailbox limits without
// Redis, or while Redis fails; like the middleware limiter it then applies per
// instance.
var authRateMemoryLimiter common.InMemoryRateLimiter

// takeAuthRate spends one request from the fixed window behind key and
// reports whether it fit, and otherwise how many seconds to wait.
func takeAuthRate(ctx context.Context, key string, maxRequests int, window int64) (bool, int64) {
	if common.RedisEnabled {
		taken, _, ttlSeconds, err := common.RedisFixedWindowTake(ctx, key, maxRequests, window)
		if err == nil {
			return taken, max(ttlSeconds, 0)
		}
		common.SysError(common.LogText("authentication rate limit fell back to memory: %v", err))
	}
	authRateMemoryLimiter.Init(common.RateLimitKeyExpirationDuration)
	if !authRateMemoryLimiter.Request(key, maxRequests, window) {
		return false, window
	}
	return true, 0
}

// authRateSubject is the key part for user-typed text such as a login
// identifier or a mail address: case and surrounding space do not make a new
// budget, and the text itself is not kept in the limiter.
func authRateSubject(text string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(text))))
	return hex.EncodeToString(digest[:])
}

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
func TakeLoginAttempt(ctx context.Context, identifier string, candidates []model.User) (bool, int64) {
	if !common.CriticalRateLimitEnable {
		return true, 0
	}
	subjects := make([]string, 0, max(len(candidates), 1))
	for i := range candidates {
		subjects = append(subjects, "user:"+strconv.Itoa(candidates[i].Id))
	}
	if len(subjects) == 0 {
		subjects = append(subjects, "name:"+authRateSubject(identifier))
	}

	allowed := true
	var retryAfter int64
	for _, subject := range subjects {
		taken, wait := takeAuthRate(ctx, "loginAttempt:v1:"+subject, common.CriticalRateLimitNum, common.CriticalRateLimitDuration)
		if !taken {
			allowed = false
			retryAfter = max(retryAfter, wait)
		}
	}
	return allowed, retryAfter
}

// TakeMailboxMail spends one request for a mail of purpose to address. The
// per-address limits in front of the mail routes do not stop requests spread
// over many addresses from flooding one mailbox, or from replacing its pending
// code over and over; this one follows the mailbox. It counts every request,
// whether or not the address has an account, so a refusal says nothing about
// which addresses are registered. It reports whether the mail may be sent and
// otherwise how many seconds to wait.
func TakeMailboxMail(ctx context.Context, purpose string, address string) (bool, int64) {
	return takeAuthRate(ctx, "mailboxMail:v1:"+purpose+":"+authRateSubject(address), mailboxMailLimit, mailboxMailWindow)
}
