package controller

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/volcark"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// assetLibraryAccountView is an account as the settings page sees it: the
// Secret Access Key is never returned and the Access Key ID only masked.
type assetLibraryAccountView struct {
	Id                  int      `json:"id"`
	Name                string   `json:"name"`
	AccessKeyIdMasked   string   `json:"access_key_id_masked"`
	HasSecret           bool     `json:"has_secret"`
	Region              string   `json:"region"`
	ProjectName         string   `json:"project_name"`
	Endpoint            string   `json:"endpoint"`
	Enabled             bool     `json:"enabled"`
	AigcEnabled         bool     `json:"aigc_enabled"`
	LivenessFaceEnabled bool     `json:"liveness_face_enabled"`
	LivenessFaceVerify  bool     `json:"liveness_face_verify"`
	AllowedUserGroups   []string `json:"allowed_user_groups"`
	Priority            int      `json:"priority"`
	MaxGroups           int      `json:"max_groups"`
	MaxAssets           int      `json:"max_assets"`
	CreateAssetQPM      int      `json:"create_asset_qpm"`
	CreatedTime         int64    `json:"created_time"`
	UpdatedTime         int64    `json:"updated_time"`
}

// assetLibraryAccountRequest creates or updates an account. On update, an
// empty Access Key ID or Secret Access Key keeps the stored value.
type assetLibraryAccountRequest struct {
	Id                  int      `json:"id"`
	Name                string   `json:"name"`
	AccessKeyId         string   `json:"access_key_id"`
	SecretAccessKey     string   `json:"secret_access_key"`
	Region              string   `json:"region"`
	ProjectName         string   `json:"project_name"`
	Endpoint            string   `json:"endpoint"`
	Enabled             bool     `json:"enabled"`
	AigcEnabled         bool     `json:"aigc_enabled"`
	LivenessFaceEnabled bool     `json:"liveness_face_enabled"`
	LivenessFaceVerify  bool     `json:"liveness_face_verify"`
	AllowedUserGroups   []string `json:"allowed_user_groups"`
	Priority            int      `json:"priority"`
	MaxGroups           int      `json:"max_groups"`
	MaxAssets           int      `json:"max_assets"`
	CreateAssetQPM      int      `json:"create_asset_qpm"`
}

const (
	maxAssetLibraryAccountLimit = 100_000_000
	maxAssetLibraryPriority     = 1_000_000
)

var assetLibraryRegionPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

func newAssetLibraryAccountView(account *model.AssetLibraryAccount) assetLibraryAccountView {
	maskedAccessKeyId := strings.Repeat("*", len(account.AccessKeyId))
	if len(account.AccessKeyId) > 8 {
		maskedAccessKeyId = account.AccessKeyId[:4] + "****" + account.AccessKeyId[len(account.AccessKeyId)-4:]
	}
	return assetLibraryAccountView{
		Id:                  account.Id,
		Name:                account.Name,
		AccessKeyIdMasked:   maskedAccessKeyId,
		HasSecret:           account.SecretAccessKey != "",
		Region:              account.Region,
		ProjectName:         account.ProjectName,
		Endpoint:            account.Endpoint,
		Enabled:             account.Enabled,
		AigcEnabled:         account.AigcEnabled,
		LivenessFaceEnabled: account.LivenessFaceEnabled,
		LivenessFaceVerify:  account.LivenessFaceVerify,
		AllowedUserGroups:   account.AllowedUserGroupList(),
		Priority:            account.Priority,
		MaxGroups:           account.MaxGroups,
		MaxAssets:           account.MaxAssets,
		CreateAssetQPM:      account.CreateAssetQPM,
		CreatedTime:         account.CreatedTime,
		UpdatedTime:         account.UpdatedTime,
	}
}

