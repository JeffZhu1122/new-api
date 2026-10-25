package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaykitdto "github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupAssetLibraryAccountTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AssetLibraryAccount{}, &model.AssetLibraryGroup{}, &model.AssetLibraryAsset{}, &model.Task{}, &model.User{}))
	model.DB, model.LOG_DB = db, db
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedis
		model.DB, model.LOG_DB = previousDB, previousLogDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

func callAssetLibraryAccountAPI(t *testing.T, handler gin.HandlerFunc, method, body string, params ...gin.Param) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, "/api/asset-library/accounts", strings.NewReader(body))
	ctx.Params = params
	handler(ctx)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return response
}

func TestAssetLibraryAccountAPINeverReturnsTheSecretAndKeepsItOnUpdate(t *testing.T) {
	setupAssetLibraryAccountTestDB(t)

	created := callAssetLibraryAccountAPI(t, CreateAssetLibraryAccount, http.MethodPost,
		`{"name":"main","access_key_id":"AKLTabcdef123456","secret_access_key":"secret-value","enabled":true,"aigc_enabled":true,"allowed_user_groups":["vip"],"max_assets":1000000,"create_asset_qpm":120}`)
	require.Equal(t, true, created["success"], created)
	view := created["data"].(map[string]any)
	assert.Equal(t, "AKLT****3456", view["access_key_id_masked"])
	assert.Equal(t, true, view["has_secret"])
	assert.Equal(t, "cn-beijing", view["region"])
	assert.Equal(t, "default", view["project_name"])
	assert.Equal(t, []any{"vip"}, view["allowed_user_groups"])
	assert.NotContains(t, fmt.Sprint(created), "secret-value")
	assert.NotContains(t, fmt.Sprint(created), "AKLTabcdef123456")

	id := int(view["id"].(float64))
	updated := callAssetLibraryAccountAPI(t, UpdateAssetLibraryAccount, http.MethodPut,
		fmt.Sprintf(`{"id":%d,"name":"renamed","access_key_id":"","secret_access_key":"","project_name":"video","enabled":false,"aigc_enabled":false,"liveness_face_enabled":true}`, id))
	require.Equal(t, true, updated["success"], updated)

	stored, err := model.GetAssetLibraryAccount(id)
	require.NoError(t, err)
	assert.Equal(t, "renamed", stored.Name)
	assert.Equal(t, "AKLTabcdef123456", stored.AccessKeyId)
	assert.Equal(t, "secret-value", stored.SecretAccessKey)
	assert.Equal(t, "video", stored.ProjectName)
	assert.False(t, stored.Enabled)
	assert.False(t, stored.AigcEnabled)
	assert.True(t, stored.LivenessFaceEnabled)
	assert.Empty(t, stored.AllowedUserGroupList())

	listed := callAssetLibraryAccountAPI(t, ListAssetLibraryAccounts, http.MethodGet, "")
	assert.NotContains(t, fmt.Sprint(listed), "secret-value")
}

func TestAssetLibraryAccountAPIRejectsInvalidAccounts(t *testing.T) {
	setupAssetLibraryAccountTestDB(t)
	cases := []struct {
		name    string
		body    string
		message string
	}{
		{"missing secret", `{"name":"a","access_key_id":"ak"}`, "Access Key ID and Secret Access Key are required"},
		{"blank name", `{"name":" ","access_key_id":"ak","secret_access_key":"sk"}`, "Account name must be 1 to 64 characters"},
		{"bad region", `{"name":"a","access_key_id":"ak","secret_access_key":"sk","region":"cn beijing"}`, "Invalid region"},
		{"bad endpoint", `{"name":"a","access_key_id":"ak","secret_access_key":"sk","endpoint":"ftp://example.com"}`, "Invalid endpoint URL"},
		{"negative quota", `{"name":"a","access_key_id":"ak","secret_access_key":"sk","max_groups":-1}`, "Invalid account limits"},
		{"empty user group", `{"name":"a","access_key_id":"ak","secret_access_key":"sk","allowed_user_groups":[""]}`, "Invalid allowed user groups"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := callAssetLibraryAccountAPI(t, CreateAssetLibraryAccount, http.MethodPost, tc.body)

			assert.Equal(t, false, response["success"])
			assert.Equal(t, tc.message, response["message"])
		})
	}
	accounts, err := model.ListAssetLibraryAccounts()
	require.NoError(t, err)
	assert.Empty(t, accounts)
}

