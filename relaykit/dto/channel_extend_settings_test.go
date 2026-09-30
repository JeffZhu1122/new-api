package dto

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelExtendSettingsValidate(t *testing.T) {
	tooManyHeaders := make([]string, MaxChannelResponseHeaderRules+1)
	for i := range tooManyHeaders {
		tooManyHeaders[i] = fmt.Sprintf("X-Rule-%d", i)
	}
	tests := []struct {
		name     string
		settings *ChannelExtendSettings
		wantErr  string
	}{
		{name: "nil settings valid", settings: nil},
		{name: "zero settings valid", settings: &ChannelExtendSettings{}},
		{name: "max boundary valid", settings: &ChannelExtendSettings{RelayTimeout: MaxChannelTimeoutSeconds, StreamingTimeout: MaxChannelTimeoutSeconds, MinInputTokens: MaxChannelInputTokensBound - 1, MaxInputTokens: MaxChannelInputTokensBound}},
		{name: "negative relay timeout rejected", settings: &ChannelExtendSettings{RelayTimeout: -1}, wantErr: "relay_timeout"},
		{name: "oversized relay timeout rejected", settings: &ChannelExtendSettings{RelayTimeout: MaxChannelTimeoutSeconds + 1}, wantErr: "relay_timeout"},
		{name: "negative streaming timeout rejected", settings: &ChannelExtendSettings{StreamingTimeout: -1}, wantErr: "streaming_timeout"},
		{name: "oversized streaming timeout rejected", settings: &ChannelExtendSettings{StreamingTimeout: MaxChannelTimeoutSeconds + 1}, wantErr: "streaming_timeout"},
		{name: "negative min input tokens rejected", settings: &ChannelExtendSettings{MinInputTokens: -1}, wantErr: "min_input_tokens"},
		{name: "oversized min input tokens rejected", settings: &ChannelExtendSettings{MinInputTokens: MaxChannelInputTokensBound + 1}, wantErr: "min_input_tokens"},
		{name: "negative max input tokens rejected", settings: &ChannelExtendSettings{MaxInputTokens: -1}, wantErr: "max_input_tokens"},
		{name: "oversized max input tokens rejected", settings: &ChannelExtendSettings{MaxInputTokens: MaxChannelInputTokensBound + 1}, wantErr: "max_input_tokens"},
		{name: "max equal to min rejected", settings: &ChannelExtendSettings{MinInputTokens: 1000, MaxInputTokens: 1000}, wantErr: "must be greater than min_input_tokens"},
		{name: "max below min rejected", settings: &ChannelExtendSettings{MinInputTokens: 1000, MaxInputTokens: 500}, wantErr: "must be greater than min_input_tokens"},
		{name: "max above min valid", settings: &ChannelExtendSettings{MinInputTokens: 1000, MaxInputTokens: 1001}},
		{name: "max alone valid", settings: &ChannelExtendSettings{MaxInputTokens: 500}},
		{name: "rate limits at max boundary valid", settings: &ChannelExtendSettings{RpmLimit: MaxChannelRateLimitValue, TpmLimit: MaxChannelRateLimitValue}},
		{name: "negative rpm limit rejected", settings: &ChannelExtendSettings{RpmLimit: -1}, wantErr: "rpm_limit"},
		{name: "oversized rpm limit rejected", settings: &ChannelExtendSettings{RpmLimit: MaxChannelRateLimitValue + 1}, wantErr: "rpm_limit"},
		{name: "negative tpm limit rejected", settings: &ChannelExtendSettings{TpmLimit: -1}, wantErr: "tpm_limit"},
		{name: "oversized tpm limit rejected", settings: &ChannelExtendSettings{TpmLimit: MaxChannelRateLimitValue + 1}, wantErr: "tpm_limit"},
		{name: "blacklist with header names valid", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"OpenAI-Organization", "x-ratelimit-remaining-requests"}}},
		{name: "whitelist with header names valid", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeWhitelist, ResponseHeaders: []string{"X-Request-Id"}}},
		{name: "unknown response header mode rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: "allow", ResponseHeaders: []string{"X-Foo"}}, wantErr: "invalid response_header_mode"},
		{name: "response headers without mode rejected", settings: &ChannelExtendSettings{ResponseHeaders: []string{"X-Foo"}}, wantErr: "requires response_header_mode"},
		{name: "response header mode without names rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist}, wantErr: "at least one header"},
		{name: "blank response header name rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{" "}}, wantErr: "invalid response header name"},
		{name: "response header name with space rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"X Foo"}}, wantErr: "token characters"},
		{name: "duplicate response header names rejected ignoring case", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeWhitelist, ResponseHeaders: []string{"X-Foo", "x-foo"}}, wantErr: "duplicate"},
		{name: "too many response header names rejected", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: tooManyHeaders}, wantErr: "too many"},
		{name: "cost ratio and quota limit at max boundary valid", settings: &ChannelExtendSettings{CostRatio: MaxChannelCostRatio, QuotaLimit: MaxChannelQuotaLimit}},
		{name: "fractional cost ratio valid", settings: &ChannelExtendSettings{CostRatio: 0.2}},
		{name: "negative cost ratio rejected", settings: &ChannelExtendSettings{CostRatio: -0.1}, wantErr: "cost_ratio"},
		{name: "oversized cost ratio rejected", settings: &ChannelExtendSettings{CostRatio: MaxChannelCostRatio + 1}, wantErr: "cost_ratio"},
		{name: "nan cost ratio rejected", settings: &ChannelExtendSettings{CostRatio: math.NaN()}, wantErr: "cost_ratio"},
		{name: "infinite cost ratio rejected", settings: &ChannelExtendSettings{CostRatio: math.Inf(1)}, wantErr: "cost_ratio"},
		{name: "negative quota limit rejected", settings: &ChannelExtendSettings{QuotaLimit: -1}, wantErr: "quota_limit"},
		{name: "oversized quota limit rejected", settings: &ChannelExtendSettings{QuotaLimit: MaxChannelQuotaLimit + 1}, wantErr: "quota_limit"},
		{name: "schedule with weekday window valid", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "Asia/Shanghai", Windows: []ChannelScheduleWindow{{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}}}}},
		{name: "schedule crossing midnight every day valid", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "UTC", Windows: []ChannelScheduleWindow{{Start: "22:00", End: "06:00"}}}}},
		{name: "schedule without timezone rejected", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Windows: []ChannelScheduleWindow{{Start: "09:00", End: "18:00"}}}}, wantErr: "timezone is required"},
		{name: "schedule with unknown timezone rejected", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "Mars/Olympus", Windows: []ChannelScheduleWindow{{Start: "09:00", End: "18:00"}}}}, wantErr: "invalid schedule timezone"},
		{name: "schedule without windows rejected", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "UTC"}}, wantErr: "at least one window"},
		{name: "schedule with too many windows rejected", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "UTC", Windows: make([]ChannelScheduleWindow, MaxChannelScheduleWindows+1)}}, wantErr: "too many schedule windows"},
		{name: "schedule window with bad start rejected", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "UTC", Windows: []ChannelScheduleWindow{{Start: "9:00", End: "18:00"}}}}, wantErr: "start"},
		{name: "schedule window with out of range end rejected", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "UTC", Windows: []ChannelScheduleWindow{{Start: "09:00", End: "24:00"}}}}, wantErr: "end"},
		{name: "schedule window with equal start and end rejected", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "UTC", Windows: []ChannelScheduleWindow{{Start: "09:00", End: "09:00"}}}}, wantErr: "must differ"},
		{name: "schedule window with weekday out of range rejected", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "UTC", Windows: []ChannelScheduleWindow{{Days: []int{7}, Start: "09:00", End: "18:00"}}}}, wantErr: "weekday 7"},
		{name: "schedule window with duplicate weekday rejected", settings: &ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "UTC", Windows: []ChannelScheduleWindow{{Days: []int{1, 1}, Start: "09:00", End: "18:00"}}}}, wantErr: "duplicate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestChannelExtendSettingsIsZero(t *testing.T) {
	var nilSettings *ChannelExtendSettings
	assert.True(t, nilSettings.IsZero())
	assert.True(t, (&ChannelExtendSettings{}).IsZero())
	assert.False(t, (&ChannelExtendSettings{RelayTimeout: 1}).IsZero())
	assert.False(t, (&ChannelExtendSettings{StreamingTimeout: 1}).IsZero())
	// min/max_input_tokens 单独配置时不得被当作全零删除
	assert.False(t, (&ChannelExtendSettings{MinInputTokens: 1}).IsZero())
	assert.False(t, (&ChannelExtendSettings{MaxInputTokens: 1}).IsZero())
	// rpm/tpm_limit 单独配置时同样不得被当作全零删除
	assert.False(t, (&ChannelExtendSettings{RpmLimit: 1}).IsZero())
	assert.False(t, (&ChannelExtendSettings{TpmLimit: 1}).IsZero())
	// 响应头过滤单独配置时同样不得被当作全零删除
	assert.False(t, (&ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"X-Foo"}}).IsZero())
	// 成本倍率 / 额度上限单独配置时同样不得被当作全零删除
	assert.False(t, (&ChannelExtendSettings{CostRatio: 0.5}).IsZero())
	assert.False(t, (&ChannelExtendSettings{QuotaLimit: 1}).IsZero())
	// 可用时段单独配置时同样不得被当作全零删除
	assert.False(t, (&ChannelExtendSettings{Schedule: &ChannelSchedule{Timezone: "UTC", Windows: []ChannelScheduleWindow{{Start: "09:00", End: "18:00"}}}}).IsZero())
}

