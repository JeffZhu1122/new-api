package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRpmStatsRecorder(useRedis bool) *rpmStatsRecorder {
	return &rpmStatsRecorder{enabled: true, useRedis: useRedis, buckets: map[int64]*rpmStatsBucket{}}
}

func useTestStatsRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled, previousRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = previousEnabled, previousRDB
		_ = client.Close()
	})
	return server
}

// A retried request counts once for the user and once per attempted channel.
func TestBeginAttemptFeedsRpmStats(t *testing.T) {
	previous := rpmStats
	rpmStats = newTestRpmStatsRecorder(false)
	t.Cleanup(func() { rpmStats = previous })

	start := time.Now()
	retried, _ := gin.CreateTestContext(httptest.NewRecorder())
	retried.Set("id", 7)
	RequestPolicy(retried).BeginAttempt(&model.Channel{Id: 1}, "default")
	RequestPolicy(retried).BeginAttempt(&model.Channel{Id: 2}, "default")
	single, _ := gin.CreateTestContext(httptest.NewRecorder())
	single.Set("id", 8)
	RequestPolicy(single).BeginAttempt(&model.Channel{Id: 2}, "default")

	// 15 秒后读取：记录所在的桶必然已结算并落在窗口内
	at := start.Add(15 * time.Second)
	channels, err := rpmStats.readTotals(at, rpmKindChannel, statsKindChannel, []int{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, RpmStatsSourceMemory, channels.Source)
	assert.Equal(t, map[int]int64{1: 1, 2: 2, 3: 0}, channels.Counts)

	users, err := rpmStats.readTotals(at, rpmKindUser, statsKindUser, []int{7, 8})
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 1, 8: 1}, users.Counts)

	channelUsers, err := rpmStats.readChannelUsers(at, 2)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 1, 8: 1}, channelUsers.Counts)

	userChannels, err := rpmStats.readUserChannels(at, 7)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{1: 1, 2: 1}, userChannels.Counts)
}

// Instances flushing to one Redis add up, and a reading covers exactly the
// last settled minute: older buckets and the bucket still being filled are
// left out. Settled token usage follows the same window.
func TestRpmStatsRedisSumsInstancesOverSettledWindow(t *testing.T) {
	server := useTestStatsRedis(t)

	base := time.Unix(1_000_000, 0)
	at := base.Add(65 * time.Second) // window [base, base+60s)
	instanceA := newTestRpmStatsRecorder(true)
	instanceB := newTestRpmStatsRecorder(true)
	instanceA.recordAttempt(base.Add(-time.Second), 1, 7, true)    // before the window
	instanceA.recordAttempt(base, 1, 7, true)                      // first second of the window
	instanceA.recordAttempt(base.Add(30*time.Second), 2, 7, false) // retry of that request
	instanceB.recordAttempt(base.Add(59*time.Second), 1, 8, true)  // last second of the window
	instanceB.recordAttempt(base.Add(61*time.Second), 1, 8, true)  // bucket not settled yet
	alice := statsDetail{tokenName: "k1", modelName: "gpt-4o", channelID: 2, group: "default"}
	bob := statsDetail{tokenName: "k2", modelName: "gpt-4o", channelID: 1, group: "default"}
	instanceA.recordSettled(base.Add(31*time.Second), 7, "alice", alice, TokenStats{Input: 100, CacheRead: 20, CacheWrite: 5, Output: 50})
	instanceB.recordSettled(base.Add(59*time.Second), 8, "bob", bob, TokenStats{Input: 10, Output: 1})
	instanceB.recordSettled(base.Add(61*time.Second), 8, "bob", bob, TokenStats{Input: 999})
	instanceA.flush(context.Background(), at)
	instanceB.flush(context.Background(), at)

	channels, err := instanceA.readTotals(at, rpmKindChannel, statsKindChannel, []int{2, 1, 2})
	require.NoError(t, err)
	assert.Equal(t, RpmStatsSourceRedis, channels.Source)
	assert.Equal(t, base.Unix(), channels.WindowStart)
	assert.Equal(t, base.Unix()+60, channels.WindowEnd)
	assert.Equal(t, map[int]int64{1: 2, 2: 1}, channels.Counts)
	assert.Equal(t, map[int]TokenStats{
		1: {Requests: 1, Input: 10, Output: 1},
		2: {Requests: 1, Input: 100, CacheRead: 20, CacheWrite: 5, Output: 50},
	}, channels.Tokens)

	users, err := instanceB.readTotals(at, rpmKindUser, statsKindUser, []int{7, 8})
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 1, 8: 1}, users.Counts)
	assert.Equal(t, int64(175), users.Tokens[7].Total())

	channelUsers, err := instanceB.readChannelUsers(at, 1)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 1, 8: 1}, channelUsers.Counts)
	assert.Equal(t, map[int]TokenStats{8: {Requests: 1, Input: 10, Output: 1}}, channelUsers.Tokens)

	userChannels, err := instanceA.readUserChannels(at, 7)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{1: 1, 2: 1}, userChannels.Counts)
	assert.Equal(t, map[int]TokenStats{2: {Requests: 1, Input: 100, CacheRead: 20, CacheWrite: 5, Output: 50}}, userChannels.Tokens)

	for _, key := range server.Keys() {
		assert.Positive(t, server.TTL(key), "key %s must expire", key)
	}
}

