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

func TestUpsertChannelExtendPersistsForceRetry(t *testing.T) {
	useChannelExtendDB(t)

	// 只开启出错一律重试的行必须完整往返，关闭后整行删除
	require.NoError(t, UpsertChannelExtend(nil, 9, dto.ChannelExtendSettings{ForceRetry: true}))
	settings, err := GetChannelExtend(9)
	require.NoError(t, err)
	assert.Equal(t, dto.ChannelExtendSettings{ForceRetry: true}, settings)

	require.NoError(t, UpsertChannelExtend(nil, 9, dto.ChannelExtendSettings{ForceRetry: false}))
	var count int64
	require.NoError(t, DB.Model(&ChannelExtend{}).Where("channel_id = ?", 9).Count(&count).Error)
	assert.Equal(t, int64(0), count)

	// 迁移重复执行不得报错（新增列幂等）
	require.NoError(t, DB.AutoMigrate(&ChannelExtend{}))
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

func TestUpsertChannelExtendPersistsQuotaLimit(t *testing.T) {
	db := useChannelExtendDB(t)
	// 新列上重复 AutoMigrate 必须幂等
	require.NoError(t, db.AutoMigrate(&ChannelExtend{}))

	// 只配置成本倍率与额度上限的行必须完整往返，且不得被 IsZero 误判为全零而删除
	limited := dto.ChannelExtendSettings{CostRatio: 0.25, QuotaLimit: 5_000_000}
	require.NoError(t, UpsertChannelExtend(nil, 11, limited))
	settings, err := GetChannelExtend(11)
	require.NoError(t, err)
	assert.Equal(t, limited, settings)
	assert.False(t, settings.IsZero())

	// 列表接口按页批量读取：命中的渠道带设置，无行的渠道不出现在结果中
	require.NoError(t, UpsertChannelExtend(nil, 12, dto.ChannelExtendSettings{RelayTimeout: 5}))
	batch, err := GetChannelExtendsByIds([]int{11, 12, 13})
	require.NoError(t, err)
	assert.Equal(t, map[int]dto.ChannelExtendSettings{11: limited, 12: {RelayTimeout: 5}}, batch)
	empty, err := GetChannelExtendsByIds(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	// 清掉上限但保留倍率：原地覆盖
	require.NoError(t, UpsertChannelExtend(nil, 11, dto.ChannelExtendSettings{CostRatio: 0.25}))
	settings, err = GetChannelExtend(11)
	require.NoError(t, err)
	assert.Equal(t, dto.ChannelExtendSettings{CostRatio: 0.25}, settings)
}

func TestUpdateChannelUsedQuotaDisablesChannelAtQuotaLimit(t *testing.T) {
	useChannelExtendDB(t)
	previousHandler := ChannelQuotaLimitReachedHandler
	t.Cleanup(func() { ChannelQuotaLimitReachedHandler = previousHandler })
	var notified []int64
	ChannelQuotaLimitReachedHandler = func(channel *Channel, settings dto.ChannelExtendSettings) {
		notified = append(notified, channel.UsedQuota)
		assert.Equal(t, int64(1000), settings.QuotaLimit)
	}

	require.NoError(t, DB.Create(&Channel{Id: 21, Name: "limited", Type: 1, Key: "k", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 22, Name: "unlimited", Type: 1, Key: "k", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, UpsertChannelExtend(nil, 21, dto.ChannelExtendSettings{QuotaLimit: 1000}))

	// 未达上限：只累加，不禁用
	updateChannelUsedQuota(21, 400)
	channel, err := GetChannelById(21, false)
	require.NoError(t, err)
	assert.Equal(t, int64(400), channel.UsedQuota)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	assert.Empty(t, notified)

	// 恰好达到上限：自动禁用、写入原因、通知一次
	updateChannelUsedQuota(21, 600)
	channel, err = GetChannelById(21, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), channel.UsedQuota)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
	assert.Contains(t, channel.GetOtherInfo()["status_reason"], ChannelStatusReasonQuotaLimitReached)
	assert.True(t, channel.QuotaLimitReached())
	assert.Equal(t, []int64{1000}, notified)

	// 禁用后仍有在途结算：继续累加，但不再重复禁用或通知
	updateChannelUsedQuota(21, 50)
	channel, err = GetChannelById(21, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1050), channel.UsedQuota)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
	assert.Equal(t, []int64{1000}, notified)

	// 未设上限的渠道不受影响
	updateChannelUsedQuota(22, 1_000_000)
	channel, err = GetChannelById(22, false)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	assert.False(t, channel.QuotaLimitReached())
	assert.Equal(t, []int64{1000}, notified)
}

func TestUpdateChannelUsedQuotaRefundNeverDisables(t *testing.T) {
	useChannelExtendDB(t)
	previousHandler := ChannelQuotaLimitReachedHandler
	t.Cleanup(func() { ChannelQuotaLimitReachedHandler = previousHandler })
	ChannelQuotaLimitReachedHandler = func(*Channel, dto.ChannelExtendSettings) {
		t.Fatal("refund must not trigger the quota limit handler")
	}

	// 已超上限但仍启用的渠道（例如管理员刚调低上限）在退款时不应被禁用：只有正向消耗才判定
	require.NoError(t, DB.Create(&Channel{Id: 31, Name: "over", Type: 1, Key: "k", Status: common.ChannelStatusEnabled, UsedQuota: 1500}).Error)
	require.NoError(t, UpsertChannelExtend(nil, 31, dto.ChannelExtendSettings{QuotaLimit: 1000}))

	updateChannelUsedQuota(31, -100)
	channel, err := GetChannelById(31, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1400), channel.UsedQuota)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
}

func TestEnableChannelByTagKeepsExhaustedChannelsDisabled(t *testing.T) {
	useChannelExtendDB(t)
	tag := "prepaid"
	require.NoError(t, DB.Create(&Channel{Id: 41, Name: "exhausted", Type: 1, Key: "k", Tag: &tag, Status: common.ChannelStatusAutoDisabled, UsedQuota: 2000}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 42, Name: "healthy", Type: 1, Key: "k", Tag: &tag, Status: common.ChannelStatusManuallyDisabled, UsedQuota: 2000}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "m", ChannelId: 41, Enabled: false, Tag: &tag}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "m", ChannelId: 42, Enabled: false, Tag: &tag}).Error)
	require.NoError(t, UpsertChannelExtend(nil, 41, dto.ChannelExtendSettings{QuotaLimit: 1000}))
	require.NoError(t, UpsertChannelExtend(nil, 42, dto.ChannelExtendSettings{QuotaLimit: 5000}))

	skipped, err := EnableChannelByTag(tag)
	require.NoError(t, err)
	assert.Equal(t, []int{41}, skipped)

	exhausted, err := GetChannelById(41, false)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, exhausted.Status)
	healthy, err := GetChannelById(42, false)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, healthy.Status)

	var abilities []Ability
	require.NoError(t, DB.Order("channel_id").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.False(t, abilities[0].Enabled, "exhausted channel abilities must stay disabled")
	assert.True(t, abilities[1].Enabled)
}