// 2026-09-30 是周三；用 UTC 时刻输入，验证按规则时区换算后再比较
func TestChannelScheduleContains(t *testing.T) {
	shanghai := func(day, hour, minute int) time.Time {
		// 上海 = UTC+8，无夏令时
		return time.Date(2026, time.September, day, hour-8, minute, 0, 0, time.UTC)
	}
	workdays := &ChannelSchedule{Timezone: "Asia/Shanghai", Windows: []ChannelScheduleWindow{{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}}}
	fridayNight := &ChannelSchedule{Timezone: "Asia/Shanghai", Windows: []ChannelScheduleWindow{{Days: []int{5}, Start: "22:00", End: "06:00"}}}
	everyNight := &ChannelSchedule{Timezone: "Asia/Shanghai", Windows: []ChannelScheduleWindow{{Start: "22:00", End: "06:00"}}}
	twoWindows := &ChannelSchedule{Timezone: "Asia/Shanghai", Windows: []ChannelScheduleWindow{{Days: []int{3}, Start: "09:00", End: "12:00"}, {Days: []int{3}, Start: "14:00", End: "18:00"}}}

	tests := []struct {
		name     string
		schedule *ChannelSchedule
		at       time.Time
		want     bool
	}{
		{name: "nil schedule always available", schedule: nil, at: shanghai(30, 3, 0), want: true},
		{name: "workday inside window", schedule: workdays, at: shanghai(30, 10, 0), want: true},
		{name: "workday start boundary inclusive", schedule: workdays, at: shanghai(30, 9, 0), want: true},
		{name: "workday one minute before start", schedule: workdays, at: shanghai(30, 8, 59), want: false},
		{name: "workday end boundary exclusive", schedule: workdays, at: shanghai(30, 18, 0), want: false},
		{name: "saturday outside workday window", schedule: workdays, at: shanghai(26, 10, 0), want: false},
		{name: "friday night after start", schedule: fridayNight, at: shanghai(25, 23, 0), want: true},
		{name: "saturday early morning belongs to friday window", schedule: fridayNight, at: shanghai(26, 5, 59), want: true},
		{name: "saturday morning after window end", schedule: fridayNight, at: shanghai(26, 6, 0), want: false},
		{name: "saturday night not a friday start", schedule: fridayNight, at: shanghai(26, 23, 0), want: false},
		{name: "thursday early morning not a friday window", schedule: fridayNight, at: shanghai(24, 3, 0), want: false},
		{name: "every night covers wednesday early morning", schedule: everyNight, at: shanghai(30, 1, 30), want: true},
		{name: "every night excludes daytime", schedule: everyNight, at: shanghai(30, 12, 0), want: false},
		{name: "second window of the day", schedule: twoWindows, at: shanghai(30, 15, 0), want: true},
		{name: "gap between two windows", schedule: twoWindows, at: shanghai(30, 13, 0), want: false},
		{name: "unloadable timezone fails open", schedule: &ChannelSchedule{Timezone: "Mars/Olympus", Windows: []ChannelScheduleWindow{{Start: "09:00", End: "10:00"}}}, at: shanghai(30, 3, 0), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.schedule.Contains(tt.at))
		})
	}

	// AvailableAt 是选路谓词的入口：无设置或无规则全天可用
	var nilSettings *ChannelExtendSettings
	assert.True(t, nilSettings.AvailableAt(shanghai(30, 3, 0)))
	assert.True(t, (&ChannelExtendSettings{}).AvailableAt(shanghai(30, 3, 0)))
	assert.False(t, (&ChannelExtendSettings{Schedule: workdays}).AvailableAt(shanghai(30, 3, 0)))
}

