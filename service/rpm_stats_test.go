package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRpmStatsRecorder(useRedis bool) *rpmStatsRecorder {
	return &rpmStatsRecorder{enabled: true, useRedis: useRedis, buckets: map[int64]rpmStatsBucket{}}
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
	channels, err := rpmStats.read(at, rpmStatsHash{kind: rpmKindChannel}, []int{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, RpmStatsSourceMemory, channels.Source)
	assert.Equal(t, map[int]int64{1: 1, 2: 2, 3: 0}, channels.Counts)

	users, err := rpmStats.read(at, rpmStatsHash{kind: rpmKindUser}, []int{7, 8})
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 1, 8: 1}, users.Counts)

	channelUsers, err := rpmStats.read(at, rpmStatsHash{kind: rpmKindChannelUser, owner: 2}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 1, 8: 1}, channelUsers.Counts)

	userChannels, err := rpmStats.read(at, rpmStatsHash{kind: rpmKindUserChannel, owner: 7}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{1: 1, 2: 1}, userChannels.Counts)
}

// Instances flushing to one Redis add up, and a reading covers exactly the
// last settled minute: older buckets and the bucket still being filled are
// left out.
func TestRpmStatsRedisSumsInstancesOverSettledWindow(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled, previousRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = previousEnabled, previousRDB
		_ = client.Close()
	})

	base := time.Unix(1_000_000, 0)
	at := base.Add(65 * time.Second) // window [base, base+60s)
	instanceA := newTestRpmStatsRecorder(true)
	instanceB := newTestRpmStatsRecorder(true)
	instanceA.record(base.Add(-time.Second), 1, 7, true)    // before the window
	instanceA.record(base, 1, 7, true)                      // first second of the window
	instanceA.record(base.Add(30*time.Second), 2, 7, false) // retry of that request
	instanceB.record(base.Add(59*time.Second), 1, 8, true)  // last second of the window
	instanceB.record(base.Add(61*time.Second), 1, 8, true)  // bucket not settled yet
	instanceA.flush(context.Background(), at)
	instanceB.flush(context.Background(), at)

	channels, err := instanceA.read(at, rpmStatsHash{kind: rpmKindChannel}, []int{2, 1, 2})
	require.NoError(t, err)
	assert.Equal(t, RpmStatsSourceRedis, channels.Source)
	assert.Equal(t, base.Unix(), channels.WindowStart)
	assert.Equal(t, base.Unix()+60, channels.WindowEnd)
	assert.Equal(t, map[int]int64{1: 2, 2: 1}, channels.Counts)

	users, err := instanceB.read(at, rpmStatsHash{kind: rpmKindUser}, []int{7, 8})
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 1, 8: 1}, users.Counts)

	channelUsers, err := instanceB.read(at, rpmStatsHash{kind: rpmKindChannelUser, owner: 1}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 1, 8: 1}, channelUsers.Counts)

	userChannels, err := instanceA.read(at, rpmStatsHash{kind: rpmKindUserChannel, owner: 7}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{1: 1, 2: 1}, userChannels.Counts)

	for _, key := range server.Keys() {
		assert.Positive(t, server.TTL(key), "key %s must expire", key)
	}
}

func TestRpmStatsDisabledReportsSource(t *testing.T) {
	recorder := &rpmStatsRecorder{buckets: map[int64]rpmStatsBucket{}}
	now := time.Now()
	recorder.record(now, 1, 7, true)

	reading, err := recorder.read(now.Add(15*time.Second), rpmStatsHash{kind: rpmKindChannel}, []int{1})
	require.NoError(t, err)
	assert.Equal(t, RpmStatsSourceDisabled, reading.Source)
	assert.Equal(t, map[int]int64{1: 0}, reading.Counts)
}
