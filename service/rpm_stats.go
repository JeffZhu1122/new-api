package service

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/go-redis/redis/v8"
)

// Live RPM statistics per channel and per user, independent of consume logs.
//
// Every dispatch attempt (RequestPolicyState.BeginAttempt) is counted in
// process memory first. With Redis, a background loop flushes the deltas every
// rpmStatsFlushInterval in one pipeline, so Redis traffic grows with the
// number of instances rather than with request volume, and the counts of all
// instances add up. Without Redis the counters stay in memory and cover this
// instance only.
//
// Counters live in rpmStatsBucketSeconds buckets. A reading sums the
// rpmStatsWindowBuckets most recent buckets that ended at least
// rpmStatsSettleDelay ago: one full minute that every instance has already
// flushed. Readings lag real time by a few seconds but never show a
// half-filled bucket.
//
// Channel figures count dispatch attempts: every retry counts once on the
// channel it went to, failed attempts included, because that is the load the
// upstream sees. User totals count requests: only the first attempt of a
// request counts, so retries do not inflate a user's RPM.
const (
	rpmStatsBucketSeconds = 10
	rpmStatsWindowBuckets = 6
	rpmStatsFlushInterval = 2 * time.Second
	rpmStatsSettleDelay   = 4 * time.Second
	rpmStatsKeyTTL        = 2 * time.Minute
	rpmStatsRedisTimeout  = time.Second
)

const (
	RpmStatsSourceRedis    = "redis"
	RpmStatsSourceMemory   = "memory"
	RpmStatsSourceDisabled = "disabled"
)

const (
	rpmKindChannel     = "ch" // channel id -> attempts
	rpmKindUser        = "u"  // user id -> requests
	rpmKindChannelUser = "cu" // per channel: user id -> attempts
	rpmKindUserChannel = "uc" // per user: channel id -> attempts
)

// rpmStatsHash names one counter hash inside a bucket; its fields are channel
// or user ids. The Redis key mirrors it: rpmstat:<kind>[:<owner>]:<bucket>.
type rpmStatsHash struct {
	kind  string
	owner int
}

func (h rpmStatsHash) redisKey(bucket int64) string {
	if h.owner == 0 {
		return fmt.Sprintf("rpmstat:%s:%d", h.kind, bucket)
	}
	return fmt.Sprintf("rpmstat:%s:%d:%d", h.kind, h.owner, bucket)
}

type rpmStatsBucket map[rpmStatsHash]map[int]int64

func (b rpmStatsBucket) add(hash rpmStatsHash, field int) {
	counts := b[hash]
	if counts == nil {
		counts = map[int]int64{}
		b[hash] = counts
	}
	counts[field]++
}

type rpmStatsRecorder struct {
	mu       sync.Mutex
	enabled  bool
	useRedis bool
	// buckets holds unflushed counts in Redis mode and every readable count
	// in memory mode, keyed by unix seconds / rpmStatsBucketSeconds.
	buckets         map[int64]rpmStatsBucket
	lastFlushErrLog time.Time
}

var rpmStats = &rpmStatsRecorder{enabled: true, buckets: map[int64]rpmStatsBucket{}}

// RpmReading is one RPM window: Counts maps channel or user ids to the number
// of requests within [WindowStart, WindowEnd) in unix seconds.
type RpmReading struct {
	Source      string
	WindowStart int64
	WindowEnd   int64
	Counts      map[int]int64
}

// StartRpmStats applies RPM_STATS_ENABLED and starts the Redis flush loop when
// Redis is available. Call it once after Redis has been initialised.
func StartRpmStats() {
	enabled := common.GetEnvOrDefaultBool("RPM_STATS_ENABLED", true)
	useRedis := enabled && common.RedisEnabled && common.RDB != nil
	rpmStats.mu.Lock()
	rpmStats.enabled = enabled
	rpmStats.useRedis = useRedis
	rpmStats.mu.Unlock()
	switch {
	case !enabled:
		common.SysLog("rpm stats disabled by RPM_STATS_ENABLED")
		return
	case !useRedis:
		common.SysLog("rpm stats: Redis not enabled, counting this instance only")
		return
	}
	go func() {
		ticker := time.NewTicker(rpmStatsFlushInterval)
		defer ticker.Stop()
		for range ticker.C {
			rpmStats.flush(context.Background(), time.Now())
		}
	}()
}

// RecordRpmAttempt counts one dispatch attempt to a channel. firstAttempt
// marks the attempt that also counts as a new request of the user.
func RecordRpmAttempt(channelID int, userID int, firstAttempt bool) {
	rpmStats.record(time.Now(), channelID, userID, firstAttempt)
}

// ChannelRpm returns the RPM of each given channel.
func ChannelRpm(channelIDs []int) (RpmReading, error) {
	return rpmStats.read(time.Now(), rpmStatsHash{kind: rpmKindChannel}, channelIDs)
}

// UserRpm returns the RPM of each given user.
func UserRpm(userIDs []int) (RpmReading, error) {
	return rpmStats.read(time.Now(), rpmStatsHash{kind: rpmKindUser}, userIDs)
}

// ChannelUserRpm splits one channel's attempts by user.
func ChannelUserRpm(channelID int) (RpmReading, error) {
	return rpmStats.read(time.Now(), rpmStatsHash{kind: rpmKindChannelUser, owner: channelID}, nil)
}

