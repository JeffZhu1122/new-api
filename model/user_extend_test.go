package model

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserRateLimitOverrideRoundTrip(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_extend")
	})

	// 无记录 → 无覆盖
	override, err := GetUserRateLimitOverride(7)
	require.NoError(t, err)
	assert.Nil(t, override)

	rpm, tpm := 5, 1000
	saved := &dto.RateLimitOverride{
		Default: &dto.RateLimitValues{Rpm: &rpm},
		Models:  map[string]dto.RateLimitValues{"gpt-4o": {Tpm: &tpm}},
	}
	require.NoError(t, UpdateUserRateLimitOverride(7, saved))

	loaded, err := GetUserRateLimitOverride(7)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.NotNil(t, loaded.Default)
	require.NotNil(t, loaded.Default.Rpm)
	assert.Equal(t, 5, *loaded.Default.Rpm)
	assert.Nil(t, loaded.Default.Tpm)
	require.Contains(t, loaded.Models, "gpt-4o")
	require.NotNil(t, loaded.Models["gpt-4o"].Tpm)
	assert.Equal(t, 1000, *loaded.Models["gpt-4o"].Tpm)

	// 二次更新覆盖旧值(upsert 路径)
	newRpm := 9
	require.NoError(t, UpdateUserRateLimitOverride(7, &dto.RateLimitOverride{
		Default: &dto.RateLimitValues{Rpm: &newRpm},
	}))
	loaded, err = GetUserRateLimitOverride(7)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, 9, *loaded.Default.Rpm)
	assert.NotContains(t, loaded.Models, "gpt-4o")

	// 空内容 = 清除覆盖,行被删除
	require.NoError(t, UpdateUserRateLimitOverride(7, &dto.RateLimitOverride{}))
	loaded, err = GetUserRateLimitOverride(7)
	require.NoError(t, err)
	assert.Nil(t, loaded)
	var count int64
	require.NoError(t, DB.Model(&UserExtend{}).Where("user_id = ?", 7).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestUserModelDiscountRoundTrip(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_extend")
	})

	// 无记录 → 无折扣
	discounts, err := GetUserModelDiscount(11)
	require.NoError(t, err)
	assert.Nil(t, discounts)

	require.NoError(t, UpdateUserModelDiscount(11, map[string]float64{"gpt-4o": 0.5, "*": 0.25}))
	discounts, err = GetUserModelDiscount(11)
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"gpt-4o": 0.5, "*": 0.25}, discounts)

	// 二次更新覆盖旧值(upsert 路径)
	require.NoError(t, UpdateUserModelDiscount(11, map[string]float64{"claude-3-opus": 0.75}))
	discounts, err = GetUserModelDiscount(11)
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"claude-3-opus": 0.75}, discounts)

	// 空 map = 清除,行被删除
	require.NoError(t, UpdateUserModelDiscount(11, map[string]float64{}))
	discounts, err = GetUserModelDiscount(11)
	require.NoError(t, err)
	assert.Nil(t, discounts)
	var count int64
	require.NoError(t, DB.Model(&UserExtend{}).Where("user_id = ?", 11).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

// 回归:清除一个 override 列绝不能连带删掉另一个列的数据(整行删除 bug)。
func TestUserExtendClearingOneOverrideKeepsTheOther(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_extend")
	})

	rpm := 5
	require.NoError(t, UpdateUserRateLimitOverride(12, &dto.RateLimitOverride{
		Default: &dto.RateLimitValues{Rpm: &rpm},
	}))
	require.NoError(t, UpdateUserModelDiscount(12, map[string]float64{"*": 0.5}))

	// 清除限流覆盖 → 折扣必须保留
	require.NoError(t, UpdateUserRateLimitOverride(12, nil))
	override, err := GetUserRateLimitOverride(12)
	require.NoError(t, err)
	assert.Nil(t, override)
	discounts, err := GetUserModelDiscount(12)
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"*": 0.5}, discounts)

	// 反向:重设限流后清除折扣 → 限流必须保留
	require.NoError(t, UpdateUserRateLimitOverride(12, &dto.RateLimitOverride{
		Default: &dto.RateLimitValues{Rpm: &rpm},
	}))
	require.NoError(t, UpdateUserModelDiscount(12, nil))
	override, err = GetUserRateLimitOverride(12)
	require.NoError(t, err)
	require.NotNil(t, override)
	assert.Equal(t, 5, *override.Default.Rpm)

	// 两列都清空后行才被回收
	require.NoError(t, UpdateUserRateLimitOverride(12, nil))
	var count int64
	require.NoError(t, DB.Model(&UserExtend{}).Where("user_id = ?", 12).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}
