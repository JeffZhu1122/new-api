package controller

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

// setupLogFilterTest stores consume logs of three channels in the log database
// named by TEST_LOG_FILTER_DIALECT: sqlite by default, or mysql / postgres on
// the disposable loopback instance in TEST_<DIALECT>_DSN.
func setupLogFilterTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dialect := os.Getenv("TEST_LOG_FILTER_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	db, _ := newAuditTestDatabase(t, dialect, os.Getenv("TEST_"+strings.ToUpper(dialect)+"_DSN"))
	db.Logger = logger.Default.LogMode(logger.Silent)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.Channel{}, &model.User{}))
	dbType := common.DatabaseTypeSQLite
	switch dialect {
	case "mysql":
		dbType = common.DatabaseTypeMySQL
	case "postgres":
		dbType = common.DatabaseTypePostgreSQL
	}
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousCache, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(dbType, dbType)
	common.MemoryCacheEnabled, common.RedisEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.MemoryCacheEnabled, common.RedisEnabled = previousCache, previousRedis
	})
	for _, log := range []model.Log{
		{UserId: 1, Username: "alice", Type: model.LogTypeConsume, ModelName: "gpt-4o", ChannelId: 1, Quota: 100, PromptTokens: 100, CompletionTokens: 10, UseTime: 1, FirstResponseMs: 200, CreatedAt: 1000},
		{UserId: 1, Username: "alice", Type: model.LogTypeConsume, ModelName: "gpt-4o-mini", ChannelId: 2, Quota: 200, PromptTokens: 2000, CompletionTokens: 50, UseTime: 5, CreatedAt: 1001},
		{UserId: 1, Username: "alice", Type: model.LogTypeConsume, ModelName: "claude-sonnet-4", ChannelId: 3, Quota: 400, PromptTokens: 50000, CompletionTokens: 800, UseTime: 30, FirstResponseMs: 1500, CreatedAt: 1002},
	} {
		require.NoError(t, db.Create(&log).Error)
	}
}

type logFilterResponse struct {
	Success    bool   `json:"success"`
	MessageKey string `json:"message_key"`
	Data       struct {
		Total int `json:"total"`
		Items []struct {
			ChannelId int    `json:"channel"`
			ModelName string `json:"model_name"`
		} `json:"items"`
		Quota      int    `json:"quota"`
		RateSource string `json:"rate_source"`
	} `json:"data"`
}

// logFilterRequest calls a usage-log handler as the root user alice (id 1).
func logFilterRequest(t *testing.T, handler gin.HandlerFunc, query url.Values) logFilterResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/log/?"+query.Encode(), nil)
	c.Set("role", common.RoleRootUser)
	c.Set("id", 1)
	c.Set("username", "alice")
	handler(c)
	var response logFilterResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return response
}

func TestUsageLogsFilterBySeveralChannels(t *testing.T) {
	setupLogFilterTest(t)
	for _, test := range []struct {
		name, channel string
		channels      []int
		quota         int
	}{
		{"comma separated", "1, 3", []int{1, 3}, 500},
		{"Chinese comma, spaces and repeats", "1，2 2", []int{1, 2}, 300},
		{"one channel", "2", []int{2}, 200},
		{"zero keeps meaning no filter", "0", []int{1, 2, 3}, 700},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := url.Values{"type": {"2"}, "channel": {test.channel}}
			logs := logFilterRequest(t, GetAllLogs, query)
			require.True(t, logs.Success, logs.MessageKey)
			channels := make([]int, 0, len(logs.Data.Items))
			for _, item := range logs.Data.Items {
				channels = append(channels, item.ChannelId)
			}
			assert.ElementsMatch(t, test.channels, channels)
			assert.Equal(t, len(test.channels), logs.Data.Total)

			stat := logFilterRequest(t, GetLogsStat, query)
			require.True(t, stat.Success, stat.MessageKey)
			assert.Equal(t, test.quota, stat.Data.Quota)
		})
	}

	tooMany := make([]string, maxLogChannelFilter+1)
	for i := range tooMany {
		tooMany[i] = strconv.Itoa(i + 1)
	}
	for _, test := range []struct{ name, channel, messageKey string }{
		{"not a number", "1,abc", "Invalid channel ID"},
		{"negative", "-3", "Invalid channel ID"},
		{"too many", strings.Join(tooMany, ","), "Search at most {{max}} channel IDs at once"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, handler := range []gin.HandlerFunc{GetAllLogs, GetLogsStat} {
				response := logFilterRequest(t, handler, url.Values{"channel": {test.channel}})
				assert.False(t, response.Success)
				assert.Equal(t, test.messageKey, response.MessageKey)
			}
		})
	}
}

