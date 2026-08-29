package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rpmTpmTestUserId = 42

func useModelRpmTpmRules(t *testing.T, jsonStr string) {
	t.Helper()
	previousEnabled := setting.ModelRateLimitEnabled
	setting.ModelRateLimitEnabled = true
	require.NoError(t, setting.UpdateModelRateLimitRulesByJSONString(jsonStr))
	t.Cleanup(func() {
		setting.ModelRateLimitEnabled = previousEnabled
		require.NoError(t, setting.UpdateModelRateLimitRulesByJSONString(""))
	})
}

// setupUserExtendTestDB provides an in-memory SQLite DB so the middleware can
// resolve per-user overrides from the user_extend table.
func setupUserExtendTestDB(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalIsMasterNode := common.IsMasterNode
	originalRedisEnabled := common.RedisEnabled
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.RedisEnabled = false
	common.SQLitePath = fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB
	require.NoError(t, model.DB.AutoMigrate(&model.UserExtend{}))

	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.IsMasterNode = originalIsMasterNode
		common.RedisEnabled = originalRedisEnabled
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
}

func newModelRpmTpmTestRouter(group string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", rpmTpmTestUserId)
		common.SetContextKey(c, constant.ContextKeyUserGroup, group)
	})
	router.Use(ModelRpmTpmRateLimit())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return router
}

func performChatRequest(router http.Handler, modelName string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	body := strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, modelName))
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "10.0.0.1:1234"
	router.ServeHTTP(recorder, request)
	return recorder
}

func seedNoUserOverride(t *testing.T, redisServer *miniredis.Miniredis) {
	t.Helper()
	require.NoError(t, redisServer.Set(fmt.Sprintf("user_extend_rl:%d", rpmTpmTestUserId), "null"))
}

func TestModelRpmLimitRedis(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	seedNoUserOverride(t, redisServer)
	useModelRpmTpmRules(t, `{"groups":{"default":{"default":{"rpm":2}}}}`)

	router := newModelRpmTpmTestRouter("default")

	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)
	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)

	third := performChatRequest(router, "gpt-4o")
	require.Equal(t, http.StatusTooManyRequests, third.Code)
	assert.NotEmpty(t, third.Header().Get("Retry-After"))

	redisServer.FastForward(61 * time.Second)
	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)
}

func TestModelRpmLimitIsolatedPerModel(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	seedNoUserOverride(t, redisServer)
	useModelRpmTpmRules(t, `{"groups":{"default":{"models":{"gpt-4o":{"rpm":1}}}}}`)

	router := newModelRpmTpmTestRouter("default")

	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)
	assert.Equal(t, http.StatusTooManyRequests, performChatRequest(router, "gpt-4o").Code)
	// 未配置规则的模型不受影响
	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-3.5-turbo").Code)
}

func TestModelTpmLimitRedis(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	seedNoUserOverride(t, redisServer)
	useModelRpmTpmRules(t, `{"groups":{"default":{"default":{"tpm":100}}}}`)

	router := newModelRpmTpmTestRouter("default")

	// 当前(及下一)分钟桶已达限额 → 429;跨过分钟边界也不 flake
	minute := common.UnixMinute()
	require.NoError(t, redisServer.Set(common.ModelTpmRedisKey(rpmTpmTestUserId, "default", "gpt-4o", minute), "150"))
	require.NoError(t, redisServer.Set(common.ModelTpmRedisKey(rpmTpmTestUserId, "default", "gpt-4o", minute+1), "150"))
	blocked := performChatRequest(router, "gpt-4o")
	require.Equal(t, http.StatusTooManyRequests, blocked.Code)
	assert.NotEmpty(t, blocked.Header().Get("Retry-After"))

	// 其他模型共享分组默认规则但各自计数
	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-3.5-turbo").Code)
}