// The test call lists one group of every enabled type in the account's
// project, signed with the stored key when the request leaves it empty.
func TestAssetLibraryAccountTestCallsArkWithTheStoredKey(t *testing.T) {
	setupAssetLibraryAccountTestDB(t)
	var mu sync.Mutex
	var bodies, credentials []string
	failWith := ""
	ark := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		bodies = append(bodies, r.URL.Query().Get("Action")+" "+string(bytes.TrimSpace(body)))
		credentials = append(credentials, strings.SplitN(strings.TrimPrefix(r.Header.Get("Authorization"), "HMAC-SHA256 Credential="), "/", 2)[0])
		if failWith != "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"req-9","Error":{"Code":"`+failWith+`","Message":"denied"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"req-1"},"Result":{"Items":[]}}`)
	}))
	defer ark.Close()
	account := &model.AssetLibraryAccount{Name: "main", AccessKeyId: "AKLTstored", SecretAccessKey: "stored-secret", Region: "cn-beijing", ProjectName: "video", Endpoint: ark.URL}
	require.NoError(t, model.CreateAssetLibraryAccount(account))

	request := fmt.Sprintf(`{"id":%d,"name":"main","project_name":"video","endpoint":%q,"aigc_enabled":true,"liveness_face_enabled":true}`, account.Id, ark.URL)
	succeeded := callAssetLibraryAccountAPI(t, TestAssetLibraryAccount, http.MethodPost, request)

	require.Equal(t, true, succeeded["success"], succeeded)
	assert.Equal(t, "Connection test succeeded", succeeded["message"])
	assert.Equal(t, []string{
		`ListAssetGroups {"Filter":{"GroupType":"AIGC"},"MaxResults":1,"ProjectName":"video"}`,
		`ListAssetGroups {"Filter":{"GroupType":"LivenessFace"},"MaxResults":1,"ProjectName":"video"}`,
	}, bodies)
	assert.Equal(t, []string{"AKLTstored", "AKLTstored"}, credentials)

	failWith = "AccessDenied"
	failed := callAssetLibraryAccountAPI(t, TestAssetLibraryAccount, http.MethodPost, request)

	assert.Equal(t, false, failed["success"])
	assert.Contains(t, failed["message"], "AccessDenied")
	assert.Contains(t, failed["message"], "req-9")
}

// sqlStatementCapture records every statement a session runs.
type sqlStatementCapture struct {
	gormlogger.Interface
	statements []string
}

func (c *sqlStatementCapture) LogMode(gormlogger.LogLevel) gormlogger.Interface { return c }

func (c *sqlStatementCapture) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	statement, _ := fc()
	c.statements = append(c.statements, statement)
}

