package common

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type verificationValue struct {
	code     string
	time     time.Time
	failures int
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10
var VerificationValidMinutes = 10

// verificationMaxFailures is how many wrong guesses a code survives. The
// registration code is six hex characters, so without a cap the per-address
// request limit is the only thing between a guesser and the code.
const verificationMaxFailures = 5

// verificationGuessScript counts one guess at the code stored under KEYS[1]
// and hands the code out for comparison only while the guesses stay within
// ARGV[1]. Counting before comparing, in one step, keeps guesses sent in
// parallel from all being compared against the code before any of them is
// counted.
const verificationGuessScript = `
local code = redis.call('HGET', KEYS[1], 'code')
if not code then
  return false
end
local tries = redis.call('HINCRBY', KEYS[1], 'tries', 1)
if tries > tonumber(ARGV[1]) then
  redis.call('DEL', KEYS[1])
  return false
end
return {code, tries}
`

// verificationRefundScript gives back the guess counted for a correct code,
// unless the code was deleted in the meantime.
const verificationRefundScript = `
if redis.call('EXISTS', KEYS[1]) == 1 then
  redis.call('HINCRBY', KEYS[1], 'tries', -1)
end
return 0
`

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

// verificationRedisKey names the Redis hash holding the code for key. Keys
// are mail addresses, so Redis sees only their digest.
func verificationRedisKey(key string, purpose string) string {
	digest := sha256.Sum256([]byte(key))
	return "verification:v1:" + purpose + ":" + hex.EncodeToString(digest[:])
}

// RegisterVerificationCodeWithKey makes code the live code for key, replacing
// an earlier one together with the guesses made at it. With Redis the code is
// kept there, so that whichever instance serves the next request can check
// it; without Redis it stays in this process.
func RegisterVerificationCodeWithKey(key string, code string, purpose string) error {
	if RedisEnabled {
		ctx := context.Background()
		redisKey := verificationRedisKey(key, purpose)
		_, err := RDB.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Del(ctx, redisKey)
			pipe.HSet(ctx, redisKey, "code", code, "tries", 0)
			pipe.Expire(ctx, redisKey, time.Duration(VerificationValidMinutes)*time.Minute)
			return nil
		})
		return err
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
	return nil
}

// VerifyCodeWithKey reports whether code is the live code for key. A match
// leaves the code in place for the caller to delete once the operation it
// guards has gone through; a wrong guess counts against the code, and the
// guess that reaches verificationMaxFailures removes it.
func VerifyCodeWithKey(key string, code string, purpose string) bool {
	if RedisEnabled {
		return verifyRedisCode(verificationRedisKey(key, purpose), code)
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	if !okay {
		return false
	}
	if int(time.Since(value.time).Seconds()) >= VerificationValidMinutes*60 {
		delete(verificationMap, purpose+key)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(value.code)) == 1 {
		return true
	}
	value.failures++
	if value.failures >= verificationMaxFailures {
		delete(verificationMap, purpose+key)
		return false
	}
	verificationMap[purpose+key] = value
	return false
}

func verifyRedisCode(redisKey string, code string) bool {
	ctx := context.Background()
	values, err := RDB.Eval(ctx, verificationGuessScript, []string{redisKey}, verificationMaxFailures).Slice()
	if errors.Is(err, redis.Nil) {
		return false
	}
	if err != nil || len(values) != 2 {
		SysError(LogText("verification code check failed: %v", err))
		return false
	}
	stored, isString := values[0].(string)
	tries, err := redisReplyInteger(values[1])
	if !isString || err != nil {
		SysError(LogText("verification code check failed: unexpected Redis reply"))
		return false
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(stored)) == 1 {
		if err := RDB.Eval(ctx, verificationRefundScript, []string{redisKey}).Err(); err != nil {
			SysError(LogText("verification code check failed: %v", err))
		}
		return true
	}
	if tries >= verificationMaxFailures {
		if err := RDB.Del(ctx, redisKey).Err(); err != nil {
			SysError(LogText("verification code removal failed: %v", err))
		}
	}
	return false
}

func DeleteKey(key string, purpose string) {
	if RedisEnabled {
		if err := RDB.Del(context.Background(), verificationRedisKey(key, purpose)).Err(); err != nil {
			SysError(LogText("verification code removal failed: %v", err))
		}
		return
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
