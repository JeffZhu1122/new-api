package model

import (
	"bytes"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"model_price": 0.004,
		"admin_info": map[string]any{
			"quota_saturation": map[string]any{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

func TestTaskPluginLogVisibilityIsRoleSeparated(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"model_price": 1.25,
		"admin_info": map[string]any{
			"task_plugin": map[string]any{
				"key":     "document-parser",
				"name":    "Document Parser",
				"version": "1.2.3",
			},
		},
		"root_info": map[string]any{
			"upstream_task_id": "upstream-private",
			"task_plugin": map[string]any{
				"generation": 42,
			},
		},
	})

	t.Run("user", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		formatUserLogs(logs, 0)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.NotContains(t, parsed, "admin_info")
		assert.NotContains(t, parsed, "root_info")
		assert.Equal(t, 1.25, parsed["model_price"])
	})

	t.Run("admin", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		FormatAdminLogs(logs)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Contains(t, parsed, "admin_info")
		assert.NotContains(t, parsed, "root_info")
	})

	t.Run("root", func(t *testing.T) {
		logs := []*Log{{Other: other}}
		FormatRootLogs(logs)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Contains(t, parsed, "admin_info")
		assert.Contains(t, parsed, "root_info")
	})
}

func TestLegacyLogOtherVisibilityIsRoleSeparated(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"request_path":  "/v1/chat/completions",
		"channel_id":    202,
		"channel_name":  "legacy-secret-channel",
		"channel_type":  1,
		"reject_reason": "legacy-policy-rejection",
		"admin_info": map[string]any{
			"existing_admin_field": "preserved",
		},
		"root_info": map[string]any{
			"upstream_request_id": "upstream-private",
		},
		"audit_info": map[string]any{
			"method": "POST",
		},
	})

	t.Run("user", func(t *testing.T) {
		logs := []*Log{{
			Id:          99,
			ChannelId:   77,
			ChannelName: "resolved-secret-channel",
			Other:       other,
		}}

		formatUserLogs(logs, 10)

		assert.Equal(t, 11, logs[0].Id)
		assert.Equal(t, 77, logs[0].ChannelId)
		assert.Empty(t, logs[0].ChannelName)
		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Equal(t, "/v1/chat/completions", parsed["request_path"])
		for _, key := range []string{
			"channel_id",
			"channel_name",
			"channel_type",
			"reject_reason",
			"admin_info",
			"root_info",
			"audit_info",
		} {
			assert.NotContains(t, parsed, key)
		}
	})

	t.Run("admin", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		FormatAdminLogs(logs)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Equal(t, "legacy-secret-channel", parsed["channel_name"])
		assert.NotContains(t, parsed, "reject_reason")
		assert.NotContains(t, parsed, "root_info")
		assert.Contains(t, parsed, "audit_info")
		adminInfo, ok := parsed["admin_info"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "preserved", adminInfo["existing_admin_field"])
		assert.Equal(t, "legacy-policy-rejection", adminInfo["reject_reason"])
	})

	t.Run("root", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		FormatRootLogs(logs)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Equal(t, "legacy-secret-channel", parsed["channel_name"])
		assert.NotContains(t, parsed, "reject_reason")
		assert.Contains(t, parsed, "root_info")
		assert.Contains(t, parsed, "audit_info")
		adminInfo, ok := parsed["admin_info"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "preserved", adminInfo["existing_admin_field"])
		assert.Equal(t, "legacy-policy-rejection", adminInfo["reject_reason"])
	})
}

func TestLegacyRejectReasonDoesNotOverrideScopedValue(t *testing.T) {
	other := common.MapToJsonStr(map[string]any{
		"reject_reason": "legacy-value",
		"admin_info": map[string]any{
			"reject_reason": "scoped-value",
		},
	})
	logs := []*Log{{Other: other}}

	FormatRootLogs(logs)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, parsed, "reject_reason")
	adminInfo, ok := parsed["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "scoped-value", adminInfo["reject_reason"])
}

func TestLegacyRejectReasonHandlesNullAdminInfo(t *testing.T) {
	logs := []*Log{{Other: `{"reject_reason":"legacy-value","admin_info":null}`}}

	FormatAdminLogs(logs)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, parsed, "reject_reason")
	adminInfo, ok := parsed["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "legacy-value", adminInfo["reject_reason"])
}

func TestLogFormattingPreservesLargeIntegerLexemes(t *testing.T) {
	const other = `{"public_id":9007199254740993,"admin_info":{"admin_id":9007199254740995},"root_info":{"generation":18446744073709551615}}`

	t.Run("user", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		formatUserLogs(logs, 0)

		assert.Contains(t, logs[0].Other, `"public_id":9007199254740993`)
		assert.NotContains(t, logs[0].Other, "admin_id")
		assert.NotContains(t, logs[0].Other, "generation")
	})

	t.Run("admin", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		FormatAdminLogs(logs)

		assert.Contains(t, logs[0].Other, `"public_id":9007199254740993`)
		assert.Contains(t, logs[0].Other, `"admin_id":9007199254740995`)
		assert.NotContains(t, logs[0].Other, "generation")
	})

	t.Run("root", func(t *testing.T) {
		logs := []*Log{{Other: other}}

		FormatRootLogs(logs)

		assert.Equal(t, other, logs[0].Other)
	})

	t.Run("unprivileged", func(t *testing.T) {
		const unprivileged = `{"public_id":9007199254740993,"model_price":0.004}`

		userLogs := []*Log{{Other: unprivileged}}
		formatUserLogs(userLogs, 0)
		assert.Equal(t, unprivileged, userLogs[0].Other)

		adminLogs := []*Log{{Other: unprivileged}}
		FormatAdminLogs(adminLogs)
		assert.Equal(t, unprivileged, adminLogs[0].Other)
	})
}

