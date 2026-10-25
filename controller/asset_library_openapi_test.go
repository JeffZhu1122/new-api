package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeArk is an Ark asset OpenAPI that keeps resources in memory and records
// every call it receives.
type fakeArk struct {
	mu          sync.Mutex
	calls       []string
	next        int
	assetStatus string
	failAction  string
}

func (f *fakeArk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	action := r.URL.Query().Get("Action")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action+" "+string(body))
	f.next++
	metadata := fmt.Sprintf(`"ResponseMetadata":{"RequestId":"ark-%d","Action":%q,"Version":"2024-01-01","Service":"ark","Region":"cn-beijing"}`, f.next, action)
	if action == f.failAction {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"ResponseMetadata":{"RequestId":"ark-%d","Action":%q,"Error":{"Code":"InvalidParameter.URL","Message":"bad url"}}}`, f.next, action)
		return
	}
	var request map[string]any
	_ = common.Unmarshal(body, &request)
	switch action {
	case "CreateAssetGroup":
		_, _ = fmt.Fprintf(w, `{%s,"Result":{"Id":"group-%d"}}`, metadata, f.next)
	case "CreateAsset":
		_, _ = fmt.Fprintf(w, `{%s,"Result":{"Id":"asset-%d"}}`, metadata, f.next)
	case "GetAsset":
		errorField := ""
		if f.assetStatus == "Failed" {
			errorField = `,"Error":{"Code":"FaceMismatch","Message":"face does not match"}`
		}
		_, _ = fmt.Fprintf(w, `{%s,"Result":{"Id":%q,"Name":"renamed by ark","AssetType":"Image","GroupId":"g","Status":%q,"ProjectName":"video","URL":"https://tos.example/%s?sig=1","CreateTime":"2026-10-01T00:00:00Z","UpdateTime":"2026-10-01T00:01:00Z"%s}}`, metadata, request["Id"], f.assetStatus, request["Id"], errorField)
	default:
		_, _ = fmt.Fprintf(w, `{%s,"Result":{}}`, metadata)
	}
}

func (f *fakeArk) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type assetLibraryFixture struct {
	ark      *fakeArk
	accounts map[string]*model.AssetLibraryAccount
}

func setupAssetLibraryOpenAPI(t *testing.T) *assetLibraryFixture {
	t.Helper()
	setupAssetLibraryAccountTestDB(t)
	previous := config.GlobalConfig.ExportAllConfigs()
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("asset_library_setting"), map[string]string{
		"enabled": "true", "max_groups_per_user": "2", "max_assets_per_user": "3", "allowed_user_groups": "[]",
	}))
	t.Cleanup(func() {
		restore := map[string]string{}
		for _, key := range []string{"enabled", "max_groups_per_user", "max_assets_per_user", "allowed_user_groups"} {
			restore[key] = previous["asset_library_setting."+key]
		}
		_ = config.UpdateConfigFromMap(config.GlobalConfig.Get("asset_library_setting"), restore)
	})
	ark := &fakeArk{assetStatus: "Active"}
	server := httptest.NewServer(ark)
	t.Cleanup(server.Close)
	for _, user := range []*model.User{{Id: 1, Username: "alice", AffCode: "alice", Group: "default", Status: common.UserStatusEnabled}, {Id: 2, Username: "bob", AffCode: "bob", Group: "default", Status: common.UserStatusEnabled}} {
		require.NoError(t, model.DB.Create(user).Error)
	}
	fixture := &assetLibraryFixture{ark: ark, accounts: map[string]*model.AssetLibraryAccount{}}
	for _, account := range []*model.AssetLibraryAccount{
		{Name: "aigc-low", Priority: 1, AigcEnabled: true},
		{Name: "aigc-high", Priority: 5, AigcEnabled: true},
		{Name: "people", Priority: 3, LivenessFaceEnabled: true},
	} {
		account.AccessKeyId, account.SecretAccessKey, account.Region, account.ProjectName, account.Endpoint, account.Enabled = "ak", "sk", "cn-beijing", "video", server.URL, true
		require.NoError(t, model.CreateAssetLibraryAccount(account))
		fixture.accounts[account.Name] = account
	}
	return fixture
}

// callAction runs one action as the user and returns the HTTP status and the
// decoded body.
func callAction(t *testing.T, userId int, action, body string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/volc?Action="+action+"&Version=2024-01-01", strings.NewReader(body))
	ctx.Set("id", userId)
	ctx.Set(common.RequestIdKey, "gateway-req")
	AssetLibraryOpenAPI(ctx)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &decoded), recorder.Body.String())
	return recorder.Code, decoded
}

func arkErrorCode(body map[string]any) string {
	metadata, _ := body["ResponseMetadata"].(map[string]any)
	apiErr, _ := metadata["Error"].(map[string]any)
	code, _ := apiErr["Code"].(string)
	return code
}

func resultId(t *testing.T, body map[string]any) string {
	t.Helper()
	result, ok := body["Result"].(map[string]any)
	require.True(t, ok, body)
	return result["Id"].(string)
}

// Groups go to the highest-priority account that takes their type, Ark sees
// the account's project, and the response is Ark's own.
func TestAssetLibraryCreateGroupPicksTheAccountAndReturnsArksResponse(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)

	status, body := callAction(t, 1, "CreateAssetGroup", `{"Name":"portraits","Description":"d","ProjectName":"someone-else"}`)
	require.Equal(t, http.StatusOK, status, body)
	virtualId := resultId(t, body)
	assert.Equal(t, "ark-1", body["ResponseMetadata"].(map[string]any)["RequestId"])

	status, body = callAction(t, 1, "CreateAssetGroup", `{"Name":"me","GroupType":"LivenessFace"}`)
	require.Equal(t, http.StatusOK, status, body)
	personId := resultId(t, body)

	assert.Equal(t, []string{
		`CreateAssetGroup {"Description":"d","GroupType":"AIGC","Name":"portraits","ProjectName":"video"}`,
		`CreateAssetGroup {"GroupType":"LivenessFace","Name":"me","ProjectName":"video"}`,
	}, fixture.ark.recorded())
	virtual, err := model.GetUserAssetLibraryGroup(1, virtualId)
	require.NoError(t, err)
	assert.Equal(t, fixture.accounts["aigc-high"].Id, virtual.AccountId)
	person, err := model.GetUserAssetLibraryGroup(1, personId)
	require.NoError(t, err)
	assert.Equal(t, fixture.accounts["people"].Id, person.AccountId)

	status, body = callAction(t, 1, "CreateAssetGroup", `{"Name":"third"}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "QuotaExceeded", arkErrorCode(body))
	assert.Len(t, fixture.ark.recorded(), 2)
}

