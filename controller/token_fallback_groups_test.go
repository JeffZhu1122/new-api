package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fallbackTestUsableGroups = `{"default":"Default","vip":"VIP","svip":"SVIP"}`
	fallbackTestGroupRatios  = `{"default":1,"vip":2,"svip":3}`
)

// configureTokenFallbackGroupsTest 配置多分组令牌测试所需的分组上限、可用分组与分组倍率，
// 有意不把 auto 加入可用分组：多分组令牌不依赖管理员开放 auto。
func configureTokenFallbackGroupsTest(t *testing.T, maxCount string, usableGroups string, groupRatios string) {
	t.Helper()
	originalMax := setting.GetMaxTokenAutoGroups()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateMaxTokenAutoGroups(maxCount))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(usableGroups))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(groupRatios))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(stringInt(originalMax)))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})
}

func baseGroupTokenRequest(name string, group string) map[string]any {
	return map[string]any{
		"name":              name,
		"expired_time":      -1,
		"remain_quota":      0,
		"unlimited_quota":   true,
		"group":             group,
		"cross_group_retry": true,
	}
}

func TestAddTokenPersistsPrimaryAndFallbackGroups(t *testing.T) {
	configureTokenFallbackGroupsTest(t, "3", fallbackTestUsableGroups, fallbackTestGroupRatios)
	user := setupTokenAutoGroupsControllerTest(t)
	request := baseGroupTokenRequest("multi-group", "vip")
	request["auto_groups"] = []string{"default", "svip"}

	ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
	AddToken(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var token model.Token
	require.NoError(t, model.DB.Where("name = ?", "multi-group").First(&token).Error)
	assert.Equal(t, "vip", token.Group)
	assert.JSONEq(t, `["default","svip"]`, token.AutoGroups)
	assert.True(t, token.CrossGroupRetry)
	assert.True(t, token.IsMultiGroup())
	assert.Equal(t, []string{"vip", "default", "svip"}, token.GetRoutingGroups())

	getCtx, getRecorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodGet, "/api/token/"+stringInt(token.Id), nil, user.Id)
	getCtx.Params = append(getCtx.Params, gin.Param{Key: "id", Value: stringInt(token.Id)})
	GetToken(getCtx)
	getResponse := decodeAPIResponse(t, getRecorder)
	require.True(t, getResponse.Success)
	var data struct {
		Group      string   `json:"group"`
		AutoGroups []string `json:"auto_groups"`
	}
	require.NoError(t, common.Unmarshal(getResponse.Data, &data))
	assert.Equal(t, "vip", data.Group)
	assert.Equal(t, []string{"default", "svip"}, data.AutoGroups)
}

func TestAddTokenWithoutFallbacksStaysSingleGroup(t *testing.T) {
	tests := []struct {
		name         string
		includeField bool
		value        any
	}{
		{name: "omitted"},
		{name: "null", includeField: true, value: nil},
		{name: "empty array", includeField: true, value: []string{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenFallbackGroupsTest(t, "5", fallbackTestUsableGroups, fallbackTestGroupRatios)
			user := setupTokenAutoGroupsControllerTest(t)
			request := baseGroupTokenRequest("single-"+test.name, "vip")
			if test.includeField {
				request["auto_groups"] = test.value
			}

			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
			AddToken(ctx)
			response := decodeAPIResponse(t, recorder)
			require.True(t, response.Success, response.Message)

			var token model.Token
			require.NoError(t, model.DB.Where("name = ?", request["name"]).First(&token).Error)
			assert.Equal(t, "vip", token.Group)
			assert.Empty(t, token.AutoGroups)
			assert.False(t, token.CrossGroupRetry)
			assert.False(t, token.IsMultiGroup())
		})
	}
}

