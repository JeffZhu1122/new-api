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

// Live RPM / TPM statistics, independent of consume logs.
//
// Two kinds of events are counted in process memory first:
//   - dispatch attempts (RequestPolicyState.BeginAttempt), feeding the channel
//     and user RPM of the admin tables;
//   - settled requests with their token split (RecordSettledUsage, see
//     token_stats.go), feeding TPM and the usage-log statistics.
//
// With Redis, a background loop flushes the deltas every
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
// Channel RPM counts dispatch attempts: every retry counts once on the
// channel it went to, failed attempts included, because that is the load the
// upstream sees. User RPM counts requests: only the first attempt of a
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
	// RpmStatsSourceUnavailable marks a reading Redis could not serve.
	RpmStatsSourceUnavailable = "unavailable"
)

const (
	rpmKindChannel     = "ch" // channel id -> attempts
	rpmKindUser        = "u"  // user id -> requests
	rpmKindChannelUser = "cu" // per channel: user id -> attempts
	rpmKindUserChannel = "uc" // per user: channel id -> attempts
)

// rpmStatsHash names one counter hash inside a bucket. The Redis key mirrors
// it: rpmstat:<kind>[:<owner>]:<bucket>.
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

type rpmStatsBucket struct {
	counts map[rpmStatsHash]map[string]int64
	// users maps every user settled in the bucket to the username its consume
	// log carries, so usage-log filters can match usernames without the DB.
	users map[int]string
}

func (b *rpmStatsBucket) add(hash rpmStatsHash, field string, n int64) {
	counts := b.counts[hash]
	if counts == nil {
		counts = map[string]int64{}
		b.counts[hash] = counts
	}
	counts[field] += n
}

type rpmStatsRecorder struct {
	mu       sync.Mutex
	enabled  bool
	useRedis bool
	// buckets holds unflushed counts in Redis mode and every readable count
	// in memory mode, keyed by unix seconds / rpmStatsBucketSeconds.
	buckets         map[int64]*rpmStatsBucket
	lastFlushErrLog time.Time
}

var rpmStats = &rpmStatsRecorder{enabled: true, buckets: map[int64]*rpmStatsBucket{}}

// RpmReading is one statistics window over [WindowStart, WindowEnd) in unix
// seconds: Counts maps channel or user ids to RPM, Tokens to settled usage.
type RpmReading struct {
	Source      string
	WindowStart int64
	WindowEnd   int64
	Counts      map[int]int64
	Tokens      map[int]TokenStats
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
	rpmStats.recordAttempt(time.Now(), channelID, userID, firstAttempt)
}

// ChannelRpm returns the RPM and settled usage of each given channel.
func ChannelRpm(channelIDs []int) (RpmReading, error) {
	return rpmStats.readTotals(time.Now(), rpmKindChannel, statsKindChannel, channelIDs)
}

// UserRpm returns the RPM and settled usage of each given user.
func UserRpm(userIDs []int) (RpmReading, error) {
	return rpmStats.readTotals(time.Now(), rpmKindUser, statsKindUser, userIDs)
}

// ChannelUserRpm splits one channel's attempts and settled usage by user.
func ChannelUserRpm(channelID int) (RpmReading, error) {
	return rpmStats.readChannelUsers(time.Now(), channelID)
}

// UserChannelRpm splits one user's attempts and settled usage by channel.
func UserChannelRpm(userID int) (RpmReading, error) {
	return rpmStats.readUserChannels(time.Now(), userID)
}

// bucketAt returns the bucket for at, creating it when needed. Callers hold
// r.mu.
func (r *rpmStatsRecorder) bucketAt(at time.Time) *rpmStatsBucket {
	idx := at.Unix() / rpmStatsBucketSeconds
	bucket := r.buckets[idx]
	if bucket != nil {
		return bucket
	}
	bucket = &rpmStatsBucket{counts: map[rpmStatsHash]map[string]int64{}, users: map[int]string{}}
	r.buckets[idx] = bucket
	if !r.useRedis {
		// 内存模式下桶只用于读取，超出任何读取窗口的旧桶直接丢弃
		for old := range r.buckets {
			if old < idx-rpmStatsWindowBuckets-2 {
				delete(r.buckets, old)
			}
		}
	}
	return bucket
}

func (r *rpmStatsRecorder) recordAttempt(at time.Time, channelID int, userID int, firstAttempt bool) {
	if channelID <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled {
		return
	}
	bucket := r.bucketAt(at)
	channel := strconv.Itoa(channelID)
	bucket.add(rpmStatsHash{kind: rpmKindChannel}, channel, 1)
	if userID <= 0 {
		return
	}
	user := strconv.Itoa(userID)
	bucket.add(rpmStatsHash{kind: rpmKindChannelUser, owner: channelID}, user, 1)
	bucket.add(rpmStatsHash{kind: rpmKindUserChannel, owner: userID}, channel, 1)
	if firstAttempt {
		bucket.add(rpmStatsHash{kind: rpmKindUser}, user, 1)
	}
}