// Another user's group and asset read as missing and never reach Ark.
func TestAssetLibraryHidesOtherUsersResources(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)
	_, body := callAction(t, 1, "CreateAssetGroup", `{"Name":"alice"}`)
	groupId := resultId(t, body)
	_, body = callAction(t, 1, "CreateAsset", fmt.Sprintf(`{"GroupId":%q,"URL":"https://cdn.example/a.png","AssetType":"Image"}`, groupId))
	assetId := resultId(t, body)
	callsBefore := len(fixture.ark.recorded())

	for _, attempt := range []struct{ action, body string }{
		{"GetAssetGroup", fmt.Sprintf(`{"Id":%q}`, groupId)},
		{"UpdateAssetGroup", fmt.Sprintf(`{"Id":%q,"Name":"mine now"}`, groupId)},
		{"DeleteAssetGroup", fmt.Sprintf(`{"Id":%q}`, groupId)},
		{"CreateAsset", fmt.Sprintf(`{"GroupId":%q,"URL":"https://cdn.example/b.png","AssetType":"Image"}`, groupId)},
		{"GetAsset", fmt.Sprintf(`{"Id":%q}`, assetId)},
		{"UpdateAsset", fmt.Sprintf(`{"Id":%q,"Name":"x"}`, assetId)},
		{"DeleteAsset", fmt.Sprintf(`{"Id":%q}`, assetId)},
	} {
		status, response := callAction(t, 2, attempt.action, attempt.body)
		assert.Equal(t, http.StatusNotFound, status, attempt.action)
		assert.Equal(t, "ResourceNotFound", arkErrorCode(response), attempt.action)
	}
	assert.Len(t, fixture.ark.recorded(), callsBefore)

	_, groups := callAction(t, 2, "ListAssetGroups", `{"Filter":{"GroupType":"AIGC"}}`)
	assert.Empty(t, groups["Result"].(map[string]any)["Items"])
	_, assets := callAction(t, 2, "ListAssets", `{"Filter":{"GroupType":"AIGC","GroupIds":[`+fmt.Sprintf("%q", groupId)+`]}}`)
	assert.Empty(t, assets["Result"].(map[string]any)["Items"])
}

