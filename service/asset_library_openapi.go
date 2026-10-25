package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/volcark"
	"github.com/QuantumNous/new-api/setting/asset_library_setting"

	"gorm.io/gorm"
)

// The asset library speaks Ark's own asset OpenAPI (same Action, Version,
// request and response bodies) so users can switch an existing integration
// over by changing only the endpoint. Each user sees only their own groups
// and assets: reads and writes of one resource go to the account that holds
// it after an ownership check, and lists are answered from the gateway's
// records because a user's groups can span several accounts.

// AssetLibraryCaller is the gateway user an asset library request acts for.
type AssetLibraryCaller struct {
	UserId    int
	UserGroup string
}

// AssetLibraryResponse is the HTTP answer to one action.
type AssetLibraryResponse struct {
	StatusCode int
	Body       []byte
}

// Error codes of checks the gateway makes before Ark is called. Ark's own
// errors are returned as Ark sent them.
const (
	assetLibraryCodeInvalidAction   = "InvalidActionOrVersion"
	assetLibraryCodeInvalidParam    = "InvalidParameter"
	assetLibraryCodeNotFound        = "ResourceNotFound"
	assetLibraryCodeAccessDenied    = "AccessDenied"
	assetLibraryCodeQuotaExceeded   = "QuotaExceeded"
	assetLibraryCodeRequestLimit    = "RequestLimitExceeded"
	assetLibraryCodeInternalError   = "InternalError"
	assetLibraryMaxListResults      = 100
	assetLibraryDefaultListResults  = 10
	assetLibraryURLRefreshInterval  = 11 * time.Hour
	assetLibraryRefreshConcurrency  = 8
	assetLibraryRefreshTimeout      = 8 * time.Second
	assetLibraryNameMaxLength       = 64
	assetLibraryDescriptionMaxRunes = 300
)

type assetLibraryErrorBody struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type assetLibraryMetadata struct {
	RequestId string                 `json:"RequestId"`
	Action    string                 `json:"Action"`
	Version   string                 `json:"Version"`
	Service   string                 `json:"Service"`
	Region    string                 `json:"Region"`
	Error     *assetLibraryErrorBody `json:"Error,omitempty"`
}

type assetLibraryEnvelope struct {
	ResponseMetadata assetLibraryMetadata `json:"ResponseMetadata"`
	Result           any                  `json:"Result,omitempty"`
}

type assetLibraryRequest struct {
	ctx       context.Context
	caller    AssetLibraryCaller
	action    string
	requestId string
	body      []byte
}

var assetLibraryActions = map[string]func(*assetLibraryRequest) AssetLibraryResponse{
	"CreateAssetGroup": (*assetLibraryRequest).createGroup,
	"GetAssetGroup":    (*assetLibraryRequest).getGroup,
	"UpdateAssetGroup": (*assetLibraryRequest).updateGroup,
	"DeleteAssetGroup": (*assetLibraryRequest).deleteGroup,
	"ListAssetGroups":  (*assetLibraryRequest).listGroups,
	"CreateAsset":      (*assetLibraryRequest).createAsset,
	"GetAsset":         (*assetLibraryRequest).getAsset,
	"UpdateAsset":      (*assetLibraryRequest).updateAsset,
	"DeleteAsset":      (*assetLibraryRequest).deleteAsset,
	"ListAssets":       (*assetLibraryRequest).listAssets,
}

// HandleAssetLibraryAction runs one Ark asset action for the caller.
func HandleAssetLibraryAction(ctx context.Context, caller AssetLibraryCaller, action, version string, body []byte, requestId string) AssetLibraryResponse {
	request := &assetLibraryRequest{ctx: ctx, caller: caller, action: action, requestId: requestId, body: body}
	handler, ok := assetLibraryActions[action]
	if !ok || version != volcark.APIVersion {
		return request.fail(http.StatusBadRequest, assetLibraryCodeInvalidAction, fmt.Sprintf("The action %q of version %q is not supported.", action, version))
	}
	if !asset_library_setting.GetSetting().UserGroupAllowed(caller.UserGroup) {
		return request.fail(http.StatusForbidden, assetLibraryCodeAccessDenied, "Your user group may not use the asset library.")
	}
	return handler(request)
}