// The web console sends a model search as %text%; the list and the quota
// statistics must agree on what it matches.
func TestUsageLogsModelSearchMatchesPartOfTheName(t *testing.T) {
	setupLogFilterTest(t)
	for _, test := range []struct {
		name, modelName, channel string
		models                   []string
		quota                    int
	}{
		{"part of the name", "%-4o%", "", []string{"gpt-4o", "gpt-4o-mini"}, 300},
		{"with several channels", "%-4o%", "2,3", []string{"gpt-4o-mini"}, 200},
		{"exact name without wildcards", "gpt-4o", "", []string{"gpt-4o"}, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := url.Values{"type": {"2"}, "model_name": {test.modelName}, "channel": {test.channel}}
			logs := logFilterRequest(t, GetAllLogs, query)
			require.True(t, logs.Success, logs.MessageKey)
			models := make([]string, 0, len(logs.Data.Items))
			for _, item := range logs.Data.Items {
				models = append(models, item.ModelName)
			}
			assert.ElementsMatch(t, test.models, models)

			stat := logFilterRequest(t, GetLogsStat, query)
			require.True(t, stat.Success, stat.MessageKey)
			assert.Equal(t, test.quota, stat.Data.Quota)
		})
	}
}

func TestUsageLogsFilterByTokenRanges(t *testing.T) {
	setupLogFilterTest(t)
	for _, test := range []struct {
		name      string
		query     url.Values
		models    []string
		quota     int
		adminOnly bool
	}{
		{"input at least", url.Values{"prompt_tokens_min": {"1000"}}, []string{"gpt-4o-mini", "claude-sonnet-4"}, 600, false},
		{"input at most, bound included", url.Values{"prompt_tokens_max": {"2000"}}, []string{"gpt-4o", "gpt-4o-mini"}, 300, false},
		{"output between", url.Values{"completion_tokens_min": {"20"}, "completion_tokens_max": {"500"}}, []string{"gpt-4o-mini"}, 200, false},
		{"input and output together", url.Values{"prompt_tokens_min": {"100"}, "completion_tokens_max": {"50"}}, []string{"gpt-4o", "gpt-4o-mini"}, 300, false},
		// Only the admin view filters by channel.
		{"with several channels", url.Values{"prompt_tokens_min": {"1000"}, "channel": {"1,3"}}, []string{"claude-sonnet-4"}, 400, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.query.Set("type", "2")
			views := map[string][2]gin.HandlerFunc{"all logs": {GetAllLogs, GetLogsStat}}
			if !test.adminOnly {
				views["own logs"] = [2]gin.HandlerFunc{GetUserLogs, GetLogsSelfStat}
			}
			for name, handlers := range views {
				logs := logFilterRequest(t, handlers[0], test.query)
				require.True(t, logs.Success, logs.MessageKey)
				models := make([]string, 0, len(logs.Data.Items))
				for _, item := range logs.Data.Items {
					models = append(models, item.ModelName)
				}
				assert.ElementsMatch(t, test.models, models, name)

				stat := logFilterRequest(t, handlers[1], test.query)
				require.True(t, stat.Success, stat.MessageKey)
				assert.Equal(t, test.quota, stat.Data.Quota, name)
				// The live RPM / TPM keep no per-request token counts.
				assert.Equal(t, "filter_unsupported", stat.Data.RateSource, name)
			}
		})
	}

	stat := logFilterRequest(t, GetLogsStat, url.Values{"type": {"2"}})
	require.True(t, stat.Success, stat.MessageKey)
	assert.NotEqual(t, "filter_unsupported", stat.Data.RateSource, "without a token range the live statistics apply")

	for _, test := range []struct {
		name       string
		query      url.Values
		messageKey string
	}{
		{"negative", url.Values{"prompt_tokens_min": {"-1"}}, "Token counts must be whole numbers of 0 or more"},
		{"not a number", url.Values{"completion_tokens_max": {"many"}}, "Token counts must be whole numbers of 0 or more"},
		{"fraction", url.Values{"completion_tokens_min": {"1.5"}}, "Token counts must be whole numbers of 0 or more"},
		{"minimum above maximum", url.Values{"prompt_tokens_min": {"500"}, "prompt_tokens_max": {"100"}}, "The minimum token count cannot be greater than the maximum"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, handler := range []gin.HandlerFunc{GetAllLogs, GetLogsStat, GetUserLogs, GetLogsSelfStat} {
				response := logFilterRequest(t, handler, test.query)
				assert.False(t, response.Success)
				assert.Equal(t, test.messageKey, response.MessageKey)
			}
		})
	}
}