// UserChannelRpm splits one user's attempts by channel.
func UserChannelRpm(userID int) (RpmReading, error) {
	return rpmStats.read(time.Now(), rpmStatsHash{kind: rpmKindUserChannel, owner: userID}, nil)
}

func (r *rpmStatsRecorder) record(at time.Time, channelID int, userID int, firstAttempt bool) {
	if channelID <= 0 {
		return
	}
	idx := at.Unix() / rpmStatsBucketSeconds
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled {
		return
	}
	bucket := r.buckets[idx]
	if bucket == nil {
		bucket = rpmStatsBucket{}
		r.buckets[idx] = bucket
		if !r.useRedis {
			// 内存模式下桶只用于读取，超出任何读取窗口的旧桶直接丢弃
			for old := range r.buckets {
				if old < idx-rpmStatsWindowBuckets-2 {
					delete(r.buckets, old)
				}
			}
		}
	}
	bucket.add(rpmStatsHash{kind: rpmKindChannel}, channelID)
	if userID <= 0 {
		return
	}
	bucket.add(rpmStatsHash{kind: rpmKindChannelUser, owner: channelID}, userID)
	bucket.add(rpmStatsHash{kind: rpmKindUserChannel, owner: userID}, channelID)
	if firstAttempt {
		bucket.add(rpmStatsHash{kind: rpmKindUser}, userID)
	}
}

// flush moves every pending count to Redis in one pipeline. Statistics may be
// lost: a failed flush is dropped rather than retried, so a Redis outage only
// undercounts and never builds a backlog.
func (r *rpmStatsRecorder) flush(ctx context.Context, at time.Time) {
	r.mu.Lock()
	pending := r.buckets
	r.buckets = map[int64]rpmStatsBucket{}
	r.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, rpmStatsRedisTimeout)
	defer cancel()
	pipe := common.RDB.Pipeline()
	for idx, bucket := range pending {
		for hash, counts := range bucket {
			key := hash.redisKey(idx)
			for field, n := range counts {
				pipe.HIncrBy(ctx, key, strconv.Itoa(field), n)
			}
			pipe.Expire(ctx, key, rpmStatsKeyTTL)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil && at.Sub(r.lastFlushErrLog) >= time.Minute {
		r.lastFlushErrLog = at
		common.SysError("rpm stats: flush to Redis failed, dropping pending counts: " + err.Error())
	}
}

// read sums one counter hash over the window ending before at. fields limits
// the result to those ids (each present, zero when idle); nil returns every
// id that has counts.
func (r *rpmStatsRecorder) read(at time.Time, hash rpmStatsHash, fields []int) (RpmReading, error) {
	latest := at.Add(-rpmStatsSettleDelay).Unix()/rpmStatsBucketSeconds - 1
	oldest := latest - rpmStatsWindowBuckets + 1
	reading := RpmReading{
		WindowStart: oldest * rpmStatsBucketSeconds,
		WindowEnd:   (latest + 1) * rpmStatsBucketSeconds,
		Counts:      map[int]int64{},
	}
	if fields != nil {
		fields = slices.Clone(fields)
		slices.Sort(fields)
		fields = slices.Compact(fields)
		for _, id := range fields {
			reading.Counts[id] = 0
		}
	}

	r.mu.Lock()
	enabled, useRedis := r.enabled, r.useRedis
	if enabled && !useRedis {
		for idx := oldest; idx <= latest; idx++ {
			counts := r.buckets[idx][hash]
			if fields == nil {
				for id, n := range counts {
					reading.Counts[id] += n
				}
				continue
			}
			for _, id := range fields {
				reading.Counts[id] += counts[id]
			}
		}
	}
	r.mu.Unlock()

	switch {
	case !enabled:
		reading.Source = RpmStatsSourceDisabled
		return reading, nil
	case !useRedis:
		reading.Source = RpmStatsSourceMemory
		return reading, nil
	}
	reading.Source = RpmStatsSourceRedis
	if fields != nil && len(fields) == 0 {
		return reading, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpmStatsRedisTimeout)
	defer cancel()
	pipe := common.RDB.Pipeline()
	if fields == nil {
		cmds := make([]*redis.StringStringMapCmd, 0, rpmStatsWindowBuckets)
		for idx := oldest; idx <= latest; idx++ {
			cmds = append(cmds, pipe.HGetAll(ctx, hash.redisKey(idx)))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return reading, err
		}
		for _, cmd := range cmds {
			for field, value := range cmd.Val() {
				id, idErr := strconv.Atoi(field)
				n, nErr := strconv.ParseInt(value, 10, 64)
				if idErr == nil && nErr == nil {
					reading.Counts[id] += n
				}
			}
		}
		return reading, nil
	}

	names := make([]string, len(fields))
	for i, id := range fields {
		names[i] = strconv.Itoa(id)
	}
	cmds := make([]*redis.SliceCmd, 0, rpmStatsWindowBuckets)
	for idx := oldest; idx <= latest; idx++ {
		cmds = append(cmds, pipe.HMGet(ctx, hash.redisKey(idx), names...))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return reading, err
	}
	for _, cmd := range cmds {
		for i, value := range cmd.Val() {
			text, ok := value.(string)
			if !ok {
				continue
			}
			if n, err := strconv.ParseInt(text, 10, 64); err == nil {
				reading.Counts[fields[i]] += n
			}
		}
	}
	return reading, nil
}