func TestModelTpmRecordThenCheckConsistency(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	seedNoUserOverride(t, redisServer)
	useModelRpmTpmRules(t, `{"groups":{"default":{"default":{"tpm":100}}}}`)

	router := newModelRpmTpmTestRouter("default")
	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)

	// 结算侧记账后,中间件必须能在同一个 key 上看到用量(key 构造一致性回归)
	relayInfo := &relaycommon.RelayInfo{
		UserId:          rpmTpmTestUserId,
		TokenGroup:      "default",
		OriginModelName: "gpt-4o",
	}
	service.RecordModelTokensUsed(context.Background(), relayInfo, 150)

	code := performChatRequest(router, "gpt-4o").Code
	if code != http.StatusTooManyRequests {
		// 极小概率记账与检查恰好跨过分钟边界,重记一次后必须命中
		service.RecordModelTokensUsed(context.Background(), relayInfo, 150)
		code = performChatRequest(router, "gpt-4o").Code
	}
	require.Equal(t, http.StatusTooManyRequests, code)
}

func TestModelRpmLimitUserOverrideWins(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	useModelRpmTpmRules(t, `{"groups":{"default":{"default":{"rpm":100}}}}`)
	// 用户覆盖 rpm=1,应压过分组的 100
	require.NoError(t, redisServer.Set(fmt.Sprintf("user_extend_rl:%d", rpmTpmTestUserId), `{"default":{"rpm":1}}`))

	router := newModelRpmTpmTestRouter("default")

	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)
	assert.Equal(t, http.StatusTooManyRequests, performChatRequest(router, "gpt-4o").Code)
}

func TestModelRpmTpmLimitFailOpenOnRedisError(t *testing.T) {
	setupUserExtendTestDB(t)
	_, redisClient := useRateLimitMiniRedis(t)
	useModelRpmTpmRules(t, `{"groups":{"default":{"default":{"rpm":1,"tpm":1}}}}`)

	require.NoError(t, redisClient.Close())

	router := newModelRpmTpmTestRouter("default")
	// Redis 故障时 fail-open:限流失效但转发不受影响
	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)
	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)
}

func TestModelRpmTpmLimitMemoryFallback(t *testing.T) {
	setupUserExtendTestDB(t)
	useModelRpmTpmRules(t, `{"groups":{"default":{"default":{"rpm":2,"tpm":100}}}}`)

	router := newModelRpmTpmTestRouter("default")

	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)
	assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)
	assert.Equal(t, http.StatusTooManyRequests, performChatRequest(router, "gpt-4o").Code)

	// TPM 内存路径:记账后被拦截(RPM 用不同模型避开)
	relayInfo := &relaycommon.RelayInfo{
		UserId:          rpmTpmTestUserId,
		TokenGroup:      "default",
		OriginModelName: "gpt-4o-mini",
	}
	service.RecordModelTokensUsed(context.Background(), relayInfo, 150)
	code := performChatRequest(router, "gpt-4o-mini").Code
	if code != http.StatusTooManyRequests {
		service.RecordModelTokensUsed(context.Background(), relayInfo, 150)
		code = performChatRequest(router, "gpt-4o-mini").Code
	}
	require.Equal(t, http.StatusTooManyRequests, code)
}

func TestModelRpmLimitUnparsableModelStillCounted(t *testing.T) {
	// 解析失败路径会经 i18n 组装错误信息,需要已初始化的 bundle(幂等)
	require.NoError(t, i18n.Init())
	redisServer, _ := useRateLimitMiniRedis(t)
	seedNoUserOverride(t, redisServer)
	useModelRpmTpmRules(t, `{"groups":{"default":{"default":{"rpm":1}}}}`)

	router := newModelRpmTpmTestRouter("default")

	send := func() int {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{invalid"))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "10.0.0.1:1234"
		router.ServeHTTP(recorder, request)
		return recorder.Code
	}
	// 模型解析失败按 model 为空计入分组默认规则,不能成为逃逸面
	assert.Equal(t, http.StatusOK, send())
	assert.Equal(t, http.StatusTooManyRequests, send())
}

func TestModelRpmTpmLimitDisabledPassthrough(t *testing.T) {
	previousEnabled := setting.ModelRateLimitEnabled
	setting.ModelRateLimitEnabled = false
	t.Cleanup(func() { setting.ModelRateLimitEnabled = previousEnabled })

	router := newModelRpmTpmTestRouter("default")
	for i := 0; i < 5; i++ {
		assert.Equal(t, http.StatusOK, performChatRequest(router, "gpt-4o").Code)
	}
}
