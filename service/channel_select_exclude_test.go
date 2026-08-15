package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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
