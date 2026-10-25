package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaykitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// fakeArk is an Ark asset OpenAPI that keeps resources in memory and records
// every call it receives.
type fakeArk struct {
	mu          sync.Mutex
	calls       []string
	next        int
	assetStatus string
	failAction  string
	// Group Ark created for each face verification token that passed.
	verified map[string]string
	// Name each asset was created with, as GetAsset reports it.
	names map[string]string
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
		if f.names == nil {
			f.names = map[string]string{}
		}
		name, _ := request["Name"].(string)
		f.names[fmt.Sprintf("asset-%d", f.next)] = name
		_, _ = fmt.Fprintf(w, `{%s,"Result":{"Id":"asset-%d"}}`, metadata, f.next)
	case "CreateVisualValidateSession":
		_, _ = fmt.Fprintf(w, `{%s,"Result":{"BytedToken":"tok-%d","H5Link":"https://h5.example/?bytedToken=tok-%d","CallbackURL":%q}}`, metadata, f.next, f.next, request["CallbackURL"])
	case "GetVisualValidateResult":
		token, _ := request["BytedToken"].(string)
		groupId := f.verified[token]
		if groupId == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"ResponseMetadata":{"RequestId":"ark-%d","Action":%q,"Error":{"Code":"InvalidParameter.BytedToken","Message":"not verified yet"}}}`, f.next, action)
			return
		}
		_, _ = fmt.Fprintf(w, `{%s,"Result":{"GroupId":%q}}`, metadata, groupId)
	case "GetAsset":
		errorField := ""
		if f.assetStatus == "Failed" {
			errorField = `,"Error":{"Code":"FaceMismatch","Message":"face does not match"}`
		}
		id, _ := request["Id"].(string)
		_, _ = fmt.Fprintf(w, `{%s,"Result":{"Id":%q,"Name":%q,"AssetType":"Image","GroupId":"g","Status":%q,"ProjectName":"video","URL":"https://tos.example/%s?sig=1","CreateTime":"2026-10-01T00:00:00Z","UpdateTime":"2026-10-01T00:01:00Z"%s}}`, metadata, id, f.names[id], f.assetStatus, id, errorField)
	default:
		_, _ = fmt.Fprintf(w, `{%s,"Result":{}}`, metadata)
	}
}

// pass makes Ark report the group for the verification token.
func (f *fakeArk) pass(token, groupId string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.verified == nil {
		f.verified = map[string]string{}
	}
	f.verified[token] = groupId
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
		"enabled": "true", "max_groups_per_user": "2", "max_assets_per_user": "3", "allowed_user_groups": "[]", "verify_sessions_per_hour": "5", "auto_upload_references": "false",
	}))
	t.Cleanup(func() {
		restore := map[string]string{}
		for _, key := range []string{"enabled", "max_groups_per_user", "max_assets_per_user", "allowed_user_groups", "verify_sessions_per_hour", "auto_upload_references"} {
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

// CreateAsset's Moderation goes to Ark as sent; a value that is not an object
// is refused before Ark, and null counts as not sent.
func TestAssetLibraryUploadPassesModerationThrough(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)
	_, body := callAction(t, 1, "CreateAssetGroup", `{"Name":"g"}`)
	groupId := resultId(t, body)
	upload := `{"GroupId":%q,"URL":"https://cdn.example/a.png","AssetType":"Image","Moderation":%s}`

	status, body := callAction(t, 1, "CreateAsset", fmt.Sprintf(upload, groupId, `"Skip"`))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "InvalidParameter", arkErrorCode(body))
	assert.Len(t, fixture.ark.recorded(), 1)

	for _, moderation := range []string{`{"Strategy":"Skip"}`, `null`} {
		status, body = callAction(t, 1, "CreateAsset", fmt.Sprintf(upload, groupId, moderation))
		require.Equal(t, http.StatusOK, status, body)
	}
	calls := fixture.ark.recorded()
	require.Len(t, calls, 3)
	assert.Equal(t, `CreateAsset {"AssetType":"Image","GroupId":"group-1","Moderation":{"Strategy":"Skip"},"ProjectName":"video","URL":"https://cdn.example/a.png"}`, calls[1])
	assert.Equal(t, `CreateAsset {"AssetType":"Image","GroupId":"group-1","ProjectName":"video","URL":"https://cdn.example/a.png"}`, calls[2])
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
	for _, target := range []string{"/volc?Action=ListProjects&Version=2024-01-01", "/volc?Action=ListAssets&Version=2023-01-01"} {
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

// asset:// references in relayed requests: the caller's own Active assets of
// one account pass and pin routing to that account; everything else is
// refused, including references hidden behind JSON escapes.
func TestAssetLibraryReferenceCheck(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 2).Update("role", common.RoleAdminUser).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 3, Username: "carol", AffCode: "carol", Group: "default", Status: common.UserStatusEnabled}).Error)
	_, body := callAction(t, 1, "CreateAssetGroup", `{"Name":"virtual"}`)
	virtualGroup := resultId(t, body)
	_, body = callAction(t, 1, "CreateAssetGroup", `{"Name":"person","GroupType":"LivenessFace"}`)
	personGroup := resultId(t, body)
	upload := func(groupId string) string {
		_, body := callAction(t, 1, "CreateAsset", fmt.Sprintf(`{"GroupId":%q,"URL":"https://cdn.example/a.png","AssetType":"Image"}`, groupId))
		return resultId(t, body)
	}
	active, processing, otherAccount := upload(virtualGroup), upload(virtualGroup), upload(personGroup)
	require.NoError(t, model.DB.Model(&model.AssetLibraryAsset{}).Where("remote_id IN ?", []string{active, otherAccount}).Update("status", model.AssetLibraryAssetActive).Error)
	request := func(urls ...string) []byte {
		content := make([]string, 0, len(urls))
		for _, url := range urls {
			content = append(content, fmt.Sprintf(`{"type":"image_url","image_url":{"url":%q}}`, url))
		}
		return []byte(`{"model":"doubao-seedance-2-0-260128","content":[` + strings.Join(content, ",") + `]}`)
	}
	virtualAccount := fixture.accounts["aigc-high"].Id

	cases := []struct {
		name    string
		userId  int
		body    []byte
		status  int
		message string
		account int
	}{
		{"no reference", 3, request("https://cdn.example/a.png"), 0, "", 0},
		{"own active asset", 1, request("asset://" + active), 0, "", virtualAccount},
		{"escaped slashes", 3, []byte(`{"content":[{"image_url":{"url":"asset:\/\/` + active + `"}}]}`), http.StatusForbidden, "asset_library.asset_not_owned", 0},
		{"unicode escape", 3, []byte(`{"content":[{"image_url":{"url":"\u0061sset://` + active + `"}}]}`), http.StatusForbidden, "asset_library.asset_not_owned", 0},
		{"upper-case scheme", 3, request("ASSET://" + active), http.StatusForbidden, "asset_library.asset_not_owned", 0},
		{"another user's asset", 3, request("asset://" + active), http.StatusForbidden, "asset_library.asset_not_owned", 0},
		{"administrator", 2, request("asset://" + active), 0, "", virtualAccount},
		{"unknown asset", 1, request("asset://asset-unknown"), http.StatusForbidden, "asset_library.asset_not_owned", 0},
		{"empty id", 1, request("asset://"), http.StatusBadRequest, "asset_library.reference_invalid", 0},
		{"still processing", 1, request("asset://" + processing), http.StatusBadRequest, "asset_library.asset_not_ready", 0},
		{"two accounts", 1, request("asset://"+active, "asset://"+otherAccount), http.StatusBadRequest, "asset_library.mixed_accounts", 0},
		{"multipart field", 3, []byte("--b\r\nContent-Disposition: form-data; name=\"image\"\r\n\r\nasset://" + active + "\r\n--b--\r\n"), http.StatusForbidden, "asset_library.asset_not_owned", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := service.CheckAssetLibraryReferences(tc.userId, tc.body)

			assert.Equal(t, tc.status, check.StatusCode)
			assert.Equal(t, tc.message, check.MessageID)
			assert.Equal(t, tc.account, check.AccountId)
		})
	}

	// While the library is off, references are refused unless blocking is turned off too.
	for _, blocking := range []struct {
		value  string
		status int
	}{{"true", http.StatusForbidden}, {"false", 0}} {
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("asset_library_setting"), map[string]string{"enabled": "false", "block_asset_refs_when_disabled": blocking.value}))
		check := service.CheckAssetLibraryReferences(1, request("asset://"+active))
		assert.Equal(t, blocking.status, check.StatusCode, "blocking %s", blocking.value)
		assert.Zero(t, check.AccountId, "blocking %s", blocking.value)
	}
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("asset_library_setting"), map[string]string{"block_asset_refs_when_disabled": "true"}))
}

// Only channels bound to the asset's account pass the routing filter.
func TestAssetLibraryAccountFilterKeepsBoundChannels(t *testing.T) {
	filter := []dto.ChannelFilter{{Kind: dto.FilterAssetLibraryAccount, AssetLibraryAccountId: 4}}
	bound := &model.Channel{Id: 1, ExtendConfig: &relaykitdto.ChannelExtendSettings{AssetLibraryAccountId: 4}}
	otherAccount := &model.Channel{Id: 2, ExtendConfig: &relaykitdto.ChannelExtendSettings{AssetLibraryAccountId: 5}}
	unbound := &model.Channel{Id: 3}

	for _, tc := range []struct {
		channel *model.Channel
		pass    bool
	}{{bound, true}, {otherAccount, false}, {unbound, false}} {
		pass, kind := model.ChannelSatisfiesFilters(tc.channel, "doubao-seedance-2-0-260128", filter)
		assert.Equal(t, tc.pass, pass, "channel %d", tc.channel.Id)
		if !tc.pass {
			assert.Equal(t, dto.FilterAssetLibraryAccount, kind)
		}
	}
	assert.True(t, model.FiltersReadExtendConfig(filter), "the filter needs extend settings backfilled")
}

// The Asset Library page: a user's own config, and the administrators' view
// of everyone's groups, which can delete any group on Ark.
func TestAssetLibraryConsoleEndpoints(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)
	_, body := callAction(t, 1, "CreateAssetGroup", `{"Name":"alice virtual"}`)
	aliceGroup := resultId(t, body)
	callAction(t, 1, "CreateAsset", fmt.Sprintf(`{"GroupId":%q,"URL":"https://cdn.example/a.png","AssetType":"Image"}`, aliceGroup))
	_, body = callAction(t, 2, "CreateAssetGroup", `{"Name":"bob person","GroupType":"LivenessFace"}`)
	bobGroup := resultId(t, body)
	run := func(handler gin.HandlerFunc, method, target string, params gin.Params, userId int) map[string]any {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(method, target, nil)
		ctx.Params = params
		ctx.Set("id", userId)
		handler(ctx)
		var decoded map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &decoded), recorder.Body.String())
		decoded["_status"] = float64(recorder.Code)
		return decoded
	}

	userConfig := run(GetAssetLibraryConfig, http.MethodGet, "/api/asset-library/config", nil, 1)["data"].(map[string]any)
	assert.Equal(t, true, userConfig["allowed"])
	assert.Equal(t, float64(1), userConfig["group_count"])
	assert.Equal(t, float64(1), userConfig["asset_count"])
	assert.Equal(t, float64(2), userConfig["max_groups_per_user"])
	assert.Equal(t, []any{"AIGC", "LivenessFace"}, userConfig["group_types"])

	listed := run(AdminListAssetLibraryGroups, http.MethodGet, "/api/asset-library/admin/groups?p=1&page_size=10", nil, 9)["data"].(map[string]any)
	assert.Equal(t, float64(2), listed["total"])
	byId := map[string]map[string]any{}
	for _, item := range listed["items"].([]any) {
		byId[item.(map[string]any)["Id"].(string)] = item.(map[string]any)
	}
	assert.Equal(t, "alice", byId[aliceGroup]["Username"])
	assert.Equal(t, "aigc-high", byId[aliceGroup]["AccountName"])
	assert.Equal(t, float64(1), byId[aliceGroup]["AssetCount"])
	assert.Equal(t, "people", byId[bobGroup]["AccountName"])

	filtered := run(AdminListAssetLibraryGroups, http.MethodGet, "/api/asset-library/admin/groups?p=1&page_size=10&user_id=2", nil, 9)["data"].(map[string]any)
	assert.Equal(t, float64(1), filtered["total"])

	assets := run(AdminListAssetLibraryGroupAssets, http.MethodGet, "/api/asset-library/admin/groups/"+aliceGroup+"/assets", gin.Params{{Key: "id", Value: aliceGroup}}, 9)["data"].([]any)
	require.Len(t, assets, 1)
	assert.Equal(t, aliceGroup, assets[0].(map[string]any)["GroupId"])

	callsBefore := len(fixture.ark.recorded())
	deleted := run(AdminDeleteAssetLibraryGroup, http.MethodDelete, "/api/asset-library/admin/groups/"+aliceGroup, gin.Params{{Key: "id", Value: aliceGroup}}, 9)
	assert.Equal(t, true, deleted["success"], deleted)
	calls := fixture.ark.recorded()[callsBefore:]
	require.Len(t, calls, 1)
	assert.Equal(t, fmt.Sprintf(`DeleteAssetGroup {"Id":%q,"ProjectName":"video"}`, aliceGroup), calls[0])
	count, err := model.CountAssetLibraryAssets("user_id", 1)
	require.NoError(t, err)
	assert.Zero(t, count)

	missing := run(AdminDeleteAssetLibraryAsset, http.MethodDelete, "/api/asset-library/admin/assets/asset-missing", gin.Params{{Key: "id", Value: "asset-missing"}}, 9)
	assert.Equal(t, false, missing["success"])
	assert.Equal(t, "Asset not found", missing["message"])

	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("asset_library_setting"), map[string]string{"enabled": "false"}))
	assert.Equal(t, float64(http.StatusNotFound), run(GetAssetLibraryConfig, http.MethodGet, "/api/asset-library/config", nil, 1)["_status"])
	assert.Equal(t, float64(http.StatusNotFound), run(AdminListAssetLibraryGroups, http.MethodGet, "/api/asset-library/admin/groups", nil, 9)["_status"])
}

func arkErrorMessage(body map[string]any) string {
	metadata, _ := body["ResponseMetadata"].(map[string]any)
	apiErr, _ := metadata["Error"].(map[string]any)
	message, _ := apiErr["Message"].(string)
	return message
}

// A face verification belongs to the user who started it: only they can
// collect the group, which Ark creates once the person passes and the gateway
// then names and records under them.
func TestAssetLibraryFaceVerificationRecordsTheGroupForItsUser(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)

	// An allowlisted account creates real-person groups directly.
	status, body := callAction(t, 1, "CreateVisualValidateSession", `{"CallbackURL":"https://gw.example/done"}`)
	assert.Equal(t, http.StatusForbidden, status)
	assert.Contains(t, arkErrorMessage(body), "CreateAssetGroup")

	people := fixture.accounts["people"]
	people.LivenessFaceVerify = true
	require.NoError(t, model.UpdateAssetLibraryAccount(people))
	userConfig, err := service.GetAssetLibraryUserConfig(1, "default")
	require.NoError(t, err)
	assert.True(t, userConfig.LivenessFaceVerify)
	assert.Contains(t, userConfig.GroupTypes, "LivenessFace")

	status, body = callAction(t, 1, "CreateAssetGroup", `{"Name":"me","GroupType":"LivenessFace"}`)
	assert.Equal(t, http.StatusForbidden, status)
	assert.Contains(t, arkErrorMessage(body), "CreateVisualValidateSession")
	status, body = callAction(t, 1, "CreateVisualValidateSession", `{"CallbackURL":"javascript:alert(1)"}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "InvalidParameter", arkErrorCode(body))
	assert.Empty(t, fixture.ark.recorded())

	status, body = callAction(t, 1, "CreateVisualValidateSession", `{"CallbackURL":"https://gw.example/done","Name":"Alice","Description":"the real one","ProjectName":"other"}`)
	require.Equal(t, http.StatusOK, status, body)
	session := body["Result"].(map[string]any)
	token := session["BytedToken"].(string)
	assert.NotEmpty(t, session["H5Link"])
	assert.Equal(t, []string{`CreateVisualValidateSession {"CallbackURL":"https://gw.example/done","ProjectName":"video"}`}, fixture.ark.recorded())
	verification, err := model.GetUserAssetLibraryVerification(1, token)
	require.NoError(t, err)
	task, exists, err := model.GetByTaskId(1, verification.TaskId)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, model.AssetLibraryVerifyAction, task.Action)
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)
	assert.Zero(t, task.Quota)
	assert.NotContains(t, string(task.Data), token)

	// Bob cannot collect Alice's group, and Ark is not asked.
	status, body = callAction(t, 2, "GetVisualValidateResult", fmt.Sprintf(`{"BytedToken":%q}`, token))
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "ResourceNotFound", arkErrorCode(body))
	assert.Len(t, fixture.ark.recorded(), 1)

	// Until the person passes, Ark's answer comes back as is.
	status, body = callAction(t, 1, "GetVisualValidateResult", fmt.Sprintf(`{"BytedToken":%q}`, token))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "InvalidParameter.BytedToken", arkErrorCode(body))

	fixture.ark.pass(token, "group-alice")
	before := len(fixture.ark.recorded())
	status, body = callAction(t, 1, "GetVisualValidateResult", fmt.Sprintf(`{"BytedToken":%q}`, token))
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "group-alice", body["Result"].(map[string]any)["GroupId"])
	assert.Equal(t, []string{
		fmt.Sprintf(`GetVisualValidateResult {"BytedToken":%q,"ProjectName":"video"}`, token),
		`UpdateAssetGroup {"Description":"the real one","Id":"group-alice","Name":"Alice","ProjectName":"video"}`,
		`GetAssetGroup {"Id":"group-alice","ProjectName":"video"}`,
	}, fixture.ark.recorded()[before:])
	group, err := model.GetUserAssetLibraryGroup(1, "group-alice")
	require.NoError(t, err)
	assert.Equal(t, "Alice", group.Name)
	assert.Equal(t, "LivenessFace", group.GroupType)
	assert.Equal(t, people.Id, group.AccountId)
	task, _, err = model.GetByTaskId(1, verification.TaskId)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Contains(t, string(task.Data), `"GroupId":"group-alice"`)

	// Asked again, the gateway answers from its record.
	before = len(fixture.ark.recorded())
	status, body = callAction(t, 1, "GetVisualValidateResult", fmt.Sprintf(`{"BytedToken":%q}`, token))
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "group-alice", body["Result"].(map[string]any)["GroupId"])
	assert.Len(t, fixture.ark.recorded(), before)

	// The group is Alice's like any other.
	status, body = callAction(t, 2, "GetAssetGroup", `{"Id":"group-alice"}`)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "ResourceNotFound", arkErrorCode(body))
}

