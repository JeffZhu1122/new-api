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

const inputTokensTestModel = "input-tokens-test-model"

func setupInputTokensChannelTest(t *testing.T) {
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

func createInputTokensTestChannel(t *testing.T, id int, minInputTokens int, maxInputTokens int) {
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
		Models:   inputTokensTestModel,
		Group:    "default",
		Priority: &priority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     inputTokensTestModel,
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
	if minInputTokens > 0 || maxInputTokens > 0 {
		require.NoError(t, DB.Create(&ChannelExtend{
			ChannelId:      id,
			MinInputTokens: minInputTokens,
			MaxInputTokens: maxInputTokens,
		}).Error)
	}
}

func inputTokensFilter(estimate int) []dto.ChannelFilter {
	return []dto.ChannelFilter{{Kind: dto.FilterInputTokens, InputTokens: estimate}}
}

// 输入范围过滤：min 为排他下界（估算须严格大于），max 为包含上界（估算须小于
// 等于）；未配置（无 extend 行或 0）不受限。内存缓存与 DB 直查两条路径同规则。
func TestGetRandomSatisfiedChannelInputTokensRange(t *testing.T) {
	setupInputTokensChannelTest(t)
	createInputTokensTestChannel(t, 5201, 1000, 0)    // min 1000
	createInputTokensTestChannel(t, 5202, 0, 0)       // unrestricted
	createInputTokensTestChannel(t, 5203, 100, 0)     // min 100
	createInputTokensTestChannel(t, 5204, 0, 100)     // max 100
	createInputTokensTestChannel(t, 5205, 1000, 5000) // range (1000, 5000]

	tests := []struct {
		name     string
		filters  []dto.ChannelFilter
		eligible []int
	}{
		{name: "small input passes max channel", filters: inputTokensFilter(50), eligible: []int{5202, 5204}},
		{name: "max boundary inclusive min boundary exclusive", filters: inputTokensFilter(100), eligible: []int{5202, 5204}},
		{name: "just above shared boundary", filters: inputTokensFilter(101), eligible: []int{5202, 5203}},
		{name: "inside range channel window", filters: inputTokensFilter(2000), eligible: []int{5201, 5202, 5203, 5205}},
		{name: "range upper boundary inclusive", filters: inputTokensFilter(5000), eligible: []int{5201, 5202, 5203, 5205}},
		{name: "above range upper boundary", filters: inputTokensFilter(5001), eligible: []int{5201, 5202, 5203}},
		{name: "no filter ignores limits", filters: nil, eligible: []int{5201, 5202, 5203, 5204, 5205}},
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
					for i := 0; i < 40; i++ {
						channel, err := GetRandomSatisfiedChannel("default", inputTokensTestModel, 0, tt.filters, nil)
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

func TestGetRandomSatisfiedChannelInputTokensNoEligibleChannel(t *testing.T) {
	setupInputTokensChannelTest(t)
	createInputTokensTestChannel(t, 5211, 1000, 0)
	createInputTokensTestChannel(t, 5212, 0, 100)

	for _, memoryCache := range []bool{true, false} {
		mode := "db"
		if memoryCache {
			mode = "memory_cache"
		}
		t.Run(mode, func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCache
			InitChannelCache()

			// 200 tokens：低于 5211 的 min，高于 5212 的 max
			channel, err := GetRandomSatisfiedChannel("default", inputTokensTestModel, 0, inputTokensFilter(200), nil)
			require.NoError(t, err)
			assert.Nil(t, channel)
		})
	}
}

func TestHasAnyInputTokensLimit(t *testing.T) {
	setupInputTokensChannelTest(t)
	createInputTokensTestChannel(t, 5221, 0, 0)

	common.MemoryCacheEnabled = true
	InitChannelCache()
	assert.False(t, HasAnyInputTokensLimit())

	require.NoError(t, DB.Create(&ChannelExtend{ChannelId: 5221, MaxInputTokens: 300}).Error)
	InitChannelCache()
	assert.True(t, HasAnyInputTokensLimit())
}

// DB 模式下管理端保存 min/max_input_tokens 后（更新路径会调用 InitChannelCache），
// TTL 缓存必须立即失效，新配置不能等到一分钟后才生效。
func TestHasAnyInputTokensLimitDBModeInvalidatedByInitChannelCache(t *testing.T) {
	setupInputTokensChannelTest(t)
	createInputTokensTestChannel(t, 5231, 0, 0)

	common.MemoryCacheEnabled = false
	InitChannelCache()
	assert.False(t, HasAnyInputTokensLimit()) // 预热 TTL 缓存为 false

	require.NoError(t, DB.Create(&ChannelExtend{ChannelId: 5231, MinInputTokens: 300}).Error)
	InitChannelCache()
	assert.True(t, HasAnyInputTokensLimit())
}
