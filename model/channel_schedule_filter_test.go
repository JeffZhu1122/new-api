package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shanghaiInstant 返回 2026 年 9 月某日上海时间（UTC+8，无夏令时）对应的 UTC 时刻；
// 2026-09-30 为周三，9 月 31 日会被标准化为 10 月 1 日（周四）。
func shanghaiInstant(day, hour, minute int) time.Time {
	return time.Date(2026, time.September, day, hour-8, minute, 0, 0, time.UTC)
}

func scheduleFilter(at time.Time) []dto.ChannelFilter {
	return []dto.ChannelFilter{{Kind: dto.FilterChannelSchedule, At: at}}
}

func createScheduledTestChannel(t *testing.T, id int, schedule *kitdto.ChannelSchedule) {
	t.Helper()
	createInputTokensTestChannel(t, id, 0, 0)
	if schedule != nil {
		require.NoError(t, UpsertChannelExtend(nil, id, kitdto.ChannelExtendSettings{Schedule: schedule}))
	}
}

var (
	scheduleWorkdays = &kitdto.ChannelSchedule{Timezone: "Asia/Shanghai", Windows: []kitdto.ChannelScheduleWindow{{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}}}
	scheduleNightly  = &kitdto.ChannelSchedule{Timezone: "Asia/Shanghai", Windows: []kitdto.ChannelScheduleWindow{{Start: "22:00", End: "06:00"}}}
)

// 可用时段过滤：时段外的渠道从候选集剔除，无规则的渠道全天可选；内存缓存与
// DB 直查两条路径同规则，DB 路径需要回填 channel_extend。
func TestGetRandomSatisfiedChannelScheduleWindows(t *testing.T) {
	setupInputTokensChannelTest(t)
	createScheduledTestChannel(t, 5301, scheduleWorkdays)
	createScheduledTestChannel(t, 5302, nil)
	createScheduledTestChannel(t, 5303, scheduleNightly)

	tests := []struct {
		name     string
		filters  []dto.ChannelFilter
		eligible []int
	}{
		{name: "wednesday morning", filters: scheduleFilter(shanghaiInstant(30, 10, 0)), eligible: []int{5301, 5302}},
		{name: "wednesday night", filters: scheduleFilter(shanghaiInstant(30, 23, 0)), eligible: []int{5302, 5303}},
		{name: "thursday early morning belongs to the nightly window", filters: scheduleFilter(shanghaiInstant(31, 2, 0)), eligible: []int{5302, 5303}},
		{name: "saturday noon only the unrestricted channel", filters: scheduleFilter(shanghaiInstant(26, 12, 0)), eligible: []int{5302}},
		{name: "no filter ignores schedules", filters: nil, eligible: []int{5301, 5302, 5303}},
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
					for range 40 {
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

func TestGetRandomSatisfiedChannelScheduleNoEligibleChannel(t *testing.T) {
	setupInputTokensChannelTest(t)
	createScheduledTestChannel(t, 5311, scheduleWorkdays)
	createScheduledTestChannel(t, 5312, scheduleNightly)

	for _, memoryCache := range []bool{true, false} {
		mode := "db"
		if memoryCache {
			mode = "memory_cache"
		}
		t.Run(mode, func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCache
			InitChannelCache()

			// 周六 12:00：工作日窗口与夜间窗口都不包含
			channel, err := GetRandomSatisfiedChannel("default", inputTokensTestModel, 0, scheduleFilter(shanghaiInstant(26, 12, 0)), nil)
			require.NoError(t, err)
			assert.Nil(t, channel)
		})
	}
}

// 无渠道配置时段时选路不挂过滤器；保存时段后（更新路径会调用 InitChannelCache）
// 内存标志与 DB 模式的 TTL 缓存都必须立即反映新配置。
func TestHasAnyChannelSchedule(t *testing.T) {
	for _, memoryCache := range []bool{true, false} {
		mode := "db"
		if memoryCache {
			mode = "memory_cache"
		}
		t.Run(mode, func(t *testing.T) {
			setupInputTokensChannelTest(t)
			createScheduledTestChannel(t, 5321, nil)
			common.MemoryCacheEnabled = memoryCache
			InitChannelCache()
			assert.False(t, HasAnyChannelSchedule())

			require.NoError(t, UpsertChannelExtend(nil, 5321, kitdto.ChannelExtendSettings{Schedule: scheduleWorkdays}))
			InitChannelCache()
			assert.True(t, HasAnyChannelSchedule())
		})
	}
}
