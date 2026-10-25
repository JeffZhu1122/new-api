package controller

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/asset_library_setting"

	"github.com/gin-gonic/gin"
)

// assetLibraryAvailable answers 404 like a missing route while the asset
// library is off.
func assetLibraryAvailable(c *gin.Context) bool {
	if asset_library_setting.GetSetting().Enabled {
		return true
	}
	RelayNotFound(c)
	return false
}

// GetAssetLibraryConfig reports the signed-in user's limits, usage, the
// group types they can create and the compliance notice.
func GetAssetLibraryConfig(c *gin.Context) {
	if !assetLibraryAvailable(c) {
		return
	}
	userId := c.GetInt("id")
	user, err := model.GetUserCache(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	config, err := service.GetAssetLibraryUserConfig(userId, user.Group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, config)
}

// AdminListAssetLibraryGroups lists every user's asset groups.
func AdminListAssetLibraryGroups(c *gin.Context) {
	if !assetLibraryAvailable(c) {
		return
	}
	pageInfo := common.GetPageQuery(c)
	userId, _ := strconv.Atoi(c.Query("user_id"))
	accountId, _ := strconv.Atoi(c.Query("account_id"))
	groups, total, err := service.ListAssetLibraryGroupsForAdmin(model.AssetLibraryAdminFilter{
		UserId:    userId,
		AccountId: accountId,
		GroupType: c.Query("group_type"),
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		Offset:    pageInfo.GetStartIdx(),
		Limit:     pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(groups)
	common.ApiSuccess(c, pageInfo)
}

// AdminListAssetLibraryGroupAssets lists the assets of any user's group.
func AdminListAssetLibraryGroupAssets(c *gin.Context) {
	if !assetLibraryAvailable(c) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	assets, err := service.ListAssetLibraryGroupAssetsForAdmin(ctx, c.Param("id"))
	if errors.Is(err, service.ErrAssetLibraryNotFound) {
		common.ApiErrorT(c, "Asset group not found")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, assets)
}

// AdminDeleteAssetLibraryGroup deletes any user's group on Volcengine.
func AdminDeleteAssetLibraryGroup(c *gin.Context) {
	if !assetLibraryAvailable(c) {
		return
	}
	adminDeleteAssetLibraryResource(c, service.DeleteAssetLibraryGroupForAdmin, "Asset group not found")
}

// AdminDeleteAssetLibraryAsset deletes any user's asset on Volcengine.
func AdminDeleteAssetLibraryAsset(c *gin.Context) {
	if !assetLibraryAvailable(c) {
		return
	}
	adminDeleteAssetLibraryResource(c, service.DeleteAssetLibraryAssetForAdmin, "Asset not found")
}

func adminDeleteAssetLibraryResource(c *gin.Context, remove func(context.Context, string) error, notFoundKey string) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	err := remove(ctx, c.Param("id"))
	if errors.Is(err, service.ErrAssetLibraryNotFound) {
		common.ApiErrorT(c, notFoundKey)
		return
	}
	if err != nil {
		common.ApiErrorT(c, "Volcengine refused the request: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	common.ApiSuccess(c, nil)
}