// Pending verifications hold a group place and a user can start only so many
// per hour; a result page that did not pass ends the verification, and the
// background sync records groups nobody asked for and gives up after the
// token's lifetime.
func TestAssetLibraryFaceVerificationLimitsAndEndings(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)
	people := fixture.accounts["people"]
	people.LivenessFaceVerify = true
	require.NoError(t, model.UpdateAssetLibraryAccount(people))
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("asset_library_setting"), map[string]string{"verify_sessions_per_hour": "1"}))

	status, body := callAction(t, 1, "CreateVisualValidateSession", `{"CallbackURL":"https://gw.example/done"}`)
	require.Equal(t, http.StatusOK, status, body)
	aliceToken := body["Result"].(map[string]any)["BytedToken"].(string)
	calls := len(fixture.ark.recorded())
	status, body = callAction(t, 1, "CreateVisualValidateSession", `{"CallbackURL":"https://gw.example/done"}`)
	assert.Equal(t, http.StatusTooManyRequests, status)
	assert.Equal(t, "RequestLimitExceeded", arkErrorCode(body))
	assert.Len(t, fixture.ark.recorded(), calls)

	// Bob's pending verification takes the second of his two group places.
	status, body = callAction(t, 2, "CreateAssetGroup", `{"Name":"bob"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = callAction(t, 2, "CreateVisualValidateSession", `{"CallbackURL":"https://gw.example/done"}`)
	require.Equal(t, http.StatusOK, status, body)
	bobToken := body["Result"].(map[string]any)["BytedToken"].(string)
	status, body = callAction(t, 2, "CreateAssetGroup", `{"Name":"third"}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "QuotaExceeded", arkErrorCode(body))

	// The result page reports that Alice's person did not pass.
	service.HandleAssetLibraryVerificationCallback(context.Background(), aliceToken, "10001")
	status, body = callAction(t, 1, "GetVisualValidateResult", fmt.Sprintf(`{"BytedToken":%q}`, aliceToken))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "VerificationFailed", arkErrorCode(body))
	alice, err := model.GetUserAssetLibraryVerification(1, aliceToken)
	require.NoError(t, err)
	task, _, err := model.GetByTaskId(1, alice.TaskId)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	assert.Equal(t, "Face verification did not pass.", task.FailReason)
	assert.Contains(t, string(task.Data), `"ResultCode":"10001"`)

	calls = len(fixture.ark.recorded())
	service.HandleAssetLibraryVerificationCallback(context.Background(), "tok-unknown", "10000")
	assert.Len(t, fixture.ark.recorded(), calls)

	// The sync records the group of Bob's person although nobody asked.
	fixture.ark.pass(bobToken, "group-bob")
	require.NoError(t, model.DB.Model(&model.AssetLibraryVerification{}).Where("byted_token = ?", bobToken).Update("last_checked_time", 0).Error)
	service.SyncAssetLibraryVerifications(context.Background())
	_, err = model.GetUserAssetLibraryGroup(2, "group-bob")
	require.NoError(t, err)

	// A verification nobody finished gives up after the token's lifetime.
	stale := &model.AssetLibraryVerification{UserId: 2, AccountId: people.Id, BytedToken: "tok-stale"}
	require.NoError(t, model.CreateAssetLibraryVerification(stale, "default"))
	old := time.Now().Add(-40 * time.Minute).Unix()
	require.NoError(t, model.DB.Model(stale).Updates(map[string]any{"created_time": old, "last_checked_time": old}).Error)
	service.SyncAssetLibraryVerifications(context.Background())
	stale, err = model.GetAssetLibraryVerificationByToken("tok-stale")
	require.NoError(t, err)
	assert.Equal(t, model.AssetLibraryVerificationFailed, stale.Status)
	assert.Equal(t, "Face verification was not completed within 30 minutes.", stale.FailReason)
}

