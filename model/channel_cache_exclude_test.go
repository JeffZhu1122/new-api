package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const excludeTestModel = "exclude-test-model"

func setupChannelExcludeTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
		require.NoError(t, DB.Exec("DELETE FROM channels").Error)
		common.MemoryCacheEnabled = memoryCacheEnabled
		InitChannelCache()
	})
}

func createExcludeTestChannel(t *testing.T, id int, priority int64) {
	t.Helper()
	weight := uint(100)
	require.NoError(t, DB.Create(&Channel{
		Id:       id,
		Type:     constant.ChannelTypeOpenAI,
		Key:      fmt.Sprintf("key-%d", id),
		Status:   common.ChannelStatusEnabled,
		Name:     fmt.Sprintf("channel-%d", id),
		Weight:   &weight,
		Models:   excludeTestModel,
		Group:    "default",
		Priority: &priority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     excludeTestModel,
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

func TestGetRandomSatisfiedChannelExcludesFailedChannels(t *testing.T) {
	setupChannelExcludeTest(t)
	createExcludeTestChannel(t, 3101, 0)
	createExcludeTestChannel(t, 3102, 0)
	common.MemoryCacheEnabled = true
	InitChannelCache()

	exclude := map[int]bool{3101: true}
	for i := 0; i < 10; i++ {
		channel, err := GetRandomSatisfiedChannel("default", excludeTestModel, 0, nil, exclude)
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.Equal(t, 3102, channel.Id)
	}
}

func TestGetRandomSatisfiedChannelExclusionPicksHighestRemainingPriority(t *testing.T) {
	setupChannelExcludeTest(t)
	createExcludeTestChannel(t, 3201, 10)
	createExcludeTestChannel(t, 3202, 10)
	createExcludeTestChannel(t, 3203, 5)
	common.MemoryCacheEnabled = true
	InitChannelCache()

	// retry=1 在旧语义下会降到优先级 5；排除模式下应改为剩余渠道中的最高优先级层
	channel, err := GetRandomSatisfiedChannel("default", excludeTestModel, 1, nil, map[int]bool{3201: true})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 3202, channel.Id)
}

func TestGetRandomSatisfiedChannelAllExcludedReturnsNil(t *testing.T) {
	setupChannelExcludeTest(t)
	createExcludeTestChannel(t, 3301, 0)
	common.MemoryCacheEnabled = true
	InitChannelCache()

	channel, err := GetRandomSatisfiedChannel("default", excludeTestModel, 0, nil, map[int]bool{3301: true})
	require.NoError(t, err)
	assert.Nil(t, channel)
}

func TestGetRandomSatisfiedChannelWithoutExclusionKeepsPriorityTierWalk(t *testing.T) {
	setupChannelExcludeTest(t)
	createExcludeTestChannel(t, 3401, 10)
	createExcludeTestChannel(t, 3402, 5)
	common.MemoryCacheEnabled = true
	InitChannelCache()

	first, err := GetRandomSatisfiedChannel("default", excludeTestModel, 0, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, 3401, first.Id)

	second, err := GetRandomSatisfiedChannel("default", excludeTestModel, 1, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, 3402, second.Id)

	clamped, err := GetRandomSatisfiedChannel("default", excludeTestModel, 5, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, clamped)
	assert.Equal(t, 3402, clamped.Id)
}

func TestGetChannelExcludesFailedChannelsOnDatabasePath(t *testing.T) {
	setupChannelExcludeTest(t)
	createExcludeTestChannel(t, 3501, 10)
	createExcludeTestChannel(t, 3502, 10)
	createExcludeTestChannel(t, 3503, 5)
	common.MemoryCacheEnabled = false

	channel, err := GetChannel("default", excludeTestModel, 0, nil, map[int]bool{3501: true})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 3502, channel.Id)

	// 最高优先级层全部失败后应回落到剩余渠道中的最高优先级
	channel, err = GetChannel("default", excludeTestModel, 0, nil, map[int]bool{3501: true, 3502: true})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 3503, channel.Id)

	channel, err = GetChannel("default", excludeTestModel, 0, nil, map[int]bool{3501: true, 3502: true, 3503: true})
	require.NoError(t, err)
	assert.Nil(t, channel)
}