// AssetLibraryErrorResponse is an error in Ark's format for a request that
// fails before reaching an action, such as a missing API key.
func AssetLibraryErrorResponse(action, requestId string, status int, code, message string) AssetLibraryResponse {
	return (&assetLibraryRequest{action: action, requestId: requestId}).fail(status, code, message)
}

func (r *assetLibraryRequest) respond(status int, region string, result any, apiErr *assetLibraryErrorBody) AssetLibraryResponse {
	if region == "" {
		region = volcark.DefaultRegion
	}
	body, err := common.Marshal(assetLibraryEnvelope{
		ResponseMetadata: assetLibraryMetadata{
			RequestId: r.requestId,
			Action:    r.action,
			Version:   volcark.APIVersion,
			Service:   volcark.Service,
			Region:    region,
			Error:     apiErr,
		},
		Result: result,
	})
	if err != nil {
		return AssetLibraryResponse{StatusCode: http.StatusInternalServerError, Body: []byte(`{}`)}
	}
	return AssetLibraryResponse{StatusCode: status, Body: body}
}

func (r *assetLibraryRequest) fail(status int, code, message string) AssetLibraryResponse {
	return r.respond(status, "", nil, &assetLibraryErrorBody{Code: code, Message: message})
}

func (r *assetLibraryRequest) internalError(err error) AssetLibraryResponse {
	common.SysError(common.LogText("asset library %s failed: %s", r.action, err.Error()))
	return r.fail(http.StatusInternalServerError, assetLibraryCodeInternalError, "The request failed because of an internal error.")
}

// decode reads the JSON body (empty means {}) into out.
func (r *assetLibraryRequest) decode(out any) (AssetLibraryResponse, bool) {
	body := r.body
	if len(strings.TrimSpace(string(body))) == 0 {
		body = []byte("{}")
	}
	if err := common.Unmarshal(body, out); err != nil {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "The request body is not a valid JSON object for this action."), false
	}
	return AssetLibraryResponse{}, true
}

// forward sends the action to the account and returns Ark's answer as is.
// Ark errors come back in the response; only transport failures fail here.
func (r *assetLibraryRequest) forward(account *model.AssetLibraryAccount, payload map[string]any) (*volcark.Response, AssetLibraryResponse, bool) {
	payload["ProjectName"] = account.ProjectName
	encoded, err := common.Marshal(payload)
	if err != nil {
		return nil, r.internalError(err), false
	}
	response, err := AssetLibraryClient(account).CallRaw(r.ctx, r.action, encoded)
	if err != nil {
		common.SysError(common.LogText("asset library %s via account %d failed: %s", r.action, account.Id, err.Error()))
		return nil, r.fail(http.StatusBadGateway, assetLibraryCodeInternalError, "The asset library service could not be reached."), false
	}
	return response, AssetLibraryResponse{}, true
}

func passThrough(response *volcark.Response) AssetLibraryResponse {
	return AssetLibraryResponse{StatusCode: response.StatusCode, Body: response.Body}
}

func (r *assetLibraryRequest) account(id int) (*model.AssetLibraryAccount, AssetLibraryResponse, bool) {
	account, err := model.GetAssetLibraryAccount(id)
	if err != nil {
		return nil, r.internalError(fmt.Errorf("account %d: %w", id, err)), false
	}
	return account, AssetLibraryResponse{}, true
}

func (r *assetLibraryRequest) ownedGroup(id string) (*model.AssetLibraryGroup, AssetLibraryResponse, bool) {
	if strings.TrimSpace(id) == "" {
		return nil, r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "The group Id is required."), false
	}
	group, err := model.GetUserAssetLibraryGroup(r.caller.UserId, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, r.fail(http.StatusNotFound, assetLibraryCodeNotFound, fmt.Sprintf("The asset group %s does not exist.", id)), false
	}
	if err != nil {
		return nil, r.internalError(err), false
	}
	return group, AssetLibraryResponse{}, true
}

func (r *assetLibraryRequest) ownedAsset(id string) (*model.AssetLibraryAsset, AssetLibraryResponse, bool) {
	if strings.TrimSpace(id) == "" {
		return nil, r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "The asset Id is required."), false
	}
	asset, err := model.GetUserAssetLibraryAsset(r.caller.UserId, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, r.fail(http.StatusNotFound, assetLibraryCodeNotFound, fmt.Sprintf("The asset %s does not exist.", id)), false
	}
	if err != nil {
		return nil, r.internalError(err), false
	}
	return asset, AssetLibraryResponse{}, true
}

