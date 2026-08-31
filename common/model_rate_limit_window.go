package common

import (
	"fmt"
	"sync"
	"time"
)

// Key builders shared by the enforcement middleware and the token recorder so
// the checked key and the recorded key can never drift apart.

func ModelRpmKey(userId int, group string, model string) string {
	return fmt.Sprintf("rateLimit:v2:mrpm:%d:%s:%s", userId, group, model)
}

// ModelTpmKey identifies one (user, group, model) TPM counter. It is used
// directly as the in-memory counter key; the Redis key appends the minute
// bucket via ModelTpmRedisKey.
func ModelTpmKey(userId int, group string, model string) string {
	return fmt.Sprintf("rateLimit:v2:mtpm:%d:%s:%s", userId, group, model)
}

func ModelTpmRedisKey(userId int, group string, model string, unixMinute int64) string {
	return fmt.Sprintf("%s:%d", ModelTpmKey(userId, group, model), unixMinute)
}

// ChannelRpmKey identifies one channel's RPM fixed window; the limit applies
// to the whole channel across all users, groups, models and keys.
func ChannelRpmKey(channelId int) string {
	return fmt.Sprintf("rateLimit:v2:crpm:%d", channelId)
}

// ChannelTpmKey identifies one channel's TPM counter. It is used directly as
// the in-memory counter key; the Redis key appends the minute bucket via
// ChannelTpmRedisKey.
func ChannelTpmKey(channelId int) string {
	return fmt.Sprintf("rateLimit:v2:ctpm:%d", channelId)
}

func ChannelTpmRedisKey(channelId int, unixMinute int64) string {
	return fmt.Sprintf("%s:%d", ChannelTpmKey(channelId), unixMinute)
}

// UnixMinute returns the current fixed one-minute window bucket.
func UnixMinute() int64 {
	return time.Now().Unix() / 60
}

type windowBucket struct {
	minute int64
	count  int64
}

// InMemoryWindowCounter is the no-Redis fallback for TPM accounting: per-key
// counters bound to a one-minute bucket. Adding to a newer bucket resets the
// counter, so at most one bucket per key is retained.
type InMemoryWindowCounter struct {
	mutex   sync.Mutex
	buckets map[string]windowBucket
}

// ModelTpmMemoryCounter is the process-wide TPM counter used when Redis is
// disabled. Note the limits then apply per instance, not cluster-wide.
var ModelTpmMemoryCounter InMemoryWindowCounter

// ChannelTpmMemoryCounter is the no-Redis fallback for channel-level TPM
// accounting; the same per-instance caveat applies.
var ChannelTpmMemoryCounter InMemoryWindowCounter

func (c *InMemoryWindowCounter) Add(key string, minute int64, delta int64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.buckets == nil {
		c.buckets = make(map[string]windowBucket)
	}
	bucket := c.buckets[key]
	if bucket.minute != minute {
		bucket = windowBucket{minute: minute}
	}
	bucket.count += delta
	c.buckets[key] = bucket
	if len(c.buckets) > 1 && len(c.buckets)%4096 == 0 {
		c.evictStaleLocked(minute)
	}
}

func (c *InMemoryWindowCounter) Get(key string, minute int64) int64 {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	bucket, ok := c.buckets[key]
	if !ok || bucket.minute != minute {
		return 0
	}
	return bucket.count
}

// evictStaleLocked drops buckets older than the previous minute. It runs
// opportunistically from Add (no background goroutine needed) and must be
// called with the mutex held.
func (c *InMemoryWindowCounter) evictStaleLocked(currentMinute int64) {
	for key, bucket := range c.buckets {
		if bucket.minute < currentMinute-1 {
			delete(c.buckets, key)
		}
	}
}
