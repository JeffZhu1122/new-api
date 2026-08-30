package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const minInputTestModel = "min-input-test-model"

func setupMinInputChannelTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&ChannelExtend{}))
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM channel_extend").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
		require.NoError(t, DB.Exec("DELETE FROM channels").Error)
		require.NoError(t, DB.Exec("DELETE FROM channel_extend").Error)
		common.MemoryCacheEnabled = memoryCacheEnabled
		InitChannelCache()
	})
}

func createMinInputTestChannel(t *testing.T, id int, minInputTokens int) {
	t.Helper()
	weight := uint(100)
	priority := int64(0)
	require.NoError(t, DB.Create(&Channel{
		Id:       id,
		Type:     constant.ChannelTypeOpenAI,
		Key:      fmt.Sprintf("key-%d", id),
		Status:   common.ChannelStatusEnabled,
		Name:     fmt.Sprintf("channel-%d", id),
		Weight:   &weight,
		Models:   minInputTestModel,
		Group:    "default",
		Priority: &priority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     minInputTestModel,
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
	if minInputTokens > 0 {
		require.NoError(t, DB.Create(&ChannelExtend{
			ChannelId:      id,
			MinInputTokens: minInputTokens,
		}).Error)
	}
}

func minInputFilter(estimate int) []dto.ChannelFilter {
	return []dto.ChannelFilter{{Kind: dto.FilterMinInputTokens, InputTokens: estimate}}
}

// min_input_tokens 过滤：估算输入必须严格大于渠道阈值才可入选；
// 未配置（无 extend 行或 0）不受限。内存缓存与 DB 直查两条路径同规则。
func TestGetRandomSatisfiedChannelMinInputTokens(t *testing.T) {
	setupMinInputChannelTest(t)
	createMinInputTestChannel(t, 5201, 1000)
	createMinInputTestChannel(t, 5202, 0)
	createMinInputTestChannel(t, 5203, 100)

	tests := []struct {
		name     string
		filters  []dto.ChannelFilter
		eligible []int
	}{
		{name: "small input only unrestricted channel", filters: minInputFilter(50), eligible: []int{5202}},
		{name: "threshold not strictly exceeded", filters: minInputFilter(100), eligible: []int{5202}},
		{name: "medium input passes low threshold", filters: minInputFilter(500), eligible: []int{5202, 5203}},
		{name: "large input passes all thresholds", filters: minInputFilter(2000), eligible: []int{5201, 5202, 5203}},
		{name: "no filter ignores thresholds", filters: nil, eligible: []int{5201, 5202, 5203}},
	}

	for _, memoryCache := range []bool{true, false} {
		mode := "db"
		if memoryCache {
			mode = "memory_cache"
		}
		t.Run(mode, func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCache
			InitChannelCache()

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					seen := make(map[int]bool)
					for i := 0; i < 30; i++ {
						channel, err := GetRandomSatisfiedChannel("default", minInputTestModel, 0, tt.filters, nil)
						require.NoError(t, err)
						require.NotNil(t, channel)
						assert.Contains(t, tt.eligible, channel.Id)
						seen[channel.Id] = true
					}
					if len(tt.eligible) == 1 {
						assert.Equal(t, map[int]bool{tt.eligible[0]: true}, seen)
					}
				})
			}
		})
	}
}

func TestGetRandomSatisfiedChannelMinInputTokensNoEligibleChannel(t *testing.T) {
	setupMinInputChannelTest(t)
	createMinInputTestChannel(t, 5211, 1000)
	createMinInputTestChannel(t, 5212, 500)

	for _, memoryCache := range []bool{true, false} {
		mode := "db"
		if memoryCache {
			mode = "memory_cache"
		}
		t.Run(mode, func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCache
			InitChannelCache()

			channel, err := GetRandomSatisfiedChannel("default", minInputTestModel, 0, minInputFilter(200), nil)
			require.NoError(t, err)
			assert.Nil(t, channel)
		})
	}
}

func TestHasAnyMinInputTokens(t *testing.T) {
	setupMinInputChannelTest(t)
	createMinInputTestChannel(t, 5221, 0)

	common.MemoryCacheEnabled = true
	InitChannelCache()
	assert.False(t, HasAnyMinInputTokens())

	require.NoError(t, DB.Create(&ChannelExtend{ChannelId: 5221, MinInputTokens: 300}).Error)
	InitChannelCache()
	assert.True(t, HasAnyMinInputTokens())
}

// DB 模式下管理端保存 min_input_tokens 后（更新路径会调用 InitChannelCache），
// TTL 缓存必须立即失效，新配置不能等到一分钟后才生效。
func TestHasAnyMinInputTokensDBModeInvalidatedByInitChannelCache(t *testing.T) {
	setupMinInputChannelTest(t)
	createMinInputTestChannel(t, 5231, 0)

	common.MemoryCacheEnabled = false
	InitChannelCache()
	assert.False(t, HasAnyMinInputTokens()) // 预热 TTL 缓存为 false

	require.NoError(t, DB.Create(&ChannelExtend{ChannelId: 5231, MinInputTokens: 300}).Error)
	InitChannelCache()
	assert.True(t, HasAnyMinInputTokens())
}