func validAssetLibraryName(name string, required bool) bool {
	length := utf8.RuneCountInString(name)
	return length <= assetLibraryNameMaxLength && (!required || strings.TrimSpace(name) != "")
}

func (r *assetLibraryRequest) createGroup() AssetLibraryResponse {
	var request struct {
		Name        string  `json:"Name"`
		Description *string `json:"Description"`
		GroupType   string  `json:"GroupType"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	if request.GroupType == "" {
		request.GroupType = volcark.GroupTypeAIGC
	}
	if request.GroupType != volcark.GroupTypeAIGC && request.GroupType != volcark.GroupTypeLivenessFace {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "GroupType must be AIGC or LivenessFace.")
	}
	if !validAssetLibraryName(request.Name, true) {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "Name is required and must be at most 64 characters.")
	}
	if request.Description != nil && utf8.RuneCountInString(*request.Description) > assetLibraryDescriptionMaxRunes {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "Description must be at most 300 characters.")
	}
	if limit := asset_library_setting.GetSetting().MaxGroupsPerUser; limit > 0 {
		count, err := model.CountAssetLibraryGroups("user_id", r.caller.UserId)
		if err != nil {
			return r.internalError(err)
		}
		if count >= int64(limit) {
			return r.fail(http.StatusBadRequest, assetLibraryCodeQuotaExceeded, fmt.Sprintf("You can have at most %d asset groups.", limit))
		}
	}
	account, response, ok := r.accountForNewGroup(request.GroupType)
	if !ok {
		return response
	}
	payload := map[string]any{"Name": request.Name, "GroupType": request.GroupType}
	if request.Description != nil {
		payload["Description"] = *request.Description
	}
	arkResponse, response, ok := r.forward(account, payload)
	if !ok {
		return response
	}
	var created volcark.CreatedResource
	if arkResponse.Decode(r.action, &created) != nil || created.Id == "" {
		return passThrough(arkResponse)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	group := &model.AssetLibraryGroup{
		UserId:           r.caller.UserId,
		AccountId:        account.Id,
		RemoteId:         created.Id,
		GroupType:        request.GroupType,
		Name:             request.Name,
		RemoteCreateTime: now,
		RemoteUpdateTime: now,
	}
	if request.Description != nil {
		group.Description = *request.Description
	}
	if err := model.CreateAssetLibraryGroup(group); err != nil {
		r.removeOrphan(account, "DeleteAssetGroup", created.Id)
		return r.internalError(err)
	}
	return passThrough(arkResponse)
}

// accountForNewGroup picks the highest-priority enabled account that takes
// groups of the type from the caller's user group and still has room.
func (r *assetLibraryRequest) accountForNewGroup(groupType string) (*model.AssetLibraryAccount, AssetLibraryResponse, bool) {
	accounts, err := model.ListAssetLibraryAccounts()
	if err != nil {
		return nil, r.internalError(err), false
	}
	accepting := false
	for _, account := range accounts {
		typeEnabled := account.AigcEnabled
		if groupType == volcark.GroupTypeLivenessFace {
			typeEnabled = account.LivenessFaceEnabled
		}
		if !account.Enabled || !typeEnabled || !account.UserGroupAllowed(r.caller.UserGroup) {
			continue
		}
		accepting = true
		if account.MaxGroups > 0 {
			count, err := model.CountAssetLibraryGroups("account_id", account.Id)
			if err != nil {
				return nil, r.internalError(err), false
			}
			if count >= int64(account.MaxGroups) {
				continue
			}
		}
		return account, AssetLibraryResponse{}, true
	}
	if accepting {
		return nil, r.fail(http.StatusBadRequest, assetLibraryCodeQuotaExceeded, "The asset library has no room for new asset groups."), false
	}
	return nil, r.fail(http.StatusForbidden, assetLibraryCodeAccessDenied, fmt.Sprintf("No asset library account accepts %s groups for you.", groupType)), false
}

// removeOrphan deletes a resource Ark created but the gateway failed to
// record, so it does not linger unowned.
func (r *assetLibraryRequest) removeOrphan(account *model.AssetLibraryAccount, action, id string) {
	err := AssetLibraryClient(account).Call(context.WithoutCancel(r.ctx), action, map[string]any{"Id": id, "ProjectName": account.ProjectName}, nil)
	if err != nil {
		common.SysError(common.LogText("asset library could not remove unrecorded %s via account %d: %s", id, account.Id, err.Error()))
	}
}

func (r *assetLibraryRequest) getGroup() AssetLibraryResponse {
	var request struct {
		Id string `json:"Id"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	group, response, ok := r.ownedGroup(request.Id)
	if !ok {
		return response
	}
	account, response, ok := r.account(group.AccountId)
	if !ok {
		return response
	}
	arkResponse, response, ok := r.forward(account, map[string]any{"Id": group.RemoteId})
	if !ok {
		return response
	}
	var remote volcark.AssetGroup
	if arkResponse.Decode(r.action, &remote) == nil && remote.Id == group.RemoteId {
		group.Name, group.Description = remote.Name, remote.Description
		group.RemoteCreateTime, group.RemoteUpdateTime = remote.CreateTime, remote.UpdateTime
		if err := model.UpdateAssetLibraryGroup(group); err != nil {
			common.SysError(common.LogText("asset library could not save group %s: %s", group.RemoteId, err.Error()))
		}
	}
	return passThrough(arkResponse)
}

func (r *assetLibraryRequest) updateGroup() AssetLibraryResponse {
	var request struct {
		Id          string  `json:"Id"`
		Name        *string `json:"Name"`
		Description *string `json:"Description"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	if request.Name != nil && !validAssetLibraryName(*request.Name, true) {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "Name must be 1 to 64 characters.")
	}
	if request.Description != nil && utf8.RuneCountInString(*request.Description) > assetLibraryDescriptionMaxRunes {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "Description must be at most 300 characters.")
	}
	group, response, ok := r.ownedGroup(request.Id)
	if !ok {
		return response
	}
	account, response, ok := r.account(group.AccountId)
	if !ok {
		return response
	}
	payload := map[string]any{"Id": group.RemoteId}
	if request.Name != nil {
		payload["Name"] = *request.Name
	}
	if request.Description != nil {
		payload["Description"] = *request.Description
	}
	arkResponse, response, ok := r.forward(account, payload)
	if !ok {
		return response
	}
	if arkResponse.Decode(r.action, nil) == nil {
		if request.Name != nil {
			group.Name = *request.Name
		}
		if request.Description != nil {
			group.Description = *request.Description
		}
		group.RemoteUpdateTime = time.Now().UTC().Format(time.RFC3339)
		if err := model.UpdateAssetLibraryGroup(group); err != nil {
			common.SysError(common.LogText("asset library could not save group %s: %s", group.RemoteId, err.Error()))
		}
	}
	return passThrough(arkResponse)
}

func (r *assetLibraryRequest) deleteGroup() AssetLibraryResponse {
	var request struct {
		Id string `json:"Id"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	group, response, ok := r.ownedGroup(request.Id)
	if !ok {
		return response
	}
	account, response, ok := r.account(group.AccountId)
	if !ok {
		return response
	}
	arkResponse, response, ok := r.forward(account, map[string]any{"Id": group.RemoteId})
	if !ok {
		return response
	}
	if arkResponse.Decode(r.action, nil) == nil {
		if err := model.DeleteAssetLibraryGroup(group); err != nil {
			return r.internalError(err)
		}
	}
	return passThrough(arkResponse)
}

func (r *assetLibraryRequest) createAsset() AssetLibraryResponse {
	var request struct {
		GroupId   string `json:"GroupId"`
		URL       string `json:"URL"`
		AssetType string `json:"AssetType"`
		Name      string `json:"Name"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	if request.AssetType != "Image" && request.AssetType != "Video" && request.AssetType != "Audio" {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "AssetType must be Image, Video or Audio.")
	}
	lowerURL := strings.ToLower(strings.TrimSpace(request.URL))
	if !strings.HasPrefix(lowerURL, "http://") && !strings.HasPrefix(lowerURL, "https://") {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "URL must be a public http(s) URL of the file.")
	}
	if !validAssetLibraryName(request.Name, false) {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "Name must be at most 64 characters.")
	}
	group, response, ok := r.ownedGroup(request.GroupId)
	if !ok {
		return response
	}
	if limit := asset_library_setting.GetSetting().MaxAssetsPerUser; limit > 0 {
		count, err := model.CountAssetLibraryAssets("user_id", r.caller.UserId)
		if err != nil {
			return r.internalError(err)
		}
		if count >= int64(limit) {
			return r.fail(http.StatusBadRequest, assetLibraryCodeQuotaExceeded, fmt.Sprintf("You can have at most %d assets.", limit))
		}
	}
	account, response, ok := r.account(group.AccountId)
	if !ok {
		return response
	}
	if account.MaxAssets > 0 {
		count, err := model.CountAssetLibraryAssets("account_id", account.Id)
		if err != nil {
			return r.internalError(err)
		}
		if count >= int64(account.MaxAssets) {
			return r.fail(http.StatusBadRequest, assetLibraryCodeQuotaExceeded, "The asset library has no room for new assets.")
		}
	}
	if !allowAssetLibraryUpload(r.ctx, account) {
		return r.fail(http.StatusTooManyRequests, assetLibraryCodeRequestLimit, "Too many uploads, please retry in a minute.")
	}
	payload := map[string]any{"GroupId": group.RemoteId, "URL": request.URL, "AssetType": request.AssetType}
	if request.Name != "" {
		payload["Name"] = request.Name
	}
	arkResponse, response, ok := r.forward(account, payload)
	if !ok {
		return response
	}
	var created volcark.CreatedResource
	if arkResponse.Decode(r.action, &created) != nil || created.Id == "" {
		return passThrough(arkResponse)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	asset := &model.AssetLibraryAsset{
		UserId:           r.caller.UserId,
		AccountId:        account.Id,
		GroupId:          group.Id,
		RemoteGroupId:    group.RemoteId,
		RemoteId:         created.Id,
		AssetType:        request.AssetType,
		Name:             request.Name,
		Status:           model.AssetLibraryAssetProcessing,
		RemoteCreateTime: now,
		RemoteUpdateTime: now,
	}
	if err := model.CreateAssetLibraryUpload(asset, r.caller.UserGroup); err != nil {
		r.removeOrphan(account, "DeleteAsset", created.Id)
		return r.internalError(err)
	}
	return passThrough(arkResponse)
}

// applyArkAsset copies what Ark reported onto the record.
func applyArkAsset(asset *model.AssetLibraryAsset, remote *volcark.Asset) {
	asset.Name = remote.Name
	if remote.Status != "" {
		asset.Status = remote.Status
	}
	asset.ErrorCode, asset.ErrorMessage = "", ""
	if remote.Error != nil {
		asset.ErrorCode, asset.ErrorMessage = remote.Error.Code, remote.Error.Message
	}
	if remote.URL != "" {
		asset.URL = remote.URL
		asset.URLRefreshAfter = time.Now().Add(assetLibraryURLRefreshInterval).Unix()
	}
	asset.LastInferenceTime = remote.LastInferenceTime
	if remote.CreateTime != "" {
		asset.RemoteCreateTime = remote.CreateTime
	}
	if remote.UpdateTime != "" {
		asset.RemoteUpdateTime = remote.UpdateTime
	}
}

func saveArkAsset(asset *model.AssetLibraryAsset, remote *volcark.Asset) {
	previous := asset.Status
	applyArkAsset(asset, remote)
	if err := model.SaveAssetLibraryAsset(asset, previous); err != nil {
		common.SysError(common.LogText("asset library could not save asset %s: %s", asset.RemoteId, err.Error()))
	}
}

func (r *assetLibraryRequest) getAsset() AssetLibraryResponse {
	var request struct {
		Id string `json:"Id"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	asset, response, ok := r.ownedAsset(request.Id)
	if !ok {
		return response
	}
	account, response, ok := r.account(asset.AccountId)
	if !ok {
		return response
	}
	arkResponse, response, ok := r.forward(account, map[string]any{"Id": asset.RemoteId})
	if !ok {
		return response
	}
	var remote volcark.Asset
	if arkResponse.Decode(r.action, &remote) == nil && remote.Id == asset.RemoteId {
		saveArkAsset(asset, &remote)
	}
	return passThrough(arkResponse)
}

func (r *assetLibraryRequest) updateAsset() AssetLibraryResponse {
	var request struct {
		Id   string  `json:"Id"`
		Name *string `json:"Name"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	if request.Name != nil && !validAssetLibraryName(*request.Name, false) {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "Name must be at most 64 characters.")
	}
	asset, response, ok := r.ownedAsset(request.Id)
	if !ok {
		return response
	}
	account, response, ok := r.account(asset.AccountId)
	if !ok {
		return response
	}
	payload := map[string]any{"Id": asset.RemoteId}
	if request.Name != nil {
		payload["Name"] = *request.Name
	}
	arkResponse, response, ok := r.forward(account, payload)
	if !ok {
		return response
	}
	if arkResponse.Decode(r.action, nil) == nil && request.Name != nil {
		asset.Name = *request.Name
		asset.RemoteUpdateTime = time.Now().UTC().Format(time.RFC3339)
		if err := model.SaveAssetLibraryAsset(asset, asset.Status); err != nil {
			common.SysError(common.LogText("asset library could not save asset %s: %s", asset.RemoteId, err.Error()))
		}
	}
	return passThrough(arkResponse)
}

func (r *assetLibraryRequest) deleteAsset() AssetLibraryResponse {
	var request struct {
		Id string `json:"Id"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	asset, response, ok := r.ownedAsset(request.Id)
	if !ok {
		return response
	}
	account, response, ok := r.account(asset.AccountId)
	if !ok {
		return response
	}
	arkResponse, response, ok := r.forward(account, map[string]any{"Id": asset.RemoteId})
	if !ok {
		return response
	}
	if arkResponse.Decode(r.action, nil) == nil {
		if err := model.DeleteAssetLibraryAsset(asset); err != nil {
			return r.internalError(err)
		}
	}
	return passThrough(arkResponse)
}

type assetLibraryListRequest struct {
	Filter struct {
		GroupType string   `json:"GroupType"`
		GroupIds  []string `json:"GroupIds"`
		Name      string   `json:"Name"`
		Statuses  []string `json:"Statuses"`
	} `json:"Filter"`
	NextToken  *string `json:"NextToken"`
	MaxResults *int    `json:"MaxResults"`
	PageNumber *int    `json:"PageNumber"`
	PageSize   *int    `json:"PageSize"`
	SortBy     string  `json:"SortBy"`
	SortOrder  string  `json:"SortOrder"`
}

// assetLibraryPage is where a list request starts and how it pages.
type assetLibraryPage struct {
	pageNumbers bool
	offset      int
	limit       int
}

// NextToken is an opaque cursor; it only encodes an offset into the
// caller's own records, so a forged token cannot reach anyone else's.
func encodeAssetLibraryNextToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("offset:" + strconv.Itoa(offset)))
}

func (r *assetLibraryRequest) parseList(request *assetLibraryListRequest, sortFields []string) (model.AssetLibraryListFilter, assetLibraryPage, AssetLibraryResponse, bool) {
	var filter model.AssetLibraryListFilter
	var page assetLibraryPage
	fail := func(message string) (model.AssetLibraryListFilter, assetLibraryPage, AssetLibraryResponse, bool) {
		return filter, page, r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, message), false
	}
	if request.Filter.GroupType != volcark.GroupTypeAIGC && request.Filter.GroupType != volcark.GroupTypeLivenessFace {
		return fail("Filter.GroupType is required and must be AIGC or LivenessFace.")
	}
	if utf8.RuneCountInString(request.Filter.Name) > assetLibraryNameMaxLength {
		return fail("Filter.Name must be at most 64 characters.")
	}
	sortBy := request.SortBy
	if sortBy == "" {
		sortBy = "CreateTime"
	}
	if !slices.Contains(sortFields, sortBy) {
		return fail("SortBy must be one of " + strings.Join(sortFields, ", ") + ".")
	}
	if request.SortOrder != "" && request.SortOrder != "Desc" && request.SortOrder != "Asc" {
		return fail("SortOrder must be Desc or Asc.")
	}
	usesTokens := request.NextToken != nil || request.MaxResults != nil
	usesPages := request.PageNumber != nil || request.PageSize != nil
	switch {
	case usesTokens && usesPages:
		return fail("NextToken and MaxResults cannot be used together with PageNumber and PageSize.")
	case usesPages:
		if request.PageNumber == nil || request.PageSize == nil {
			return fail("PageNumber and PageSize must be used together.")
		}
		if *request.PageNumber < 1 || *request.PageSize < 1 || *request.PageSize > assetLibraryMaxListResults {
			return fail("PageNumber must be at least 1 and PageSize must be between 1 and 100.")
		}
		page = assetLibraryPage{pageNumbers: true, offset: (*request.PageNumber - 1) * *request.PageSize, limit: *request.PageSize}
	default:
		page.limit = assetLibraryDefaultListResults
		if request.MaxResults != nil {
			if *request.MaxResults < 1 || *request.MaxResults > assetLibraryMaxListResults {
				return fail("MaxResults must be between 1 and 100.")
			}
			page.limit = *request.MaxResults
		}
		if request.NextToken != nil {
			decoded, err := base64.RawURLEncoding.DecodeString(*request.NextToken)
			offsetText, found := strings.CutPrefix(string(decoded), "offset:")
			offset, convErr := strconv.Atoi(offsetText)
			if err != nil || !found || convErr != nil || offset < 0 {
				return fail("NextToken is invalid.")
			}
			page.offset = offset
		}
	}
	filter = model.AssetLibraryListFilter{
		UserId:    r.caller.UserId,
		GroupType: request.Filter.GroupType,
		GroupIds:  request.Filter.GroupIds,
		Name:      request.Filter.Name,
		Statuses:  request.Filter.Statuses,
		SortBy:    sortBy,
		Ascending: request.SortOrder == "Asc",
		Offset:    page.offset,
		Limit:     page.limit,
	}
	return filter, page, AssetLibraryResponse{}, true
}

// pageResult adds the paging fields Ark returns for the pagination style
// the request used.
func pageResult(result map[string]any, page assetLibraryPage, returned int, total int64) map[string]any {
	if page.pageNumbers {
		result["TotalCount"] = total
		result["PageNumber"] = page.offset/page.limit + 1
		result["PageSize"] = page.limit
		return result
	}
	if next := page.offset + returned; returned > 0 && int64(next) < total {
		result["NextToken"] = encodeAssetLibraryNextToken(next)
	}
	return result
}

// accountProjects maps account ids to their project names.
func accountProjects() (map[int]string, error) {
	accounts, err := model.ListAssetLibraryAccounts()
	if err != nil {
		return nil, err
	}
	projects := make(map[int]string, len(accounts))
	for _, account := range accounts {
		projects[account.Id] = account.ProjectName
	}
	return projects, nil
}

func (r *assetLibraryRequest) listGroups() AssetLibraryResponse {
	var request assetLibraryListRequest
	if response, ok := r.decode(&request); !ok {
		return response
	}
	filter, page, response, ok := r.parseList(&request, []string{"CreateTime", "UpdateTime"})
	if !ok {
		return response
	}
	groups, total, err := model.ListAssetLibraryGroups(filter)
	if err != nil {
		return r.internalError(err)
	}
	projects, err := accountProjects()
	if err != nil {
		return r.internalError(err)
	}
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		items = append(items, map[string]any{
			"Id":          group.RemoteId,
			"Name":        group.Name,
			"Description": group.Description,
			"GroupType":   group.GroupType,
			"ProjectName": projects[group.AccountId],
			"CreateTime":  group.RemoteCreateTime,
			"UpdateTime":  group.RemoteUpdateTime,
		})
	}
	return r.respond(http.StatusOK, "", pageResult(map[string]any{"Items": items}, page, len(items), total), nil)
}