// captureServerInfoLog collects the INFO lines the server log writes.
func captureServerInfoLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	common.LogWriterMu.Lock()
	previous := gin.DefaultWriter
	gin.DefaultWriter = &buffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = previous
		common.LogWriterMu.Unlock()
	})
	return &buffer
}

func TestConsumeLogWritesARequestSummaryLine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name          string
		consumeLogged bool
		params        RecordConsumeLogParams
		other         map[string]any
		want          string
	}{
		{
			name:          "a Claude stream with cache reads and writes",
			consumeLogged: true,
			params:        RecordConsumeLogParams{ChannelId: 3, ModelName: "claude-sonnet-4", PromptTokens: 1000, CompletionTokens: 200, Quota: 1500, IsStream: true, Group: "vip"},
			other:         map[string]any{"frt": float64(350), "cache_tokens": 100, "cache_creation_tokens": 40},
			want:          `request completed: user_id=1 model="claude-sonnet-4" channel_id=3 channel_name="Claude main" group="vip" stream=true input_tokens=1000 output_tokens=200 cache_read_tokens=100 cache_write_tokens=40 quota=1500 ttft_ms=350 `,
		},
		{
			// The settlement logs cache_write_tokens as the sum of the 5m and 1h writes.
			name:          "cache writes split into 5m and 1h use the normalized total",
			consumeLogged: true,
			params:        RecordConsumeLogParams{ChannelId: 3, ModelName: "claude-opus-4", PromptTokens: 10, CompletionTokens: 2, Quota: 90, IsStream: true, Group: "vip"},
			other:         map[string]any{"frt": float64(80), "cache_tokens": 0, "cache_creation_tokens": 40, "cache_creation_tokens_5m": 30, "cache_creation_tokens_1h": 30, "cache_write_tokens": 60},
			want:          `cache_read_tokens=0 cache_write_tokens=60 quota=90 ttft_ms=80 `,
		},
		{
			name:          "an OpenAI cache write reported as cache_write_tokens",
			consumeLogged: true,
			params:        RecordConsumeLogParams{ChannelId: 3, ModelName: "gpt-5", PromptTokens: 2000, CompletionTokens: 20, Quota: 300, IsStream: true, Group: "default"},
			other:         map[string]any{"frt": float64(500), "cache_tokens": 1024, "cache_creation_tokens": 512, "cache_write_tokens": 512},
			want:          `model="gpt-5" channel_id=3 channel_name="Claude main" group="default" stream=true input_tokens=2000 output_tokens=20 cache_read_tokens=1024 cache_write_tokens=512 quota=300 ttft_ms=500 `,
		},
		{
			name:          "a non-stream request without a first token time, consume logs off",
			consumeLogged: false,
			params:        RecordConsumeLogParams{ChannelId: 3, ModelName: "gpt-4o", PromptTokens: 12, CompletionTokens: 5, Quota: 17, Group: "default"},
			other:         map[string]any{"frt": float64(-1760000000000), "cache_tokens": 0},
			want:          `request completed: user_id=1 model="gpt-4o" channel_id=3 channel_name="Claude main" group="default" stream=false input_tokens=12 output_tokens=5 cache_read_tokens=0 cache_write_tokens=0 quota=17 ttft_ms=0 `,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := common.LogConsumeEnabled
			common.LogConsumeEnabled = tc.consumeLogged
			t.Cleanup(func() { common.LogConsumeEnabled = previous })
			serverLog := captureServerInfoLog(t)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyChannelName, "Claude main")
			common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now().Add(-2*time.Second))
			c.Set(common.UpstreamRequestIdKey, "req_up_9")
			other := NewLogOther()
			other.MergePublic(tc.other)
			tc.params.Other = other

			RecordConsumeLog(c, 1, tc.params)

			assert.Contains(t, serverLog.String(), tc.want)
			assert.Contains(t, serverLog.String(), `upstream_request_id="req_up_9"`)
			// duration_ms covers the whole request and tps is output tokens per second of it.
			match := regexp.MustCompile(`duration_ms=(\d+) tps=([\d.]+) `).FindStringSubmatch(serverLog.String())
			require.Len(t, match, 3, serverLog.String())
			durationMs, _ := strconv.Atoi(match[1])
			tps, _ := strconv.ParseFloat(match[2], 64)
			assert.GreaterOrEqual(t, durationMs, 2000)
			assert.InDelta(t, float64(tc.params.CompletionTokens)/(float64(durationMs)/1000), tps, 0.01)
		})
	}
}