func TestAddTokenRejectsInvalidFallbackGroups(t *testing.T) {
	tests := []struct {
		name      string
		maxCount  string
		group     string
		fallbacks []string
	}{
		{name: "primary counts toward the limit", maxCount: "2", group: "vip", fallbacks: []string{"default", "svip"}},
		{name: "duplicate fallback", maxCount: "5", group: "vip", fallbacks: []string{"default", "default"}},
		{name: "fallback equals primary", maxCount: "5", group: "vip", fallbacks: []string{"vip"}},
		{name: "auto pseudo group", maxCount: "5", group: "vip", fallbacks: []string{"auto"}},
		{name: "unavailable fallback", maxCount: "5", group: "vip", fallbacks: []string{"missing"}},
		{name: "unavailable primary", maxCount: "5", group: "missing", fallbacks: []string{"default"}},
		{name: "empty primary", maxCount: "5", group: "", fallbacks: []string{"default"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenFallbackGroupsTest(t, test.maxCount, fallbackTestUsableGroups, fallbackTestGroupRatios)
			user := setupTokenAutoGroupsControllerTest(t)
			request := baseGroupTokenRequest("invalid-"+test.name, test.group)
			request["auto_groups"] = test.fallbacks

			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
			AddToken(ctx)

			response := decodeAPIResponse(t, recorder)
			assert.False(t, response.Success)
			assert.NotEmpty(t, response.Message)
			var count int64
			require.NoError(t, model.DB.Model(&model.Token{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestAddTokenAllowsPrimaryPlusFallbacksUpToLimit(t *testing.T) {
	configureTokenFallbackGroupsTest(t, "3", fallbackTestUsableGroups, fallbackTestGroupRatios)
	user := setupTokenAutoGroupsControllerTest(t)
	request := baseGroupTokenRequest("at-limit", "default")
	request["auto_groups"] = []string{"vip", "svip"}

	ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
	AddToken(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
}

func TestUpdateTokenFallbackGroups(t *testing.T) {
	tests := []struct {
		name              string
		storedGroup       string
		storedFallbacks   []string
		requestGroup      string
		includeField      bool
		value             any
		expectedGroup     string
		expectedFallbacks string
		expectedRetry     bool
	}{
		{
			name: "omitted preserves fallbacks", storedGroup: "vip", storedFallbacks: []string{"default", "svip"},
			requestGroup: "vip", expectedGroup: "vip", expectedFallbacks: `["default","svip"]`, expectedRetry: true,
		},
		{
			name: "omitted strips the new primary from fallbacks", storedGroup: "vip", storedFallbacks: []string{"default", "svip"},
			requestGroup: "default", expectedGroup: "default", expectedFallbacks: `["svip"]`, expectedRetry: true,
		},
		{
			name: "explicit list replaces fallbacks", storedGroup: "vip", storedFallbacks: []string{"default", "svip"},
			requestGroup: "vip", includeField: true, value: []string{"svip"}, expectedGroup: "vip", expectedFallbacks: `["svip"]`, expectedRetry: true,
		},
		{
			name: "empty list clears fallbacks and disables retry", storedGroup: "vip", storedFallbacks: []string{"default"},
			requestGroup: "vip", includeField: true, value: []string{}, expectedGroup: "vip", expectedRetry: false,
		},
		{
			name: "null clears fallbacks and disables retry", storedGroup: "vip", storedFallbacks: []string{"default"},
			requestGroup: "vip", includeField: true, value: nil, expectedGroup: "vip", expectedRetry: false,
		},
		{
			name: "switching to auto without a list inherits the global order", storedGroup: "vip", storedFallbacks: []string{"default"},
			requestGroup: "auto", expectedGroup: "auto", expectedRetry: true,
		},
		{
			name: "switching from auto without a list drops the auto order", storedGroup: "auto", storedFallbacks: []string{"vip", "default"},
			requestGroup: "vip", expectedGroup: "vip", expectedRetry: false,
		},
		{
			name: "switching from auto with a list becomes multi-group", storedGroup: "auto", storedFallbacks: []string{"vip", "default"},
			requestGroup: "vip", includeField: true, value: []string{"default"}, expectedGroup: "vip", expectedFallbacks: `["default"]`, expectedRetry: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenFallbackGroupsTest(t, "5", fallbackTestUsableGroups, fallbackTestGroupRatios)
			user := setupTokenAutoGroupsControllerTest(t)
			token := seedToken(t, model.DB, user.Id, "update-multi", "update-multi-key")
			token.Group = test.storedGroup
			token.CrossGroupRetry = true
			require.NoError(t, token.SetAutoGroups(test.storedFallbacks))
			require.NoError(t, model.DB.Save(token).Error)

			request := baseGroupTokenRequest("updated-multi", test.requestGroup)
			request["id"] = token.Id
			request["status"] = common.TokenStatusEnabled
			if test.includeField {
				request["auto_groups"] = test.value
			}
			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPut, "/api/token/", request, user.Id)
			UpdateToken(ctx)
			response := decodeAPIResponse(t, recorder)
			require.True(t, response.Success, response.Message)

			var updated model.Token
			require.NoError(t, model.DB.First(&updated, token.Id).Error)
			assert.Equal(t, test.expectedGroup, updated.Group)
			if test.expectedFallbacks == "" {
				assert.Empty(t, updated.AutoGroups)
			} else {
				assert.JSONEq(t, test.expectedFallbacks, updated.AutoGroups)
			}
			assert.Equal(t, test.expectedRetry, updated.CrossGroupRetry)
		})
	}
}

func TestTokenAuthRoutesMultiGroupTokensWithoutAutoGroup(t *testing.T) {
	tests := []struct {
		name         string
		usableGroups string
		fallbacks    []string
		expectedCode int
	}{
		{name: "primary and fallback usable without auto enabled", usableGroups: fallbackTestUsableGroups, fallbacks: []string{"svip"}, expectedCode: http.StatusOK},
		{name: "revoked primary still routes through a usable fallback", usableGroups: `{"default":"Default","svip":"SVIP"}`, fallbacks: []string{"svip"}, expectedCode: http.StatusOK},
		{name: "every bound group revoked is forbidden", usableGroups: `{"default":"Default"}`, fallbacks: []string{"svip"}, expectedCode: http.StatusForbidden},
		{name: "single group token keeps the strict group check", usableGroups: `{"default":"Default","svip":"SVIP"}`, expectedCode: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenFallbackGroupsTest(t, "5", test.usableGroups, fallbackTestGroupRatios)
			// TokenAuth 走真实的 key 查询与用户缓存，需要完整初始化的数据库夹具。
			_, token := setupResponsesWSRequestTest(t)
			token.Group = "vip"
			token.CrossGroupRetry = true
			require.NoError(t, token.SetAutoGroups(test.fallbacks))
			require.NoError(t, model.DB.Save(token).Error)

			var usingGroup, tokenGroup string
			var routingGroups []string
			engine := gin.New()
			engine.GET("/v1/models", middleware.TokenAuth(), func(c *gin.Context) {
				usingGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
				tokenGroup = common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
				if value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups); ok {
					routingGroups, _ = value.([]string)
				}
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			request.Header.Set("Authorization", "Bearer sk-"+token.Key)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			require.Equal(t, test.expectedCode, recorder.Code, recorder.Body.String())
			if test.expectedCode != http.StatusOK {
				return
			}
			assert.Equal(t, "auto", usingGroup)
			assert.Equal(t, "auto", tokenGroup)
			assert.Equal(t, []string{"vip", "svip"}, routingGroups)
		})
	}
}