func (r *assetLibraryRequest) listAssets() AssetLibraryResponse {
	var request assetLibraryListRequest
	if response, ok := r.decode(&request); !ok {
		return response
	}
	for _, status := range request.Filter.Statuses {
		if status != model.AssetLibraryAssetActive && status != model.AssetLibraryAssetProcessing && status != model.AssetLibraryAssetFailed {
			return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "Filter.Statuses may contain Active, Processing and Failed.")
		}
	}
	filter, page, response, ok := r.parseList(&request, []string{"CreateTime", "UpdateTime", "GroupId"})
	if !ok {
		return response
	}
	assets, total, err := model.ListAssetLibraryAssets(filter)
	if err != nil {
		return r.internalError(err)
	}
	refreshAssetLibraryAssets(r.ctx, assets)
	projects, err := accountProjects()
	if err != nil {
		return r.internalError(err)
	}
	items := make([]map[string]any, 0, len(assets))
	for _, asset := range assets {
		item := map[string]any{
			"Id":          asset.RemoteId,
			"Name":        asset.Name,
			"AssetType":   asset.AssetType,
			"CreateTime":  asset.RemoteCreateTime,
			"UpdateTime":  asset.RemoteUpdateTime,
			"ProjectName": projects[asset.AccountId],
			"URL":         asset.URL,
			"GroupId":     asset.RemoteGroupId,
			"Status":      asset.Status,
			"Moderation":  map[string]any{"Strategy": "Default"},
		}
		if asset.LastInferenceTime != "" {
			item["LastInferenceTime"] = asset.LastInferenceTime
		}
		if asset.ErrorCode != "" || asset.ErrorMessage != "" {
			item["Error"] = map[string]any{"Code": asset.ErrorCode, "Message": asset.ErrorMessage}
		}
		items = append(items, item)
	}
	return r.respond(http.StatusOK, "", pageResult(map[string]any{"Items": items}, page, len(items), total), nil)
}

