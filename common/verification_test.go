package common

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useVerificationStore points the verification store at a fresh Redis, or at
// process memory when server is false.
func useVerificationStore(t *testing.T, server bool) *miniredis.Miniredis {
	t.Helper()
	previousEnabled, previousRDB, previousMinutes := RedisEnabled, RDB, VerificationValidMinutes
	t.Cleanup(func() { RedisEnabled, RDB, VerificationValidMinutes = previousEnabled, previousRDB, previousMinutes })
	VerificationValidMinutes = 10
	if !server {
		RedisEnabled = false
		return nil
	}
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	RedisEnabled, RDB = true, client
	return redisServer
}

func TestVerificationCodesBehaveTheSameInMemoryAndInRedis(t *testing.T) {
	for _, store := range []string{"memory", "redis"} {
		t.Run(store, func(t *testing.T) {
			redisServer := useVerificationStore(t, store == "redis")
			// The memory store is process-wide, so every case uses a new address.
			address := func(name string) string {
				return fmt.Sprintf("%s-%s-%d@example.com", store, name, time.Now().UnixNano())
			}

			t.Run("a match survives the guesses before it and is not one itself", func(t *testing.T) {
				key := address("match")
				require.NoError(t, RegisterVerificationCodeWithKey(key, "a1b2c3", EmailVerificationPurpose))
				for range verificationMaxFailures - 1 {
					assert.False(t, VerifyCodeWithKey(key, "ffffff", EmailVerificationPurpose))
				}
				assert.True(t, VerifyCodeWithKey(key, "a1b2c3", EmailVerificationPurpose))
				assert.True(t, VerifyCodeWithKey(key, "a1b2c3", EmailVerificationPurpose), "the caller may check the code again before deleting it")
				assert.False(t, VerifyCodeWithKey(key, "ffffff", EmailVerificationPurpose))
				assert.False(t, VerifyCodeWithKey(key, "a1b2c3", EmailVerificationPurpose), "the guess that reaches the cap removes the code")
			})

			t.Run("a new code starts without the old guesses", func(t *testing.T) {
				key := address("renew")
				require.NoError(t, RegisterVerificationCodeWithKey(key, "a1b2c3", EmailVerificationPurpose))
				for range verificationMaxFailures - 1 {
					assert.False(t, VerifyCodeWithKey(key, "ffffff", EmailVerificationPurpose))
				}
				require.NoError(t, RegisterVerificationCodeWithKey(key, "d4e5f6", EmailVerificationPurpose))
				assert.False(t, VerifyCodeWithKey(key, "a1b2c3", EmailVerificationPurpose), "the replaced code is gone")
				for range verificationMaxFailures - 2 {
					assert.False(t, VerifyCodeWithKey(key, "ffffff", EmailVerificationPurpose))
				}
				assert.True(t, VerifyCodeWithKey(key, "d4e5f6", EmailVerificationPurpose))
			})

			t.Run("purposes do not share codes or guesses", func(t *testing.T) {
				key := address("purpose")
				require.NoError(t, RegisterVerificationCodeWithKey(key, "a1b2c3", EmailVerificationPurpose))
				for range verificationMaxFailures {
					assert.False(t, VerifyCodeWithKey(key, "a1b2c3", PasswordResetPurpose))
				}
				assert.True(t, VerifyCodeWithKey(key, "a1b2c3", EmailVerificationPurpose))
			})

			t.Run("deleted", func(t *testing.T) {
				key := address("deleted")
				require.NoError(t, RegisterVerificationCodeWithKey(key, "a1b2c3", PasswordResetPurpose))
				DeleteKey(key, PasswordResetPurpose)
				assert.False(t, VerifyCodeWithKey(key, "a1b2c3", PasswordResetPurpose))
			})

			t.Run("expired", func(t *testing.T) {
				key := address("expired")
				require.NoError(t, RegisterVerificationCodeWithKey(key, "a1b2c3", PasswordResetPurpose))
				if redisServer != nil {
					redisServer.FastForward(time.Duration(VerificationValidMinutes)*time.Minute + time.Second)
				} else {
					VerificationValidMinutes = 0
					t.Cleanup(func() { VerificationValidMinutes = 10 })
				}
				assert.False(t, VerifyCodeWithKey(key, "a1b2c3", PasswordResetPurpose))
			})
		})
	}
}

func TestVerificationCodesInRedisReachEveryInstance(t *testing.T) {
	redisServer := useVerificationStore(t, true)
	key := fmt.Sprintf("shared-%d@example.com", time.Now().UnixNano())
	require.NoError(t, RegisterVerificationCodeWithKey(key, "reset-token", PasswordResetPurpose))

	// Another instance has nothing of this one's memory, only the same Redis.
	verificationMutex.Lock()
	verificationMap = make(map[string]verificationValue)
	verificationMutex.Unlock()
	assert.True(t, VerifyCodeWithKey(key, "reset-token", PasswordResetPurpose))

	keys := redisServer.Keys()
	require.Len(t, keys, 1)
	assert.False(t, strings.Contains(keys[0], key), "Redis holds a digest of the address, not the address")
	assert.Positive(t, redisServer.TTL(keys[0]), "the code expires in Redis on its own")
}