func TestRpmStatsDisabledReportsSource(t *testing.T) {
	recorder := &rpmStatsRecorder{buckets: map[int64]*rpmStatsBucket{}}
	now := time.Now()
	recorder.recordAttempt(now, 1, 7, true)
	recorder.recordSettled(now, 7, "alice", statsDetail{channelID: 1}, TokenStats{Input: 5})

	reading, err := recorder.readTotals(now.Add(15*time.Second), rpmKindChannel, statsKindChannel, []int{1})
	require.NoError(t, err)
	assert.Equal(t, RpmStatsSourceDisabled, reading.Source)
	assert.Equal(t, map[int]int64{1: 0}, reading.Counts)
	assert.Equal(t, map[int]TokenStats{1: {}}, reading.Tokens)
}

// Input never includes cached tokens: OpenAI-style prompts contain cache reads
// and writes, Claude-style prompts do not.
func TestSettledTokensExcludeCacheFromInput(t *testing.T) {
	cases := []struct {
		name    string
		summary textQuotaSummary
		legacy  bool
		want    TokenStats
	}{
		{
			name:    "openai prompt includes cache read and write",
			summary: textQuotaSummary{PromptTokens: 1000, CacheTokens: 300, CacheCreationTokens: 100, CompletionTokens: 50},
			want:    TokenStats{Input: 600, CacheRead: 300, CacheWrite: 100, Output: 50},
		},
		{
			name:    "openai overlapping cache counts clamp input at zero",
			summary: textQuotaSummary{PromptTokens: 100, CacheTokens: 80, CacheCreationTokens: 50, CompletionTokens: 5},
			want:    TokenStats{Input: 0, CacheRead: 80, CacheWrite: 50, Output: 5},
		},
		{
			name:    "anthropic input excludes cache",
			summary: textQuotaSummary{IsClaudeUsageSemantic: true, PromptTokens: 200, CacheTokens: 300, CacheCreationTokens: 100, CacheCreationTokens5m: 60, CacheCreationTokens1h: 40, CompletionTokens: 70},
			want:    TokenStats{Input: 200, CacheRead: 300, CacheWrite: 100, Output: 70},
		},
		{
			name:    "anthropic split cache writes above the reported total",
			summary: textQuotaSummary{IsClaudeUsageSemantic: true, PromptTokens: 10, CacheCreationTokens: 50, CacheCreationTokens5m: 40, CacheCreationTokens1h: 30},
			want:    TokenStats{Input: 10, CacheWrite: 70},
		},
		{
			name:    "legacy claude-derived openai usage excludes cache",
			summary: textQuotaSummary{PromptTokens: 200, CacheTokens: 300, CacheCreationTokens5m: 10, CompletionTokens: 1},
			legacy:  true,
			want:    TokenStats{Input: 200, CacheRead: 300, CacheWrite: 10, Output: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, settledTextTokens(tc.summary, tc.legacy))
		})
	}

	realtime := settledOpenAITokens(500, 40, dto.InputTokenDetails{CachedTokens: 100, CacheWriteTokens: 50})
	assert.Equal(t, TokenStats{Input: 350, CacheRead: 100, CacheWrite: 50, Output: 40}, realtime)
}