// refreshAssetLibraryAssets asks Ark again about the listed assets that are
// still processing or whose signed URL is missing or about to expire.
func refreshAssetLibraryAssets(ctx context.Context, assets []*model.AssetLibraryAsset) {
	now := time.Now().Unix()
	stale := make([]*model.AssetLibraryAsset, 0)
	for _, asset := range assets {
		if asset.Status == model.AssetLibraryAssetProcessing || (asset.Status == model.AssetLibraryAssetActive && (asset.URL == "" || asset.URLRefreshAfter < now)) {
			stale = append(stale, asset)
		}
	}
	if len(stale) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, assetLibraryRefreshTimeout)
	defer cancel()
	accounts := make(map[int]*model.AssetLibraryAccount)
	for _, asset := range stale {
		if _, loaded := accounts[asset.AccountId]; loaded {
			continue
		}
		account, err := model.GetAssetLibraryAccount(asset.AccountId)
		if err != nil {
			common.SysError(common.LogText("asset library account %d for asset %s: %s", asset.AccountId, asset.RemoteId, err.Error()))
		}
		accounts[asset.AccountId] = account
	}
	slots := make(chan struct{}, assetLibraryRefreshConcurrency)
	var wg sync.WaitGroup
	for _, asset := range stale {
		account := accounts[asset.AccountId]
		if account == nil {
			continue
		}
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			refreshAssetLibraryAsset(ctx, account, asset)
		})
	}
	wg.Wait()
}