// The asset library tables migrate on every supported database, a second
// migration leaves them alone, false and zero values survive an update, name
// search escapes LIKE wildcards, and an upload finishes its task.
func TestAssetLibraryAccountsAcrossDatabases(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip(dialect.env + " is not configured")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			tables := []any{&model.AssetLibraryAccount{}, &model.AssetLibraryGroup{}, &model.AssetLibraryAsset{}, &model.Task{}, &model.ChannelExtend{}}
			require.NoError(t, db.AutoMigrate(tables...))

			capture := &sqlStatementCapture{Interface: gormlogger.Discard}
			require.NoError(t, db.Session(&gorm.Session{Logger: capture}).AutoMigrate(tables...))
			for _, statement := range capture.statements {
				upper := strings.ToUpper(statement)
				assert.False(t, strings.HasPrefix(upper, "ALTER") || strings.HasPrefix(upper, "CREATE"), "second migration ran %s", statement)
			}

			low := &model.AssetLibraryAccount{Name: "low", AccessKeyId: "AKLTlow", SecretAccessKey: "sk-low", Region: "cn-beijing", ProjectName: "default", Enabled: true, AigcEnabled: true, Priority: 1, MaxAssets: 50, CreateAssetQPM: 3}
			high := &model.AssetLibraryAccount{Name: "high", AccessKeyId: "AKLThigh", SecretAccessKey: "sk-high", Region: "cn-beijing", ProjectName: "video", Enabled: true, LivenessFaceEnabled: true, Priority: 9, AllowedUserGroups: `["vip"]`}
			require.NoError(t, model.CreateAssetLibraryAccount(low))
			require.NoError(t, model.CreateAssetLibraryAccount(high))

			low.Enabled, low.AigcEnabled, low.MaxAssets, low.CreateAssetQPM, low.Priority = false, false, 0, 0, 0
			require.NoError(t, model.UpdateAssetLibraryAccount(low))
			stored, err := model.GetAssetLibraryAccount(low.Id)
			require.NoError(t, err)
			assert.False(t, stored.Enabled)
			assert.False(t, stored.AigcEnabled)
			assert.Zero(t, stored.MaxAssets)
			assert.Zero(t, stored.CreateAssetQPM)
			assert.Equal(t, "sk-low", stored.SecretAccessKey)

			accounts, err := model.ListAssetLibraryAccounts()
			require.NoError(t, err)
			require.Len(t, accounts, 2)
			assert.Equal(t, []string{"high", "low"}, []string{accounts[0].Name, accounts[1].Name})
			assert.Equal(t, []string{"vip"}, accounts[0].AllowedUserGroupList())

			require.NoError(t, model.DeleteAssetLibraryAccount(high.Id))
			accounts, err = model.ListAssetLibraryAccounts()
			require.NoError(t, err)
			assert.Len(t, accounts, 1)

			// Name search escapes LIKE wildcards with "!" on every database.
			for index, name := range []string{"100% real", "1000 real", "a_b", "axb"} {
				require.NoError(t, model.CreateAssetLibraryGroup(&model.AssetLibraryGroup{UserId: 1, AccountId: low.Id, RemoteId: fmt.Sprintf("group-%d", index), GroupType: "AIGC", Name: name}))
			}
			for _, search := range []struct {
				name string
				want []string
			}{{"0% r", []string{"100% real"}}, {"a_b", []string{"a_b"}}, {"!", nil}} {
				groups, total, err := model.ListAssetLibraryGroups(model.AssetLibraryListFilter{UserId: 1, GroupType: "AIGC", Name: search.name, SortBy: "CreateTime", Limit: 10})
				require.NoError(t, err)
				names := make([]string, 0, len(groups))
				for _, group := range groups {
					names = append(names, group.Name)
				}
				assert.Equal(t, int64(len(search.want)), total, search.name)
				assert.ElementsMatch(t, search.want, names, search.name)
			}
			require.Error(t, model.CreateAssetLibraryGroup(&model.AssetLibraryGroup{UserId: 2, AccountId: low.Id, RemoteId: "group-0", GroupType: "AIGC", Name: "dup"}))

			// An upload records the asset and its task together, and finishes the task with the asset.
			group, err := model.GetUserAssetLibraryGroup(1, "group-0")
			require.NoError(t, err)
			asset := &model.AssetLibraryAsset{UserId: 1, AccountId: low.Id, GroupId: group.Id, RemoteGroupId: group.RemoteId, RemoteId: "asset-1", AssetType: "Image", Status: model.AssetLibraryAssetProcessing}
			require.NoError(t, model.CreateAssetLibraryUpload(asset, "default"))
			asset.Status, asset.ErrorCode, asset.ErrorMessage = model.AssetLibraryAssetFailed, "DownloadFailed", "cannot download"
			require.NoError(t, model.SaveAssetLibraryAsset(asset, model.AssetLibraryAssetProcessing))
			task, exists, err := model.GetByTaskId(1, "asset-1")
			require.NoError(t, err)
			require.True(t, exists)
			assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
			assert.Equal(t, "DownloadFailed: cannot download", task.FailReason)
			assets, total, err := model.ListAssetLibraryAssets(model.AssetLibraryListFilter{UserId: 1, GroupType: "AIGC", Statuses: []string{model.AssetLibraryAssetFailed}, SortBy: "GroupId", Limit: 10})
			require.NoError(t, err)
			assert.Equal(t, int64(1), total)
			require.Len(t, assets, 1)
			require.NoError(t, model.DeleteAssetLibraryGroup(group))
			count, err := model.CountAssetLibraryAssets("user_id", 1)
			require.NoError(t, err)
			assert.Zero(t, count)

			// A channel's binding to an account round-trips and clears with 0.
			require.NoError(t, model.UpsertChannelExtend(model.DB, 77, relaykitdto.ChannelExtendSettings{AssetLibraryAccountId: low.Id}))
			extend, err := model.GetChannelExtend(77)
			require.NoError(t, err)
			assert.Equal(t, low.Id, extend.AssetLibraryAccountId)
			require.NoError(t, model.UpsertChannelExtend(model.DB, 77, relaykitdto.ChannelExtendSettings{RpmLimit: 5}))
			extend, err = model.GetChannelExtend(77)
			require.NoError(t, err)
			assert.Zero(t, extend.AssetLibraryAccountId)
		})
	}
}