// An upload is recorded as a task with no quota; Ark finishing it, as seen
// by GetAsset, finishes the task.
func TestAssetLibraryUploadIsATaskThatFinishesWithTheAsset(t *testing.T) {
	cases := []struct {
		arkStatus  string
		taskStatus model.TaskStatus
		failReason string
	}{
		{"Active", model.TaskStatusSuccess, ""},
		{"Failed", model.TaskStatusFailure, "FaceMismatch: face does not match"},
	}
	for _, tc := range cases {
		t.Run(tc.arkStatus, func(t *testing.T) {
			fixture := setupAssetLibraryOpenAPI(t)
			_, body := callAction(t, 1, "CreateAssetGroup", `{"Name":"g"}`)
			groupId := resultId(t, body)
			status, body := callAction(t, 1, "CreateAsset", fmt.Sprintf(`{"GroupId":%q,"URL":"https://cdn.example/a.png","AssetType":"Image","Name":"front"}`, groupId))
			require.Equal(t, http.StatusOK, status, body)
			assetId := resultId(t, body)

			task, exists, err := model.GetByTaskId(1, assetId)
			require.NoError(t, err)
			require.True(t, exists)
			assert.Equal(t, constant.TaskPlatformAssetLibrary, task.Platform)
			assert.Equal(t, "CreateAsset", task.Action)
			assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)
			assert.Zero(t, task.Quota)
			assert.Zero(t, task.ChannelId)

			fixture.ark.assetStatus = tc.arkStatus
			status, body = callAction(t, 1, "GetAsset", fmt.Sprintf(`{"Id":%q}`, assetId))
			require.Equal(t, http.StatusOK, status, body)
			assert.Equal(t, tc.arkStatus, body["Result"].(map[string]any)["Status"])

			task, _, err = model.GetByTaskId(1, assetId)
			require.NoError(t, err)
			assert.Equal(t, tc.taskStatus, task.Status)
			assert.Equal(t, "100%", task.Progress)
			assert.Equal(t, tc.failReason, task.FailReason)
			stored, err := model.GetUserAssetLibraryAsset(1, assetId)
			require.NoError(t, err)
			assert.Equal(t, tc.arkStatus, stored.Status)
		})
	}
}

// Ark's own errors come back unchanged and leave nothing recorded.
func TestAssetLibraryPassesArkErrorsThrough(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)
	_, body := callAction(t, 1, "CreateAssetGroup", `{"Name":"g"}`)
	groupId := resultId(t, body)
	fixture.ark.failAction = "CreateAsset"

	status, body := callAction(t, 1, "CreateAsset", fmt.Sprintf(`{"GroupId":%q,"URL":"https://cdn.example/a.png","AssetType":"Image"}`, groupId))

	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "InvalidParameter.URL", arkErrorCode(body))
	count, err := model.CountAssetLibraryAssets("user_id", 1)
	require.NoError(t, err)
	assert.Zero(t, count)
}

// Uploads beyond the account's per-minute limit are refused before Ark.
func TestAssetLibraryLimitsUploadsPerAccount(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)
	require.NoError(t, model.DB.Model(&model.AssetLibraryAccount{}).Where("id = ?", fixture.accounts["aigc-high"].Id).Update("create_asset_qpm", 2).Error)
	_, body := callAction(t, 1, "CreateAssetGroup", `{"Name":"g"}`)
	groupId := resultId(t, body)
	upload := fmt.Sprintf(`{"GroupId":%q,"URL":"https://cdn.example/a.png","AssetType":"Image"}`, groupId)

	statuses := make([]int, 0, 3)
	for range 3 {
		status, _ := callAction(t, 1, "CreateAsset", upload)
		statuses = append(statuses, status)
	}

	assert.Equal(t, http.StatusTooManyRequests, statuses[len(statuses)-1])
	assert.LessOrEqual(t, len(fixture.ark.recorded())-1, 2)
}