func TestChannelExtendSettingsQuotaLimitReached(t *testing.T) {
	var nilSettings *ChannelExtendSettings
	assert.False(t, nilSettings.QuotaLimitReached(math.MaxInt64))
	// 未设上限的渠道永远不会触发
	assert.False(t, (&ChannelExtendSettings{}).QuotaLimitReached(math.MaxInt64))
	limited := &ChannelExtendSettings{QuotaLimit: 5_000_000}
	assert.False(t, limited.QuotaLimitReached(4_999_999))
	// 上限为包含边界：恰好达到即触发
	assert.True(t, limited.QuotaLimitReached(5_000_000))
	assert.True(t, limited.QuotaLimitReached(5_000_001))
}

func TestChannelExtendSettingsAllowsResponseHeader(t *testing.T) {
	blacklist := &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"OpenAI-Organization", " x-ratelimit-remaining-requests "}}
	whitelist := &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeWhitelist, ResponseHeaders: []string{"X-Request-Id"}}
	tests := []struct {
		name     string
		settings *ChannelExtendSettings
		header   string
		want     bool
	}{
		{name: "nil settings copy every header", settings: nil, header: "OpenAI-Organization", want: true},
		{name: "no mode copies every header", settings: &ChannelExtendSettings{}, header: "OpenAI-Organization", want: true},
		{name: "blacklist drops a listed header ignoring case", settings: blacklist, header: "openai-organization", want: false},
		{name: "blacklist trims whitespace around rules", settings: blacklist, header: "X-RateLimit-Remaining-Requests", want: false},
		{name: "blacklist keeps an unlisted header", settings: blacklist, header: "X-Request-Id", want: true},
		{name: "whitelist keeps a listed header ignoring case", settings: whitelist, header: "x-request-id", want: true},
		{name: "whitelist drops an unlisted header", settings: whitelist, header: "OpenAI-Organization", want: false},
		{name: "whitelist always keeps Content-Type", settings: whitelist, header: "Content-Type", want: true},
		{name: "blacklist cannot drop Content-Encoding", settings: &ChannelExtendSettings{ResponseHeaderMode: ResponseHeaderModeBlacklist, ResponseHeaders: []string{"Content-Encoding"}}, header: "content-encoding", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.settings.AllowsResponseHeader(tt.header))
		})
	}
}
