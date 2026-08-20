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

const countTokensTestModel = "count-tokens-test-model"

func setupCountTokensChannelTest(t *testing.T) {
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

func createCountTokensTestChannel(t *testing.T, id int, channelType int, otherSettings string) {
	t.Helper()
	weight := uint(100)
	priority := int64(0)
	require.NoError(t, DB.Create(&Channel{
		Id:            id,
		Type:          channelType,
		Key:           fmt.Sprintf("key-%d", id),
		Status:        common.ChannelStatusEnabled,
		Name:          fmt.Sprintf("channel-%d", id),
		Weight:        &weight,
		Models:        countTokensTestModel,
		Group:         "default",
		Priority:      &priority,
		OtherSettings: otherSettings,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     countTokensTestModel,
		ChannelId: id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

// count_tokens 路径只允许「Anthropic 类型且开启 count_tokens_enabled」的渠道；
// 普通路径不受该开关影响。内存缓存与数据库直查两条选择路径必须同规则。
func TestGetRandomSatisfiedChannelCountTokensPath(t *testing.T) {
	setupCountTokensChannelTest(t)
	createCountTokensTestChannel(t, 4201, constant.ChannelTypeAnthropic, `{"count_tokens_enabled":true}`)
	createCountTokensTestChannel(t, 4202, constant.ChannelTypeAnthropic, "")
	createCountTokensTestChannel(t, 4203, constant.ChannelTypeOpenAI, `{"count_tokens_enabled":true}`)

	for _, memoryCache := range []bool{true, false} {
		name := "db"
		if memoryCache {
			name = "memory_cache"
		}
		t.Run(name, func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCache
			InitChannelCache()

			for i := 0; i < 20; i++ {
				channel, err := GetRandomSatisfiedChannel("default", countTokensTestModel, 0, []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: constant.ClaudeCountTokensPath}}, nil)
				require.NoError(t, err)
				require.NotNil(t, channel)
				assert.Equal(t, 4201, channel.Id)
			}

			for i := 0; i < 20; i++ {
				channel, err := GetRandomSatisfiedChannel("default", countTokensTestModel, 0, []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: "/v1/messages"}}, nil)
				require.NoError(t, err)
				require.NotNil(t, channel)
				assert.Contains(t, []int{4201, 4202, 4203}, channel.Id)
			}
		})
	}
}

func TestGetRandomSatisfiedChannelCountTokensPathNoEligibleChannel(t *testing.T) {
	setupCountTokensChannelTest(t)
	createCountTokensTestChannel(t, 4211, constant.ChannelTypeAnthropic, "")
	createCountTokensTestChannel(t, 4212, constant.ChannelTypeOpenAI, `{"count_tokens_enabled":true}`)

	for _, memoryCache := range []bool{true, false} {
		name := "db"
		if memoryCache {
			name = "memory_cache"
		}
		t.Run(name, func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCache
			InitChannelCache()

			channel, err := GetRandomSatisfiedChannel("default", countTokensTestModel, 0, []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: constant.ClaudeCountTokensPath}}, nil)
			require.NoError(t, err)
			assert.Nil(t, channel)
		})
	}
}