// recordLogsThroughWriters stores logs with the gateway's own writers, so the
// filters are checked against the stored format of other and TTFT.
func recordLogsThroughWriters(t *testing.T) {
	t.Helper()
	previousConsume := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	t.Cleanup(func() { common.LogConsumeEnabled = previousConsume })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("username", "alice")

	for _, consume := range []struct {
		modelName string
		frt       float64
	}{
		{"gpt-4.1", 3500},
		// A first token time that was never set yields a negative frt.
		{"gpt-4.1-nano", -1760000000000},
	} {
		other := model.NewLogOther()
		other.SetPublic("frt", consume.frt)
		model.RecordConsumeLog(c, 1, model.RecordConsumeLogParams{ChannelId: 1, ModelName: consume.modelName, Quota: 800, UseTimeSeconds: 12, Group: "default", Other: other})
	}
	for _, failure := range []struct {
		modelName  string
		statusCode int
	}{
		{"error-429", 429},
		{"error-502", 502},
	} {
		other := model.NewLogOther()
		other.SetPublic("error_type", "upstream_error")
		other.SetPublic("error_code", "bad_response_status_code")
		other.SetPublic("status_code", failure.statusCode)
		other.SetAdmin("is_multi_key", false)
		model.RecordErrorLog(c, 1, 2, failure.modelName, "", "upstream failed", 0, 3, false, "default", other)
	}
}

