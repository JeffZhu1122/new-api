package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheGetRandomSatisfiedChannelSkipsExcludedChannels(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "exclude-runtime-model"
	createChannelSelectAutoGroupsChannel(t, db, 2301, "default", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2302, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")

	retry := 1
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "default",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
	}
	param.AddFailedChannel(2301)

	channel, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2302, channel.Id)
	assert.Equal(t, "default", selectedGroup)

	// 所有渠道均已失败：返回 nil 渠道且无错误，由上层报告耗尽
	param.AddFailedChannel(2302)
	channel, _, err = CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	assert.Nil(t, channel)
}

func TestCacheGetRandomSatisfiedChannelExclusionAdvancesAutoGroup(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "exclude-auto-group-model"
	createChannelSelectAutoGroupsChannel(t, db, 2401, "vip", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2402, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)

	retry := 0
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
	}
	param.AddFailedChannel(2401)

	channel, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 2402, channel.Id)
	assert.Equal(t, "default", selectedGroup)
}

// scheduleSelectContext 构造一个带请求时刻的选路上下文；SelectChannelForRequest
// 以 ContextKeyRequestStartTime 作为时段判定时刻。
func scheduleSelectContext(at time.Time) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, at)
	return ctx
}

func scheduleRetryParam(ctx *gin.Context, modelName string) *RetryParam {
	return &RetryParam{Ctx: ctx, TokenGroup: "default", ModelName: modelName, RequestPath: "/v1/chat/completions", Retry: common.GetPointer(0)}
}

// 2026-09-30 为周三；渠道只在周三 09:00-18:00（上海）可用
var wednesdayDaytime = &kitdto.ChannelSchedule{Timezone: "Asia/Shanghai", Windows: []kitdto.ChannelScheduleWindow{{Days: []int{3}, Start: "09:00", End: "18:00"}}}

var (
	wednesdayEvening = time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC) // 20:00 上海
	wednesdayMorning = time.Date(2026, time.September, 30, 2, 0, 0, 0, time.UTC)  // 10:00 上海
)

func TestSelectChannelForRequestSkipsChannelsOutsideSchedule(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelExtend{}))
	const modelName = "schedule-runtime-model"
	createChannelSelectAutoGroupsChannel(t, db, 2601, "default", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 2602, "default", modelName)
	require.NoError(t, model.UpsertChannelExtend(db, 2601, kitdto.ChannelExtendSettings{Schedule: wednesdayDaytime}))
	model.InitChannelCache()

	// 时段外：只能选到没有时段限制的 2602
	for range 20 {
		ctx := scheduleSelectContext(wednesdayEvening)
		channel, _, selectErr := SelectChannelForRequest(ctx, modelName, scheduleRetryParam(ctx, modelName))
		require.Nil(t, selectErr)
		require.NotNil(t, channel)
		assert.Equal(t, 2602, channel.Id)
	}

	// 时段内：两个渠道都参与随机选择
	seen := make(map[int]bool)
	for range 40 {
		ctx := scheduleSelectContext(wednesdayMorning)
		channel, _, selectErr := SelectChannelForRequest(ctx, modelName, scheduleRetryParam(ctx, modelName))
		require.Nil(t, selectErr)
		require.NotNil(t, channel)
		seen[channel.Id] = true
	}
	assert.Equal(t, map[int]bool{2601: true, 2602: true}, seen)

	// 同一请求多次选路（重试）只挂一次时段过滤器
	ctx := scheduleSelectContext(wednesdayEvening)
	for range 3 {
		_, _, selectErr := SelectChannelForRequest(ctx, modelName, scheduleRetryParam(ctx, modelName))
		require.Nil(t, selectErr)
	}
	scheduleFilters := 0
	for _, filter := range GetChannelConstraints(ctx).Filters {
		if filter.Kind == dto.FilterChannelSchedule {
			scheduleFilters++
		}
	}
	assert.Equal(t, 1, scheduleFilters)
}

func TestSelectChannelForRequestPinnedChannelOutsideSchedule(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelExtend{}))
	const modelName = "schedule-pinned-model"
	createChannelSelectAutoGroupsChannel(t, db, 2701, "default", modelName)
	require.NoError(t, model.UpsertChannelExtend(db, 2701, kitdto.ChannelExtendSettings{Schedule: wednesdayDaytime}))
	model.InitChannelCache()

	// 令牌指定渠道在时段外：视同渠道不可用
	ctx := scheduleSelectContext(wednesdayEvening)
	GetChannelConstraints(ctx).AddPin(dto.ChannelPin{ChannelId: 2701, Source: dto.PinSourceToken, Rank: dto.PinRankToken, RetryMode: dto.PinRetrySingleAttempt})
	channel, _, selectErr := SelectChannelForRequest(ctx, modelName, scheduleRetryParam(ctx, modelName))
	require.NotNil(t, selectErr)
	assert.Nil(t, channel)
	assert.Equal(t, http.StatusForbidden, selectErr.StatusCode)
	assert.Equal(t, i18n.MsgDistributorChannelDisabled, selectErr.MessageID)

	// 任务轮询固定回原渠道：不受时段限制
	ctx = scheduleSelectContext(wednesdayEvening)
	GetChannelConstraints(ctx).AddPin(dto.ChannelPin{ChannelId: 2701, Source: dto.PinSourceOriginTask, Rank: dto.PinRankOriginTask, RetryMode: dto.PinRetrySameChannel})
	channel, _, selectErr = SelectChannelForRequest(ctx, modelName, scheduleRetryParam(ctx, modelName))
	require.Nil(t, selectErr)
	require.NotNil(t, channel)
	assert.Equal(t, 2701, channel.Id)

	// 时段内的令牌指定渠道照常可用
	ctx = scheduleSelectContext(wednesdayMorning)
	GetChannelConstraints(ctx).AddPin(dto.ChannelPin{ChannelId: 2701, Source: dto.PinSourceToken, Rank: dto.PinRankToken, RetryMode: dto.PinRetrySingleAttempt})
	channel, _, selectErr = SelectChannelForRequest(ctx, modelName, scheduleRetryParam(ctx, modelName))
	require.Nil(t, selectErr)
	require.NotNil(t, channel)
	assert.Equal(t, 2701, channel.Id)
}