// Lists answer in Ark's shape with both pagination styles, newest first.
func TestAssetLibraryListsPageLikeArk(t *testing.T) {
	setupAssetLibraryOpenAPI(t)
	ids := make([]string, 0, 2)
	for _, name := range []string{"first", "second"} {
		_, body := callAction(t, 1, "CreateAssetGroup", fmt.Sprintf(`{"Name":%q}`, name))
		ids = append(ids, resultId(t, body))
	}
	require.NoError(t, model.DB.Model(&model.AssetLibraryGroup{}).Where("remote_id = ?", ids[0]).Update("created_time", 1).Error)

	_, body := callAction(t, 1, "ListAssetGroups", `{"Filter":{"GroupType":"AIGC"},"MaxResults":1}`)
	result := body["Result"].(map[string]any)
	items := result["Items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, ids[1], items[0].(map[string]any)["Id"])
	assert.Equal(t, "video", items[0].(map[string]any)["ProjectName"])
	token, ok := result["NextToken"].(string)
	require.True(t, ok, result)

	_, body = callAction(t, 1, "ListAssetGroups", fmt.Sprintf(`{"Filter":{"GroupType":"AIGC"},"NextToken":%q}`, token))
	result = body["Result"].(map[string]any)
	assert.Equal(t, ids[0], result["Items"].([]any)[0].(map[string]any)["Id"])
	assert.NotContains(t, result, "NextToken")

	_, body = callAction(t, 1, "ListAssetGroups", `{"Filter":{"GroupType":"AIGC","Name":"sec"},"PageNumber":1,"PageSize":10}`)
	result = body["Result"].(map[string]any)
	assert.Equal(t, float64(1), result["TotalCount"])
	assert.Equal(t, float64(1), result["PageNumber"])

	status, body := callAction(t, 1, "ListAssetGroups", `{"Filter":{"GroupType":"AIGC"},"MaxResults":1,"PageSize":1}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "InvalidParameter", arkErrorCode(body))
}

func TestAssetLibraryRejectsUnknownActionsAndVersions(t *testing.T) {
	setupAssetLibraryOpenAPI(t)
	for _, target := range []string{"/volc?Action=CreateVisualValidateSession&Version=2024-01-01", "/volc?Action=ListAssets&Version=2023-01-01"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, target, strings.NewReader(`{}`))
		ctx.Set("id", 1)
		AssetLibraryOpenAPI(ctx)
		var body map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
		assert.Equal(t, http.StatusBadRequest, recorder.Code, target)
		assert.Equal(t, "InvalidActionOrVersion", arkErrorCode(body), target)
	}
}

// /volc takes gateway API keys, answers auth failures in Ark's format, and
// does not exist while the library is off.
func TestAssetLibraryTokenAuth(t *testing.T) {
	setupAssetLibraryOpenAPI(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}))
	require.NoError(t, model.DB.Create(&model.Token{Id: 7, UserId: 1, Key: "assetlibrarytestkey0000000000000000000000000000", Status: common.TokenStatusEnabled, Name: "k", ExpiredTime: -1, UnlimitedQuota: true}).Error)
	engine := gin.New()
	engine.POST("/volc", AssetLibraryTokenAuth(), AssetLibraryOpenAPI)
	call := func(authorization string) (int, string) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/volc?Action=ListAssetGroups&Version=2024-01-01", strings.NewReader(`{"Filter":{"GroupType":"AIGC"}}`))
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		engine.ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	status, body := call("Bearer sk-assetlibrarytestkey0000000000000000000000000000")
	assert.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, `"Items":[]`)

	status, body = call("")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, `"Code":"InvalidAuthorization"`)

	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("asset_library_setting"), map[string]string{"enabled": "false"}))
	status, body = call("Bearer sk-assetlibrarytestkey0000000000000000000000000000")
	assert.Equal(t, http.StatusNotFound, status, body)
	assert.NotContains(t, body, "ResponseMetadata")
}

// The task poller and the timeout sweep leave asset uploads alone.
func TestTaskPollingSkipsAssetLibraryUploads(t *testing.T) {
	setupAssetLibraryAccountTestDB(t)
	require.NoError(t, model.DB.Create(&model.Task{TaskID: "asset-1", Platform: constant.TaskPlatformAssetLibrary, Status: model.TaskStatusInProgress, Progress: "10%", SubmitTime: 1}).Error)
	require.NoError(t, model.DB.Create(&model.Task{TaskID: "video-1", Platform: "plugin", Status: model.TaskStatusInProgress, Progress: "10%", SubmitTime: 1}).Error)

	ids := func(tasks []*model.Task) []string {
		result := make([]string, 0, len(tasks))
		for _, task := range tasks {
			result = append(result, task.TaskID)
		}
		return result
	}
	assert.Equal(t, []string{"video-1"}, ids(model.GetAllUnFinishSyncTasks(10)))
	assert.Equal(t, []string{"video-1"}, ids(model.GetTimedOutUnfinishedTasks(100, 10)))
	require.NoError(t, model.DB.Where("task_id = ?", "video-1").Delete(&model.Task{}).Error)
	assert.False(t, model.HasUnfinishedSyncTasks())
}