func TestUsageLogsFilterByUseTimeTTFTAndStatusCodes(t *testing.T) {
	setupLogFilterTest(t)
	recordLogsThroughWriters(t)

	var written []model.Log
	require.NoError(t, model.LOG_DB.Where("model_name LIKE ?", "gpt-4.1%").Order("model_name").Find(&written).Error)
	require.Len(t, written, 2)
	assert.Equal(t, 3500, written[0].FirstResponseMs, "the consume log copies other.frt")
	assert.Zero(t, written[1].FirstResponseMs, "an unset first token time is stored as unknown")

	for _, test := range []struct {
		name   string
		query  url.Values
		models []string
	}{
		{"use time at least", url.Values{"use_time_min": {"5"}}, []string{"gpt-4o-mini", "claude-sonnet-4", "gpt-4.1", "gpt-4.1-nano"}},
		{"use time between, bounds included", url.Values{"use_time_min": {"3"}, "use_time_max": {"12"}}, []string{"gpt-4o-mini", "gpt-4.1", "gpt-4.1-nano", "error-429", "error-502"}},
		{"TTFT at least", url.Values{"ttft_ms_min": {"1000"}}, []string{"claude-sonnet-4", "gpt-4.1"}},
		{"TTFT at most leaves out unknown TTFT", url.Values{"ttft_ms_max": {"1500"}}, []string{"gpt-4o", "claude-sonnet-4"}},
		{"one status code", url.Values{"status_codes": {"429"}}, []string{"error-429"}},
		{"several status codes", url.Values{"status_codes": {"429, 502"}}, []string{"error-429", "error-502"}},
		{"a status code nobody returned", url.Values{"status_codes": {"500"}}, []string{}},
		{"status code with another filter", url.Values{"status_codes": {"429,502"}, "model_name": {"%502%"}}, []string{"error-502"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for name, handlers := range map[string][2]gin.HandlerFunc{
				"all logs": {GetAllLogs, GetLogsStat},
				"own logs": {GetUserLogs, GetLogsSelfStat},
			} {
				logs := logFilterRequest(t, handlers[0], test.query)
				require.True(t, logs.Success, logs.MessageKey)
				models := make([]string, 0, len(logs.Data.Items))
				for _, item := range logs.Data.Items {
					models = append(models, item.ModelName)
				}
				assert.ElementsMatch(t, test.models, models, name)

				stat := logFilterRequest(t, handlers[1], test.query)
				require.True(t, stat.Success, stat.MessageKey)
				assert.Equal(t, "filter_unsupported", stat.Data.RateSource, name)
			}
		})
	}

	tooMany := make([]string, maxLogStatusCodeFilter+1)
	for i := range tooMany {
		tooMany[i] = strconv.Itoa(400 + i)
	}
	for _, test := range []struct {
		name       string
		query      url.Values
		messageKey string
	}{
		{"negative use time", url.Values{"use_time_min": {"-1"}}, "Use time must be whole seconds of 0 or more"},
		{"use time minimum above maximum", url.Values{"use_time_min": {"9"}, "use_time_max": {"3"}}, "The minimum cannot be greater than the maximum"},
		{"TTFT not a number", url.Values{"ttft_ms_max": {"fast"}}, "TTFT must be whole milliseconds of 0 or more"},
		{"TTFT minimum above maximum", url.Values{"ttft_ms_min": {"900"}, "ttft_ms_max": {"100"}}, "The minimum cannot be greater than the maximum"},
		{"status code out of range", url.Values{"status_codes": {"429,99"}}, "Status codes must be numbers from 100 to 599"},
		{"status code not a number", url.Values{"status_codes": {"timeout"}}, "Status codes must be numbers from 100 to 599"},
		{"too many status codes", url.Values{"status_codes": {strings.Join(tooMany, ",")}}, "Search at most {{max}} status codes at once"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, handler := range []gin.HandlerFunc{GetAllLogs, GetLogsStat, GetUserLogs, GetLogsSelfStat} {
				response := logFilterRequest(t, handler, test.query)
				assert.False(t, response.Success)
				assert.Equal(t, test.messageKey, response.MessageKey)
			}
		})
	}
}