func TestUpsertChannelExtendPersistsSchedule(t *testing.T) {
	db := useChannelExtendDB(t)
	// 新列上重复 AutoMigrate 必须幂等
	require.NoError(t, db.AutoMigrate(&ChannelExtend{}))

	schedule := &dto.ChannelSchedule{Timezone: "Asia/Shanghai", Windows: []dto.ChannelScheduleWindow{
		{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"},
		{Start: "22:00", End: "06:00"},
	}}
	require.NoError(t, UpsertChannelExtend(nil, 51, dto.ChannelExtendSettings{Schedule: schedule}))
	settings, err := GetChannelExtend(51)
	require.NoError(t, err)
	assert.Equal(t, dto.ChannelExtendSettings{Schedule: schedule}, settings)
	assert.False(t, settings.IsZero())

	// 去掉时段但保留其他覆盖：列清空，行仍存在
	require.NoError(t, UpsertChannelExtend(nil, 51, dto.ChannelExtendSettings{RelayTimeout: 5}))
	var row ChannelExtend
	require.NoError(t, DB.Where("channel_id = ?", 51).Take(&row).Error)
	assert.Equal(t, "", row.Schedule)
	settings, err = GetChannelExtend(51)
	require.NoError(t, err)
	assert.Nil(t, settings.Schedule)

	// 只配置了时段的行在清空后整行删除
	require.NoError(t, UpsertChannelExtend(nil, 52, dto.ChannelExtendSettings{Schedule: schedule}))
	require.NoError(t, UpsertChannelExtend(nil, 52, dto.ChannelExtendSettings{}))
	var count int64
	require.NoError(t, DB.Model(&ChannelExtend{}).Where("channel_id = ?", 52).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}
