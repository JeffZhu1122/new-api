package controller

import (
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.Channel{}))
	dbType := common.DatabaseTypeSQLite
	switch dialect {
	case "mysql":
		dbType = common.DatabaseTypeMySQL
	case "postgres":
		dbType = common.DatabaseTypePostgreSQL
	}
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousCache := common.MemoryCacheEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(dbType, dbType)
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.MemoryCacheEnabled = previousCache
	})
	for _, log := range []model.Log{
		{UserId: 1, Username: "alice", Type: model.LogTypeConsume, ModelName: "gpt-4o", ChannelId: 1, Quota: 100, PromptTokens: 100, CompletionTokens: 10, CreatedAt: 1000},
		{UserId: 1, Username: "alice", Type: model.LogTypeConsume, ModelName: "gpt-4o-mini", ChannelId: 2, Quota: 200, PromptTokens: 2000, CompletionTokens: 50, CreatedAt: 1001},
		{UserId: 1, Username: "alice", Type: model.LogTypeConsume, ModelName: "claude-sonnet-4", ChannelId: 3, Quota: 400, PromptTokens: 50000, CompletionTokens: 800, CreatedAt: 1002},
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