func countArkCalls(calls []string, action string) int {
	count := 0
	for _, call := range calls {
		if strings.HasPrefix(call, action+" ") {
			count++
		}
	}
	return count
}

// With auto upload on, URL references of a Seedance 2.x request become
// asset:// references to uploads in the caller's auto-upload group. The same
// URL is uploaded once, other roles and models are left alone, and an upload
// Ark refuses fails the request with Ark's reason.
func TestAssetLibraryAutoUploadsReferenceURLs(t *testing.T) {
	fixture := setupAssetLibraryOpenAPI(t)
	caller := service.AssetLibraryCaller{UserId: 1, UserGroup: "default"}
	modelName := "doubao-seedance-2-0-fast-260128"
	body := []byte(`{"model":"doubao-seedance-2-0-fast-260128","content":[{"type":"text","text":"x"},` +
		`{"type":"image_url","image_url":{"url":"https://cdn.example/a.png"},"role":"reference_image"},` +
		`{"type":"image_url","image_url":{"url":"https://cdn.example/first.png"},"role":"first_frame"},` +
		`{"type":"video_url","video_url":{"url":"https://cdn.example/v.mp4"},"role":"reference_video"},` +
		`{"type":"image_url","image_url":{"url":"asset://asset-own"},"role":"reference_image"}]}`)

	upload := service.AutoUploadAssetLibraryReferences(context.Background(), caller, modelName, body, "req")
	assert.Nil(t, upload.Body, "the switch is off by default")
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("asset_library_setting"), map[string]string{"auto_upload_references": "true"}))

	upload = service.AutoUploadAssetLibraryReferences(context.Background(), caller, modelName, body, "req")
	require.Zero(t, upload.StatusCode, upload)
	imageRef := gjson.GetBytes(upload.Body, "content.1.image_url.url").String()
	videoRef := gjson.GetBytes(upload.Body, "content.3.video_url.url").String()
	assert.True(t, strings.HasPrefix(imageRef, "asset://asset-"), imageRef)
	assert.True(t, strings.HasPrefix(videoRef, "asset://asset-"), videoRef)
	assert.Equal(t, "https://cdn.example/first.png", gjson.GetBytes(upload.Body, "content.2.image_url.url").String())
	assert.Equal(t, "asset://asset-own", gjson.GetBytes(upload.Body, "content.4.image_url.url").String())
	assert.Equal(t, "x", gjson.GetBytes(upload.Body, "content.0.text").String())
	group, err := model.GetUserAssetLibraryGroupByName(1, "AIGC", service.AssetLibraryAutoUploadGroupName)
	require.NoError(t, err)
	assert.Equal(t, fixture.accounts["aigc-high"].Id, group.AccountId)
	calls := fixture.ark.recorded()
	assert.Equal(t, 1, countArkCalls(calls, "CreateAssetGroup"))
	require.Equal(t, 2, countArkCalls(calls, "CreateAsset"))
	assert.Contains(t, calls[1], `"AssetType":"Image"`)
	assert.Contains(t, calls[1], `"URL":"https://cdn.example/a.png"`)
	assert.Contains(t, calls[2], `"AssetType":"Video"`)

	// The same URLs are reused, not uploaded again.
	again := service.AutoUploadAssetLibraryReferences(context.Background(), caller, modelName, body, "req")
	require.Zero(t, again.StatusCode, again)
	assert.Equal(t, imageRef, gjson.GetBytes(again.Body, "content.1.image_url.url").String())
	assert.Equal(t, videoRef, gjson.GetBytes(again.Body, "content.3.video_url.url").String())
	assert.Equal(t, 2, countArkCalls(fixture.ark.recorded(), "CreateAsset"))

	// Seedance 1.x takes no asset:// references.
	before := len(fixture.ark.recorded())
	other := service.AutoUploadAssetLibraryReferences(context.Background(), caller, "doubao-seedance-1-0-pro-250528", body, "req")
	assert.Nil(t, other.Body)
	assert.Zero(t, other.StatusCode)
	assert.Len(t, fixture.ark.recorded(), before)

	// More references than allowed are refused before Ark.
	refs := make([]string, 10)
	for i := range refs {
		refs[i] = fmt.Sprintf(`{"type":"image_url","image_url":{"url":"https://cdn.example/%d.png"},"role":"reference_image"}`, i)
	}
	many := service.AutoUploadAssetLibraryReferences(context.Background(), caller, modelName, []byte(`{"content":[`+strings.Join(refs, ",")+`]}`), "req")
	assert.Equal(t, http.StatusBadRequest, many.StatusCode)
	assert.Equal(t, i18n.MsgAssetLibraryAutoUploadTooMany, many.MessageID)
	assert.Len(t, fixture.ark.recorded(), before)

	// An upload Ark refuses fails the request with Ark's reason.
	fixture.ark.assetStatus = "Failed"
	refused := service.AutoUploadAssetLibraryReferences(context.Background(), caller, modelName,
		[]byte(`{"content":[{"type":"image_url","image_url":{"url":"https://cdn.example/face.png"},"role":"reference_image"}]}`), "req")
	assert.Equal(t, http.StatusBadRequest, refused.StatusCode)
	assert.Equal(t, i18n.MsgAssetLibraryAutoUploadFailed, refused.MessageID)
	assert.Equal(t, map[string]any{"Index": 1, "Reason": "FaceMismatch: face does not match"}, refused.Params)
}