// refreshAssetLibraryAsset reads one asset from Ark and saves the answer.
func refreshAssetLibraryAsset(ctx context.Context, account *model.AssetLibraryAccount, asset *model.AssetLibraryAsset) {
	var remote volcark.Asset
	err := AssetLibraryClient(account).Call(ctx, "GetAsset", map[string]any{"Id": asset.RemoteId, "ProjectName": account.ProjectName}, &remote)
	if err != nil {
		common.SysError(common.LogText("asset library could not refresh asset %s: %s", asset.RemoteId, err.Error()))
		return
	}
	if remote.Id == asset.RemoteId {
		saveArkAsset(asset, &remote)
	}
}

var assetLibraryUploadWindows = struct {
	sync.Mutex
	minute int64
	counts map[int]int
}{counts: map[int]int{}}

// allowAssetLibraryUpload enforces the account's CreateAsset rate per
// calendar minute, shared across nodes through Redis when it is enabled.
func allowAssetLibraryUpload(ctx context.Context, account *model.AssetLibraryAccount) bool {
	if account.CreateAssetQPM <= 0 {
		return true
	}
	minute := time.Now().Unix() / 60
	if common.RedisEnabled && common.RDB != nil {
		key := fmt.Sprintf("asset_library:uploads:%d:%d", account.Id, minute)
		pipe := common.RDB.TxPipeline()
		count := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, 2*time.Minute)
		if _, err := pipe.Exec(ctx); err == nil {
			return count.Val() <= int64(account.CreateAssetQPM)
		}
	}
	assetLibraryUploadWindows.Lock()
	defer assetLibraryUploadWindows.Unlock()
	if assetLibraryUploadWindows.minute != minute {
		assetLibraryUploadWindows.minute = minute
		assetLibraryUploadWindows.counts = map[int]int{}
	}
	if assetLibraryUploadWindows.counts[account.Id] >= account.CreateAssetQPM {
		return false
	}
	assetLibraryUploadWindows.counts[account.Id]++
	return true
}