// flush moves every pending count to Redis in one pipeline. Statistics may be
// lost: a failed flush is dropped rather than retried, so a Redis outage only
// undercounts and never builds a backlog.
func (r *rpmStatsRecorder) flush(ctx context.Context, at time.Time) {
	r.mu.Lock()
	pending := r.buckets
	r.buckets = map[int64]*rpmStatsBucket{}
	r.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, rpmStatsRedisTimeout)
	defer cancel()
	pipe := common.RDB.Pipeline()
	for idx, bucket := range pending {
		for hash, counts := range bucket.counts {
			key := hash.redisKey(idx)
			for field, n := range counts {
				pipe.HIncrBy(ctx, key, field, n)
			}
			pipe.Expire(ctx, key, rpmStatsKeyTTL)
		}
		if len(bucket.users) > 0 {
			key := rpmStatsHash{kind: statsKindUsers}.redisKey(idx)
			users := make(map[string]any, len(bucket.users))
			for id, name := range bucket.users {
				users[strconv.Itoa(id)] = name
			}
			pipe.HSet(ctx, key, users)
			pipe.Expire(ctx, key, rpmStatsKeyTTL)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil && at.Sub(r.lastFlushErrLog) >= time.Minute {
		r.lastFlushErrLog = at
		common.SysError("rpm stats: flush to Redis failed, dropping pending counts: " + err.Error())
	}
}

// rpmStatsWindow is the settled minute a reading covers.
type rpmStatsWindow struct {
	source         string
	start, end     int64
	oldest, latest int64
}

func (r *rpmStatsRecorder) window(at time.Time) rpmStatsWindow {
	latest := at.Add(-rpmStatsSettleDelay).Unix()/rpmStatsBucketSeconds - 1
	oldest := latest - rpmStatsWindowBuckets + 1
	w := rpmStatsWindow{
		start:  oldest * rpmStatsBucketSeconds,
		end:    (latest + 1) * rpmStatsBucketSeconds,
		oldest: oldest,
		latest: latest,
	}
	r.mu.Lock()
	switch {
	case !r.enabled:
		w.source = RpmStatsSourceDisabled
	case !r.useRedis:
		w.source = RpmStatsSourceMemory
	default:
		w.source = RpmStatsSourceRedis
	}
	r.mu.Unlock()
	return w
}

// rpmStatsQuery reads one hash over a window; nil fields reads every field.
type rpmStatsQuery struct {
	hash   rpmStatsHash
	fields []string
}

// sum adds up every query over the window, returning one field -> count map
// per query in the same order.
func (r *rpmStatsRecorder) sum(w rpmStatsWindow, queries []rpmStatsQuery) ([]map[string]int64, error) {
	sums := make([]map[string]int64, len(queries))
	for i := range sums {
		sums[i] = map[string]int64{}
	}
	switch w.source {
	case RpmStatsSourceDisabled:
		return sums, nil
	case RpmStatsSourceMemory:
		r.mu.Lock()
		defer r.mu.Unlock()
		for idx := w.oldest; idx <= w.latest; idx++ {
			bucket := r.buckets[idx]
			if bucket == nil {
				continue
			}
			for i, query := range queries {
				counts := bucket.counts[query.hash]
				if query.fields == nil {
					for field, n := range counts {
						sums[i][field] += n
					}
					continue
				}
				for _, field := range query.fields {
					if n := counts[field]; n != 0 {
						sums[i][field] += n
					}
				}
			}
		}
		return sums, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpmStatsRedisTimeout)
	defer cancel()
	pipe := common.RDB.Pipeline()
	type pending struct {
		query int
		all   *redis.StringStringMapCmd
		some  *redis.SliceCmd
	}
	var cmds []pending
	for i, query := range queries {
		if query.fields != nil && len(query.fields) == 0 {
			continue
		}
		for idx := w.oldest; idx <= w.latest; idx++ {
			key := query.hash.redisKey(idx)
			if query.fields == nil {
				cmds = append(cmds, pending{query: i, all: pipe.HGetAll(ctx, key)})
			} else {
				cmds = append(cmds, pending{query: i, some: pipe.HMGet(ctx, key, query.fields...)})
			}
		}
	}
	if len(cmds) == 0 {
		return sums, nil
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return sums, err
	}
	for _, cmd := range cmds {
		if cmd.all != nil {
			for field, value := range cmd.all.Val() {
				if n, err := strconv.ParseInt(value, 10, 64); err == nil {
					sums[cmd.query][field] += n
				}
			}
			continue
		}
		for j, value := range cmd.some.Val() {
			text, ok := value.(string)
			if !ok {
				continue
			}
			if n, err := strconv.ParseInt(text, 10, 64); err == nil {
				sums[cmd.query][queries[cmd.query].fields[j]] += n
			}
		}
	}
	return sums, nil
}

// windowUsers returns every user settled within the window with the
// username of their latest consume log.
func (r *rpmStatsRecorder) windowUsers(w rpmStatsWindow) (map[int]string, error) {
	users := map[int]string{}
	switch w.source {
	case RpmStatsSourceDisabled:
		return users, nil
	case RpmStatsSourceMemory:
		r.mu.Lock()
		defer r.mu.Unlock()
		for idx := w.oldest; idx <= w.latest; idx++ {
			if bucket := r.buckets[idx]; bucket != nil {
				for id, name := range bucket.users {
					users[id] = name
				}
			}
		}
		return users, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpmStatsRedisTimeout)
	defer cancel()
	pipe := common.RDB.Pipeline()
	cmds := make([]*redis.StringStringMapCmd, 0, rpmStatsWindowBuckets)
	for idx := w.oldest; idx <= w.latest; idx++ {
		cmds = append(cmds, pipe.HGetAll(ctx, rpmStatsHash{kind: statsKindUsers}.redisKey(idx)))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return users, err
	}
	for _, cmd := range cmds {
		for field, name := range cmd.Val() {
			if id, err := strconv.Atoi(field); err == nil {
				users[id] = name
			}
		}
	}
	return users, nil
}

func idCounts(counts map[string]int64) map[int]int64 {
	result := make(map[int]int64, len(counts))
	for field, n := range counts {
		if id, err := strconv.Atoi(field); err == nil {
			result[id] += n
		}
	}
	return result
}

// readTotals reads the attempts of attemptKind and the settled usage of
// statsKind for the given ids; every id is present, zero when idle.
func (r *rpmStatsRecorder) readTotals(at time.Time, attemptKind string, statsKind string, ids []int) (RpmReading, error) {
	w := r.window(at)
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	fields := make([]string, len(ids))
	for i, id := range ids {
		fields[i] = strconv.Itoa(id)
	}
	sums, err := r.sum(w, []rpmStatsQuery{
		{hash: rpmStatsHash{kind: attemptKind}, fields: fields},
		{hash: rpmStatsHash{kind: statsKind}, fields: tokenStatsFields(fields)},
	})
	reading := RpmReading{
		Source:      w.source,
		WindowStart: w.start,
		WindowEnd:   w.end,
		Counts:      idCounts(sums[0]),
		Tokens:      idTokenStats(sums[1]),
	}
	for _, id := range ids {
		if _, ok := reading.Counts[id]; !ok {
			reading.Counts[id] = 0
		}
		if _, ok := reading.Tokens[id]; !ok {
			reading.Tokens[id] = TokenStats{}
		}
	}
	return reading, err
}

func (r *rpmStatsRecorder) readChannelUsers(at time.Time, channelID int) (RpmReading, error) {
	w := r.window(at)
	sums, err := r.sum(w, []rpmStatsQuery{
		{hash: rpmStatsHash{kind: rpmKindChannelUser, owner: channelID}},
		{hash: rpmStatsHash{kind: statsKindChannelUser, owner: channelID}},
	})
	return RpmReading{
		Source:      w.source,
		WindowStart: w.start,
		WindowEnd:   w.end,
		Counts:      idCounts(sums[0]),
		Tokens:      idTokenStats(sums[1]),
	}, err
}

func (r *rpmStatsRecorder) readUserChannels(at time.Time, userID int) (RpmReading, error) {
	w := r.window(at)
	sums, err := r.sum(w, []rpmStatsQuery{
		{hash: rpmStatsHash{kind: rpmKindUserChannel, owner: userID}},
		{hash: rpmStatsHash{kind: statsKindDetail, owner: userID}},
	})
	reading := RpmReading{
		Source:      w.source,
		WindowStart: w.start,
		WindowEnd:   w.end,
		Counts:      idCounts(sums[0]),
		Tokens:      map[int]TokenStats{},
	}
	for field, n := range sums[1] {
		metric, detail, ok := parseDetailField(field)
		if !ok {
			continue
		}
		stats := reading.Tokens[detail.channelID]
		stats.add(metric, n)
		reading.Tokens[detail.channelID] = stats
	}
	return reading, err
}