func TestUsageLogsFilterByStreamMode(t *testing.T) {
	setupLogFilterTest(t)
	previousConsume := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	t.Cleanup(func() { common.LogConsumeEnabled = previousConsume })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("username", "alice")
	model.RecordConsumeLog(c, 1, model.RecordConsumeLogParams{ChannelId: 1, ModelName: "stream-consume", Quota: 800, IsStream: true, Group: "default", Other: model.NewLogOther()})
	model.RecordErrorLog(c, 1, 2, "stream-error", "", "upstream failed", 0, 3, true, "default", model.NewLogOther())
	model.RecordErrorLog(c, 1, 2, "plain-error", "", "upstream failed", 0, 3, false, "default", model.NewLogOther())
	// Logs that are not requests record no stream mode.
	require.NoError(t, model.LOG_DB.Create(&model.Log{UserId: 1, Username: "alice", Type: model.LogTypeTopup, Content: "top up", Quota: 5000, CreatedAt: 1003}).Error)

	for _, test := range []struct {
		name   string
		query  url.Values
		models []string
		quota  int
	}{
		{"stream requests", url.Values{"is_stream": {"true"}}, []string{"stream-consume", "stream-error"}, 800},
		{"non-stream requests leave out the top-up", url.Values{"is_stream": {"false"}}, []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4", "plain-error"}, 700},
		{"stream mode with another filter", url.Values{"is_stream": {"false"}, "model_name": {"%error%"}}, []string{"plain-error"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			for name, handlers := range map[string][2]gin.HandlerFunc{
				"all logs": {GetAllLogs, GetLogsStat},
				"own logs": {GetUserLogs, GetLogsSelfStat},
			} {
				logs := logFilterRequest(t, handlers[0], test.query)
				require.True(t, logs.Success, logs.MessageKey)
				models := make([]string, 0, len(logs.Data.Items))
				for _, item := range logs.Data.Items {
					models = append(models, item.ModelName)
				}
				assert.ElementsMatch(t, test.models, models, name)

				stat := logFilterRequest(t, handlers[1], test.query)
				require.True(t, stat.Success, stat.MessageKey)
				assert.Equal(t, test.quota, stat.Data.Quota, name)
				assert.Equal(t, "filter_unsupported", stat.Data.RateSource, name)
			}
		})
	}

	for _, handler := range []gin.HandlerFunc{GetAllLogs, GetLogsStat, GetUserLogs, GetLogsSelfStat} {
		response := logFilterRequest(t, handler, url.Values{"is_stream": {"sometimes"}})
		assert.False(t, response.Success)
		assert.Equal(t, "Stream mode must be true or false", response.MessageKey)
	}
}

// The response model filters read the model the upstream declared, as the
// relay records it on consume and error logs: by name or % pattern, including
// a declaration equal to the requested model, and the mismatches the usage
// logs flag with a warning.
func TestUsageLogsFilterByResponseModel(t *testing.T) {
	setupLogFilterTest(t)
	previousConsume := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	t.Cleanup(func() { common.LogConsumeEnabled = previousConsume })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("username", "alice")
	observed := func(requested, returned string) *model.LogOther {
		info := &relaycommon.RelayInfo{OriginModelName: requested}
		info.ObserveResponseModel(returned)
		other := model.NewLogOther()
		service.AppendResponseModelLogInfo(info, other)
		return other
	}
	longName := strings.Repeat("x", 300)
	for _, consume := range []struct{ requested, returned string }{
		{"gemini-3.1-pro-preview", "3.1-flash-lite-05-2026"},
		// A dated name extends the requested one: recorded, not a mismatch.
		{"gpt-4.1", "gpt-4.1-2025-04-14"},
		{"deepseek-v4", "deepseek-v4"},
		{"long-declaration", longName},
	} {
		model.RecordConsumeLog(c, 1, model.RecordConsumeLogParams{ChannelId: 1, ModelName: consume.requested, Quota: 300, Group: "default", Other: observed(consume.requested, consume.returned)})
	}
	model.RecordErrorLog(c, 1, 2, "claude-sonnet-4.5", "", "upstream failed", 0, 3, false, "default", observed("claude-sonnet-4.5", "glm-5.3"))

	var written []model.Log
	require.NoError(t, model.LOG_DB.Where("response_model <> ?", "").Order("id").Find(&written).Error)
	require.Len(t, written, 5)
	assert.Equal(t, "3.1-flash-lite-05-2026", written[0].ResponseModel)
	assert.True(t, written[0].ResponseModelMismatch)
	assert.False(t, written[1].ResponseModelMismatch, "a dated variant is not a mismatch")
	assert.Equal(t, "deepseek-v4", written[2].ResponseModel, "a declaration equal to the request is kept")
	assert.Equal(t, longName[:191], written[3].ResponseModel, "a declaration longer than the column is cut, and the log still written")
	assert.True(t, written[4].ResponseModelMismatch, "error logs record the declaration too")

	for _, test := range []struct {
		name   string
		query  url.Values
		models []string
		quota  int
	}{
		{"mismatches", url.Values{"response_model_mismatch": {"true"}}, []string{"gemini-3.1-pro-preview", "long-declaration", "claude-sonnet-4.5"}, 600},
		{"exact response model", url.Values{"response_model": {"3.1-flash-lite-05-2026"}}, []string{"gemini-3.1-pro-preview"}, 300},
		{"response model pattern", url.Values{"response_model": {"%flash-lite%"}}, []string{"gemini-3.1-pro-preview"}, 300},
		{"response model equal to the request", url.Values{"response_model": {"deepseek-v4"}}, []string{"deepseek-v4"}, 300},
		{"response model of an error", url.Values{"response_model": {"glm-5.3"}}, []string{"claude-sonnet-4.5"}, 0},
		{"pattern and mismatch together", url.Values{"response_model": {"%gpt-4.1%"}, "response_model_mismatch": {"true"}}, []string{}, 0},
		{"with the model name filter", url.Values{"model_name": {"%gemini%"}, "response_model_mismatch": {"true"}}, []string{"gemini-3.1-pro-preview"}, 300},
	} {
		t.Run(test.name, func(t *testing.T) {
			for name, handlers := range map[string][2]gin.HandlerFunc{
				"all logs": {GetAllLogs, GetLogsStat},
				"own logs": {GetUserLogs, GetLogsSelfStat},
			} {
				logs := logFilterRequest(t, handlers[0], test.query)
				require.True(t, logs.Success, logs.MessageKey)
				models := make([]string, 0, len(logs.Data.Items))
				for _, item := range logs.Data.Items {
					models = append(models, item.ModelName)
				}
				assert.ElementsMatch(t, test.models, models, name)

				stat := logFilterRequest(t, handlers[1], test.query)
				require.True(t, stat.Success, stat.MessageKey)
				assert.Equal(t, test.quota, stat.Data.Quota, name)
				assert.Equal(t, "filter_unsupported", stat.Data.RateSource, name)
			}
		})
	}

	unfiltered := logFilterRequest(t, GetAllLogs, url.Values{})
	notFiltered := logFilterRequest(t, GetAllLogs, url.Values{"response_model_mismatch": {"false"}})
	require.True(t, notFiltered.Success, notFiltered.MessageKey)
	assert.Equal(t, unfiltered.Data.Total, notFiltered.Data.Total, "false does not filter")

	for _, test := range []struct {
		name       string
		query      url.Values
		messageKey string
	}{
		{"mismatch not a boolean", url.Values{"response_model_mismatch": {"sometimes"}}, "Response model mismatch must be true or false"},
		{"pattern too short", url.Values{"response_model": {"%a%"}}, "Fuzzy search keywords must be at least 2 characters long"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, handler := range []gin.HandlerFunc{GetAllLogs, GetLogsStat, GetUserLogs, GetLogsSelfStat} {
				response := logFilterRequest(t, handler, test.query)
				assert.False(t, response.Success)
				assert.Equal(t, test.messageKey, response.MessageKey)
			}
		})
	}
}