// toAccount validates the request and applies it onto base (a new account
// or the stored one).
func (r assetLibraryAccountRequest) toAccount(c *gin.Context, base *model.AssetLibraryAccount) (*model.AssetLibraryAccount, bool) {
	account := *base
	account.Name = strings.TrimSpace(r.Name)
	if account.Name == "" || len([]rune(account.Name)) > 64 {
		common.ApiErrorT(c, "Account name must be 1 to 64 characters")
		return nil, false
	}
	if accessKeyId := strings.TrimSpace(r.AccessKeyId); accessKeyId != "" {
		account.AccessKeyId = accessKeyId
	}
	if secret := strings.TrimSpace(r.SecretAccessKey); secret != "" {
		account.SecretAccessKey = secret
	}
	if account.AccessKeyId == "" || account.SecretAccessKey == "" || len(account.AccessKeyId) > 128 || len(account.SecretAccessKey) > 256 {
		common.ApiErrorT(c, "Access Key ID and Secret Access Key are required")
		return nil, false
	}
	account.Region = strings.TrimSpace(r.Region)
	if account.Region == "" {
		account.Region = volcark.DefaultRegion
	}
	if !assetLibraryRegionPattern.MatchString(account.Region) {
		common.ApiErrorT(c, "Invalid region")
		return nil, false
	}
	account.ProjectName = strings.TrimSpace(r.ProjectName)
	if account.ProjectName == "" {
		account.ProjectName = "default"
	}
	if len([]rune(account.ProjectName)) > 64 {
		common.ApiErrorT(c, "Project name must be at most 64 characters")
		return nil, false
	}
	account.Endpoint = strings.TrimSpace(r.Endpoint)
	if account.Endpoint != "" {
		parsed, err := url.Parse(account.Endpoint)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.RawQuery != "" || len(account.Endpoint) > 255 {
			common.ApiErrorT(c, "Invalid endpoint URL")
			return nil, false
		}
	}
	if r.MaxGroups < 0 || r.MaxGroups > maxAssetLibraryAccountLimit || r.MaxAssets < 0 || r.MaxAssets > maxAssetLibraryAccountLimit ||
		r.CreateAssetQPM < 0 || r.CreateAssetQPM > maxAssetLibraryAccountLimit || r.Priority < -maxAssetLibraryPriority || r.Priority > maxAssetLibraryPriority {
		common.ApiErrorT(c, "Invalid account limits")
		return nil, false
	}
	groups := make([]string, 0, len(r.AllowedUserGroups))
	for _, group := range r.AllowedUserGroups {
		group = strings.TrimSpace(group)
		if group == "" || len(group) > 64 || len(groups) >= 100 {
			common.ApiErrorT(c, "Invalid allowed user groups")
			return nil, false
		}
		groups = append(groups, group)
	}
	encodedGroups, err := common.Marshal(groups)
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	account.AllowedUserGroups = string(encodedGroups)
	account.Enabled = r.Enabled
	account.AigcEnabled = r.AigcEnabled
	account.LivenessFaceEnabled = r.LivenessFaceEnabled
	account.LivenessFaceVerify = r.LivenessFaceVerify
	account.Priority = r.Priority
	account.MaxGroups = r.MaxGroups
	account.MaxAssets = r.MaxAssets
	account.CreateAssetQPM = r.CreateAssetQPM
	return &account, true
}

// storedAssetLibraryAccount loads the account a request refers to, or
// responds with an error.
func storedAssetLibraryAccount(c *gin.Context, id int) (*model.AssetLibraryAccount, bool) {
	account, err := model.GetAssetLibraryAccount(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorT(c, "Asset library account not found")
		return nil, false
	}
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	return account, true
}

func ListAssetLibraryAccounts(c *gin.Context) {
	accounts, err := model.ListAssetLibraryAccounts()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]assetLibraryAccountView, 0, len(accounts))
	for _, account := range accounts {
		views = append(views, newAssetLibraryAccountView(account))
	}
	common.ApiSuccess(c, views)
}

func CreateAssetLibraryAccount(c *gin.Context) {
	var request assetLibraryAccountRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	account, ok := request.toAccount(c, &model.AssetLibraryAccount{})
	if !ok {
		return
	}
	if err := model.CreateAssetLibraryAccount(account); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, newAssetLibraryAccountView(account))
}

func UpdateAssetLibraryAccount(c *gin.Context) {
	var request assetLibraryAccountRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	stored, ok := storedAssetLibraryAccount(c, request.Id)
	if !ok {
		return
	}
	account, ok := request.toAccount(c, stored)
	if !ok {
		return
	}
	if err := model.UpdateAssetLibraryAccount(account); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, newAssetLibraryAccountView(account))
}

func DeleteAssetLibraryAccount(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Asset library account not found")
		return
	}
	if _, ok := storedAssetLibraryAccount(c, id); !ok {
		return
	}
	groups, err := model.CountAssetLibraryGroups("account_id", id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if groups > 0 {
		common.ApiErrorT(c, "This account still holds {{count}} asset groups and cannot be deleted", map[string]any{"count": groups})
		return
	}
	if err := model.DeleteAssetLibraryAccount(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// TestAssetLibraryAccount checks the account in the request, before or after
// it is saved: with an id, empty keys fall back to the stored ones.
func TestAssetLibraryAccount(c *gin.Context) {
	var request assetLibraryAccountRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	base := &model.AssetLibraryAccount{}
	if request.Id != 0 {
		stored, ok := storedAssetLibraryAccount(c, request.Id)
		if !ok {
			return
		}
		base = stored
	}
	account, ok := request.toAccount(c, base)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := service.TestAssetLibraryAccount(ctx, account); err != nil {
		common.ApiErrorT(c, "Connection test failed: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	common.ApiSuccessT(c, "Connection test succeeded", nil)
}

// assetLibraryAccountOption is what the channel editor needs to bind a
// channel to an account, without any key.
type assetLibraryAccountOption struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	ProjectName string `json:"project_name"`
	Enabled     bool   `json:"enabled"`
}

// ListAssetLibraryAccountOptions lists the accounts a channel can be bound
// to, for administrators who edit channels but cannot see the keys.
func ListAssetLibraryAccountOptions(c *gin.Context) {
	accounts, err := model.ListAssetLibraryAccounts()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	options := make([]assetLibraryAccountOption, 0, len(accounts))
	for _, account := range accounts {
		options = append(options, assetLibraryAccountOption{Id: account.Id, Name: account.Name, ProjectName: account.ProjectName, Enabled: account.Enabled})
	}
	common.ApiSuccess(c, options)
}
