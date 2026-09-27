package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useChannelExtendDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &ChannelExtend{}, &Ability{}))
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
	})
	return db
}

func TestUpsertChannelExtendLifecycle(t *testing.T) {
	useChannelExtendDB(t)

	require.Error(t, UpsertChannelExtend(nil, 0, dto.ChannelExtendSettings{RelayTimeout: 10}))

	// insert
	require.NoError(t, UpsertChannelExtend(nil, 1, dto.ChannelExtendSettings{RelayTimeout: 30, StreamingTimeout: 60}))
	settings, err := GetChannelExtend(1)
	require.NoError(t, err)
	assert.Equal(t, dto.ChannelExtendSettings{RelayTimeout: 30, StreamingTimeout: 60}, settings)

	// update in place
	require.NoError(t, UpsertChannelExtend(nil, 1, dto.ChannelExtendSettings{RelayTimeout: 90}))
	settings, err = GetChannelExtend(1)
	require.NoError(t, err)
	assert.Equal(t, dto.ChannelExtendSettings{RelayTimeout: 90}, settings)

	// all-zero settings clear the row so the channel inherits globals again
	require.NoError(t, UpsertChannelExtend(nil, 1, dto.ChannelExtendSettings{}))
	var count int64
	require.NoError(t, DB.Model(&ChannelExtend{}).Where("channel_id = ?", 1).Count(&count).Error)
	assert.Equal(t, int64(0), count)

	// missing row reads back as zero-value settings, not an error
	settings, err = GetChannelExtend(1)
	require.NoError(t, err)
	assert.True(t, settings.IsZero())
}

func TestUpsertChannelExtendPersistsRateLimits(t *testing.T) {
	useChannelExtendDB(t)

	// 只配置 rpm/tpm 的行必须完整往返，且不得被 IsZero 误判为全零而删除
	require.NoError(t, UpsertChannelExtend(nil, 7, dto.ChannelExtendSettings{RpmLimit: 5, TpmLimit: 100}))
	settings, err := GetChannelExtend(7)
	require.NoError(t, err)
	assert.Equal(t, dto.ChannelExtendSettings{RpmLimit: 5, TpmLimit: 100}, settings)
	assert.False(t, settings.IsZero())

	var count int64
	require.NoError(t, DB.Model(&ChannelExtend{}).Where("channel_id = ?", 7).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestGetChannelExtendSettingsFallsBackToDBWithoutMemoryCache(t *testing.T) {
	useChannelExtendDB(t)
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })

	missing := GetChannelExtendSettings(42)
	assert.True(t, missing.IsZero())

	require.NoError(t, UpsertChannelExtend(nil, 42, dto.ChannelExtendSettings{RelayTimeout: 120}))
	assert.Equal(t, dto.ChannelExtendSettings{RelayTimeout: 120}, GetChannelExtendSettings(42))
}

func TestChannelDeleteCascadesExtend(t *testing.T) {
	useChannelExtendDB(t)

	channel := &Channel{Name: "test", Models: "gpt-4o", Group: "default", Key: "sk-test", ExtendConfig: &dto.ChannelExtendSettings{RelayTimeout: 45}}
	require.NoError(t, channel.Insert())
	require.NotZero(t, channel.Id)

	settings, err := GetChannelExtend(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, 45, settings.RelayTimeout)

	require.NoError(t, channel.Delete())
	settings, err = GetChannelExtend(channel.Id)
	require.NoError(t, err)
	assert.True(t, settings.IsZero())
}

func TestBatchDeleteChannelsCascadesExtend(t *testing.T) {
	useChannelExtendDB(t)

	first := &Channel{Name: "a", Models: "m", Group: "default", Key: "k1", ExtendConfig: &dto.ChannelExtendSettings{StreamingTimeout: 15}}
	second := &Channel{Name: "b", Models: "m", Group: "default", Key: "k2", ExtendConfig: &dto.ChannelExtendSettings{StreamingTimeout: 25}}
	require.NoError(t, first.Insert())
	require.NoError(t, second.Insert())

	deleted, err := BatchDeleteChannels([]int{first.Id, second.Id})
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	var count int64
	require.NoError(t, DB.Model(&ChannelExtend{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestUpsertChannelExtendPersistsResponseHeaderFilter(t *testing.T) {
	db := useChannelExtendDB(t)
	// 新列上重复 AutoMigrate 必须幂等
	require.NoError(t, db.AutoMigrate(&ChannelExtend{}))

	blacklist := dto.ChannelExtendSettings{ResponseHeaderMode: dto.ResponseHeaderModeBlacklist, ResponseHeaders: []string{"OpenAI-Organization", "X-RateLimit-Limit-Requests"}}
	require.NoError(t, UpsertChannelExtend(nil, 9, blacklist))
	settings, err := GetChannelExtend(9)
	require.NoError(t, err)
	assert.Equal(t, blacklist, settings)
	assert.False(t, settings.IsZero())

	// 切换为白名单并缩减列表：原地覆盖，不能残留旧名称
	whitelist := dto.ChannelExtendSettings{ResponseHeaderMode: dto.ResponseHeaderModeWhitelist, ResponseHeaders: []string{"X-Request-Id"}}
	require.NoError(t, UpsertChannelExtend(nil, 9, whitelist))
	settings, err = GetChannelExtend(9)
	require.NoError(t, err)
	assert.Equal(t, whitelist, settings)

	// 关闭过滤但保留其他覆盖：两列都清空，行仍存在
	require.NoError(t, UpsertChannelExtend(nil, 9, dto.ChannelExtendSettings{RelayTimeout: 5}))
	var row ChannelExtend
	require.NoError(t, DB.Where("channel_id = ?", 9).Take(&row).Error)
	assert.Equal(t, "", row.ResponseHeaderMode)
	assert.Equal(t, "", row.ResponseHeaders)
	settings, err = GetChannelExtend(9)
	require.NoError(t, err)
	assert.Equal(t, dto.ChannelExtendSettings{RelayTimeout: 5}, settings)
}