// Logs written before the response model columns existed recorded the
// declaration only in other. The backfill fills the columns of a log id range
// from it, by the rule a log is written with, so the response model filters
// find those logs.
func TestUsageLogsFilterFindsBackfilledResponseModels(t *testing.T) {
	setupLogFilterTest(t)
	earlierOther := func(requested, returned string) string {
		info := &relaycommon.RelayInfo{OriginModelName: requested}
		info.ObserveResponseModel(returned)
		other := model.NewLogOther()
		service.AppendResponseModelLogInfo(info, other)
		return other.JSONString()
	}
	earlier := []model.Log{
		{Type: model.LogTypeConsume, ModelName: "gemini-3.1-pro-preview", Quota: 300, CreatedAt: 2000, Other: earlierOther("gemini-3.1-pro-preview", "3.1-flash-lite-05-2026")},
		{Type: model.LogTypeConsume, ModelName: "gpt-4.1", Quota: 300, CreatedAt: 2001, Other: earlierOther("gpt-4.1", "gpt-4.1-2025-04-14")},
		{Type: model.LogTypeError, ModelName: "claude-sonnet-4.5", CreatedAt: 2002, Other: earlierOther("claude-sonnet-4.5", "glm-5.3")},
		// Not a declaration: the key sits inside another value.
		{Type: model.LogTypeConsume, ModelName: "gpt-4o-mini", Quota: 300, CreatedAt: 2003, Other: `{"admin_info":{"response_model":"x"}}`},
	}
	require.NoError(t, model.LOG_DB.Create(&earlier).Error)
	// An upgrade leaves the new mismatch column empty on the earlier logs.
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("created_at >= ?", 2000).Update("response_model_mismatch", nil).Error)

	fromID, toID, err := model.LogIDRange()
	require.NoError(t, err)
	assert.Equal(t, earlier[len(earlier)-1].Id, toID)
	// The range excludes its first id and includes its last.
	filled, err := model.FillLogResponseModels(context.Background(), fromID-1, earlier[1].Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, filled)
	filled, err = model.FillLogResponseModels(context.Background(), earlier[1].Id, toID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, filled)
	filled, err = model.FillLogResponseModels(context.Background(), fromID-1, toID)
	require.NoError(t, err)
	assert.Zero(t, filled, "a filled log is not filled again")

	mismatches := logFilterRequest(t, GetAllLogs, url.Values{"response_model_mismatch": {"true"}})
	require.True(t, mismatches.Success, mismatches.MessageKey)
	models := make([]string, 0, len(mismatches.Data.Items))
	for _, item := range mismatches.Data.Items {
		models = append(models, item.ModelName)
	}
	assert.ElementsMatch(t, []string{"gemini-3.1-pro-preview", "claude-sonnet-4.5"}, models)
	for name, test := range map[string]struct {
		responseModel string
		total         int
	}{
		"a dated variant, not a mismatch":              {"gpt-4.1-2025-04-14", 1},
		"a key inside another value is no declaration": {"x", 0},
	} {
		logs := logFilterRequest(t, GetAllLogs, url.Values{"response_model": {test.responseModel}})
		require.True(t, logs.Success, logs.MessageKey)
		assert.Equal(t, test.total, logs.Data.Total, name)
	}

	// A finished run is recorded once, however often it is marked.
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.SystemTask{}))
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, "log_response_model_backfill")
		common.OptionMapRWMutex.Unlock()
	})
	require.False(t, model.LogResponseModelBackfillDone())
	require.NoError(t, model.MarkLogResponseModelBackfillDone())
	require.NoError(t, model.MarkLogResponseModelBackfillDone())
	assert.True(t, model.LogResponseModelBackfillDone())
	var marks []model.Option
	require.NoError(t, model.DB.Where(&model.Option{Key: "log_response_model_backfill"}).Find(&marks).Error)
	require.Len(t, marks, 1)
	assert.Equal(t, "done", marks[0].Value)
}

// A user's own quota statistics follow the account, not the username: the
// username filter matches a name holding % as a LIKE pattern, so an account
// named "al%" would otherwise read the consumption of every "al…" account.
func TestOwnQuotaStatIsScopedToTheCallerAccount(t *testing.T) {
	setupLogFilterTest(t)
	now := common.GetTimestamp()
	for _, log := range []model.Log{
		{UserId: 1, Username: "alice", Type: model.LogTypeConsume, ModelName: "gpt-4o", Quota: 5000, CreatedAt: now},
		{UserId: 2, Username: "al%", Type: model.LogTypeConsume, ModelName: "gpt-4o", Quota: 7, CreatedAt: now},
	} {
		require.NoError(t, model.LOG_DB.Create(&log).Error)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/log/self/stat?type=2", nil)
	c.Set("role", common.RoleCommonUser)
	c.Set("id", 2)
	c.Set("username", "al%")
	GetLogsSelfStat(c)

	var response logFilterResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	require.True(t, response.Success, response.MessageKey)
	assert.Equal(t, 7, response.Data.Quota)
}
