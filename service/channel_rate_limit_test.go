package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useInMemoryChannelRateLimit(t *testing.T) {
	t.Helper()
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })
}

func TestTakeChannelRateLimitRpmInMemory(t *testing.T) {
	useInMemoryChannelRateLimit(t)

	// 渠道对象自带 ExtendConfig（内存缓存模式的预填形态），不依赖 DB
	channel := &model.Channel{Id: 2601, ExtendConfig: &kitdto.ChannelExtendSettings{RpmLimit: 2}}
	assert.True(t, TakeChannelRateLimit(nil, channel))
	assert.True(t, TakeChannelRateLimit(nil, channel))
	// 固定窗口内第三个请求被拒
	assert.False(t, TakeChannelRateLimit(nil, channel))

	// 未配置限额的渠道永远放行
	assert.True(t, TakeChannelRateLimit(nil, &model.Channel{Id: 2602, ExtendConfig: &kitdto.ChannelExtendSettings{}}))
}

func TestTakeChannelRateLimitTpmInMemory(t *testing.T) {
	useInMemoryChannelRateLimit(t)

	channel := &model.Channel{Id: 2603, ExtendConfig: &kitdto.ChannelExtendSettings{TpmLimit: 100}}
	// 无用量时放行（TPM 为事后记账，检查侧只读当前分钟累计）
	require.True(t, TakeChannelRateLimit(nil, channel))

	// 当前分钟累计 >= 限额后拒绝；极小概率记账与检查跨过分钟边界，重灌一次后必须命中
	common.ChannelTpmMemoryCounter.Add(common.ChannelTpmKey(2603), common.UnixMinute(), 150)
	allowed := TakeChannelRateLimit(nil, channel)
	if allowed {
		common.ChannelTpmMemoryCounter.Add(common.ChannelTpmKey(2603), common.UnixMinute(), 150)
		allowed = TakeChannelRateLimit(nil, channel)
	}
	assert.False(t, allowed)
}

func TestCacheGetRandomSatisfiedChannelFailsOverOnChannelRateLimit(t *testing.T) {
	useInMemoryChannelRateLimit(t)
	db := setupChannelSelectAutoGroupsTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelExtend{}))
	const modelName = "channel-rpm-failover-model"
	createChannelSelectAutoGroupsChannel(t, db, 2605, "default", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2606, "default", modelName)
	// 2605 优先级更高，确定性地被首选；每分钟仅放行 1 个请求
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 2605).Update("priority", 10).Error)
	require.NoError(t, model.UpsertChannelExtend(nil, 2605, kitdto.ChannelExtendSettings{RpmLimit: 1}))
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	newParam := func() *RetryParam {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
		retry := 0
		return &RetryParam{
			Ctx:         ctx,
			TokenGroup:  "default",
			ModelName:   modelName,
			RequestPath: "/v1/chat/completions",
			Retry:       &retry,
		}
	}

	// 第一个请求占用 2605 唯一的 RPM 槽位
	channel, _, err := CacheGetRandomSatisfiedChannel(newParam())
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2605, channel.Id)

	// 第二个请求：2605 饱和，自动转移到 2606，且 2605 记入本请求排除集
	param := newParam()
	channel, _, err = CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2606, channel.Id)
	assert.True(t, param.ExcludeChannelIds[2605])
}

func TestCacheGetRandomSatisfiedChannelFailsOverPastManySaturatedChannels(t *testing.T) {
	useInMemoryChannelRateLimit(t)
	db := setupChannelSelectAutoGroupsTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelExtend{}))
	const modelName = "channel-many-saturated-model"

	// 11 个饱和渠道按优先级排在前面，1 个健康渠道垫底：
	// 无论前面有多少饱和渠道，转移都必须能到达健康渠道（回归：不得设固定重选上限）
	saturatedIds := make([]int, 0, 11)
	for i := 0; i < 11; i++ {
		id := 2610 + i
		saturatedIds = append(saturatedIds, id)
		createChannelSelectAutoGroupsChannel(t, db, id, "default", modelName)
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Update("priority", 100-i).Error)
		require.NoError(t, model.UpsertChannelExtend(nil, id, kitdto.ChannelExtendSettings{TpmLimit: 10}))
	}
	const healthyId = 2621
	createChannelSelectAutoGroupsChannel(t, db, healthyId, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	attempt := func() int {
		for _, id := range saturatedIds {
			common.ChannelTpmMemoryCounter.Add(common.ChannelTpmKey(id), common.UnixMinute(), 100)
		}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
		retry := 0
		channel, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
			Ctx:         ctx,
			TokenGroup:  "default",
			ModelName:   modelName,
			RequestPath: "/v1/chat/completions",
			Retry:       &retry,
		})
		require.NoError(t, err)
		require.NotNil(t, channel)
		return channel.Id
	}

	got := attempt()
	if got != healthyId {
		// 极小概率灌入用量与检查恰好跨过分钟边界，重灌一次后必须命中
		got = attempt()
	}
	assert.Equal(t, healthyId, got)
}

func TestRecordModelTokensUsedRecordsChannelTpm(t *testing.T) {
	useInMemoryChannelRateLimit(t)
	prevEnabled := setting.ModelRateLimitEnabled
	setting.ModelRateLimitEnabled = false
	t.Cleanup(func() { setting.ModelRateLimitEnabled = prevEnabled })

	db := setupChannelSelectAutoGroupsTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelExtend{}))
	createChannelSelectAutoGroupsChannel(t, db, 2607, "default", "channel-tpm-record-model")
	require.NoError(t, model.UpsertChannelExtend(nil, 2607, kitdto.ChannelExtendSettings{TpmLimit: 1000}))
	model.InitChannelCache()

	// 渠道级记账独立于用户级总开关（此处已关闭），且键构造与检查侧一致
	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 2607}}
	minute := common.UnixMinute()
	RecordModelTokensUsed(context.Background(), relayInfo, 123)

	used := common.ChannelTpmMemoryCounter.Get(common.ChannelTpmKey(2607), minute)
	if used == 0 {
		used = common.ChannelTpmMemoryCounter.Get(common.ChannelTpmKey(2607), minute+1)
	}
	assert.Equal(t, int64(123), used)
}