// Every usage-log filter combination reads the same numbers from Redis or
// memory, whether a summary hash or the per-user detail answers it.
func TestLogRateMatchesUsageLogFilters(t *testing.T) {
	base := time.Unix(2_000_000, 0)
	at := base.Add(65 * time.Second)
	record := func(r *rpmStatsRecorder) {
		r.recordSettled(base.Add(10*time.Second), 7, "alice", statsDetail{tokenName: "k1", modelName: "gpt-4o", channelID: 1, group: "default"}, TokenStats{Input: 100, CacheRead: 10, Output: 20})
		r.recordSettled(base.Add(20*time.Second), 7, "alice", statsDetail{tokenName: "k2", modelName: "claude-sonnet-4", channelID: 2, group: "vip"}, TokenStats{Input: 200, CacheWrite: 30, Output: 40})
		r.recordSettled(base.Add(30*time.Second), 8, "robin", statsDetail{tokenName: "k1", modelName: "gpt-4o-mini", channelID: 1, group: "default"}, TokenStats{Input: 300, Output: 60})
		r.recordSettled(base.Add(40*time.Second), 9, "carol", statsDetail{tokenName: "k3", modelName: "GPT-4o", channelID: 2, group: "default"}, TokenStats{Input: 400, Output: 80})
		r.recordSettled(base.Add(-30*time.Second), 9, "carol", statsDetail{tokenName: "k3", modelName: "GPT-4o", channelID: 2, group: "default"}, TokenStats{Input: 5000})
	}
	aliceK1 := TokenStats{Requests: 1, Input: 100, CacheRead: 10, Output: 20}
	aliceK2 := TokenStats{Requests: 1, Input: 200, CacheWrite: 30, Output: 40}
	bob := TokenStats{Requests: 1, Input: 300, Output: 60}
	carol := TokenStats{Requests: 1, Input: 400, Output: 80}
	sum := func(parts ...TokenStats) TokenStats {
		var total TokenStats
		for _, p := range parts {
			total.Requests += p.Requests
			total.Input += p.Input
			total.CacheRead += p.CacheRead
			total.CacheWrite += p.CacheWrite
			total.Output += p.Output
		}
		return total
	}
	cases := []struct {
		name   string
		filter LogRateFilter
		want   TokenStats
	}{
		{"no filter", LogRateFilter{}, sum(aliceK1, aliceK2, bob, carol)},
		{"channel", LogRateFilter{ChannelID: 1}, sum(aliceK1, bob)},
		{"exact username", LogRateFilter{Username: "alice"}, sum(aliceK1, aliceK2)},
		{"self view", LogRateFilter{UserID: 8}, bob},
		{"exact model is case sensitive", LogRateFilter{ModelName: "gpt-4o"}, aliceK1},
		{"model pattern ignores case", LogRateFilter{ModelName: "gpt-4o%"}, sum(aliceK1, bob, carol)},
		{"group", LogRateFilter{Group: "vip"}, aliceK2},
		{"token name across users", LogRateFilter{TokenName: "k1"}, sum(aliceK1, bob)},
		{"channel and user", LogRateFilter{ChannelID: 1, Username: "alice"}, aliceK1},
		{"user and model pattern", LogRateFilter{Username: "alice", ModelName: "claude%"}, aliceK2},
		{"channel and group across users", LogRateFilter{ChannelID: 2, Group: "default"}, carol},
		{"username pattern ignores case", LogRateFilter{Username: "%RO%"}, sum(bob, carol)},
		{"self view with token", LogRateFilter{UserID: 7, TokenName: "k2"}, aliceK2},
		{"unknown user", LogRateFilter{Username: "nobody"}, TokenStats{}},
	}

	recorders := map[string]func(t *testing.T) *rpmStatsRecorder{
		"memory": func(t *testing.T) *rpmStatsRecorder {
			r := newTestRpmStatsRecorder(false)
			record(r)
			return r
		},
		"redis": func(t *testing.T) *rpmStatsRecorder {
			useTestStatsRedis(t)
			r := newTestRpmStatsRecorder(true)
			record(r)
			r.flush(context.Background(), at)
			return r
		},
	}
	for mode, setup := range recorders {
		t.Run(mode, func(t *testing.T) {
			r := setup(t)
			for _, tc := range cases {
				reading, err := r.logRate(at, tc.filter)
				require.NoError(t, err, tc.name)
				assert.Equal(t, tc.want, reading.Stats, tc.name)
				assert.Equal(t, base.Unix(), reading.WindowStart, tc.name)
			}
		})
	}

	_, err := LogRate(LogRateFilter{ModelName: "%%x"})
	assert.Error(t, err, "patterns the log queries refuse are refused here too")
}

// With a dedicated statistics Redis every statistics key lands there and the
// main Redis stays untouched.
func TestRpmStatsDedicatedRedisKeepsMainRedisClean(t *testing.T) {
	mainServer := useTestStatsRedis(t)
	statsServer := miniredis.RunT(t)
	statsClient, err := connectRpmStatsRedis("redis://" + statsServer.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = statsClient.Close() })

	base := time.Unix(3_000_000, 0)
	at := base.Add(65 * time.Second)
	recorder := newTestRpmStatsRecorder(true)
	recorder.rdb = statsClient
	recorder.recordAttempt(base, 1, 7, true)
	recorder.recordSettled(base, 7, "alice", statsDetail{tokenName: "k1", modelName: "gpt-4o", channelID: 1, group: "default"}, TokenStats{Input: 10, Output: 2})
	recorder.flush(context.Background(), at)

	assert.Empty(t, mainServer.Keys())
	assert.NotEmpty(t, statsServer.Keys())
	channels, err := recorder.readTotals(at, rpmKindChannel, statsKindChannel, []int{1})
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{1: 1}, channels.Counts)
	reading, err := recorder.logRate(at, LogRateFilter{Username: "alice", ModelName: "gpt%"})
	require.NoError(t, err)
	assert.Equal(t, TokenStats{Requests: 1, Input: 10, Output: 2}, reading.Stats)
}

// A broken dedicated Redis setting counts in memory instead of quietly moving
// the statistics load onto the main Redis.
func TestRpmStatsInvalidDedicatedRedisNeverUsesMainRedis(t *testing.T) {
	useTestStatsRedis(t)
	previous := rpmStats
	rpmStats = newTestRpmStatsRecorder(false)
	t.Cleanup(func() { rpmStats = previous })
	t.Setenv("RPM_STATS_REDIS_CONN_STRING", "not a redis url")

	StartRpmStats()

	reading, err := ChannelRpm([]int{1})
	require.NoError(t, err)
	assert.Equal(t, RpmStatsSourceMemory, reading.Source)
	assert.Nil(t, rpmStats.rdb)
}
