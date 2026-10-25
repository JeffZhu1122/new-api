package controller

import (
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/asset_library_setting"

	"github.com/gin-gonic/gin"
)

// assetLibraryMaxBodyBytes bounds one asset action's JSON body; the largest
// documented request is a ListAssets filter.
const assetLibraryMaxBodyBytes = 1 << 20

func writeAssetLibraryResponse(c *gin.Context, response service.AssetLibraryResponse) {
	c.Data(response.StatusCode, "application/json; charset=utf-8", response.Body)
}

// AssetLibraryTokenAuth authenticates /volc requests with a gateway API key
// (Authorization: Bearer sk-...) and answers failures in Ark's error format.
// While the asset library is off the endpoint does not exist.
func AssetLibraryTokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !asset_library_setting.GetSetting().Enabled {
			RelayNotFound(c)
			c.Abort()
			return
		}
		deny := func(status int, code, message string) {
			writeAssetLibraryResponse(c, service.AssetLibraryErrorResponse(c.Query("Action"), c.GetString(common.RequestIdKey), status, code, message))
			c.Abort()
		}
		key := strings.TrimSpace(c.Request.Header.Get("Authorization"))
		if len(key) > 7 && strings.EqualFold(key[:7], "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		key, _, _ = strings.Cut(strings.TrimPrefix(key, "sk-"), "-")
		token, err := model.ValidateUserToken(key)
		if err != nil || token == nil {
			deny(http.StatusUnauthorized, "InvalidAuthorization", "The API key is missing, invalid, expired or disabled.")
			return
		}
		if allowIps := token.GetIpLimits(); len(allowIps) > 0 {
			ip := net.ParseIP(c.ClientIP())
			if ip == nil || !common.IsIpInCIDRList(ip, allowIps) {
				deny(http.StatusForbidden, "AccessDenied", "The API key may not be used from this IP address.")
				return
			}
		}
		user, err := model.GetUserCache(token.UserId)
		if err != nil {
			deny(http.StatusInternalServerError, "InternalError", "The request failed because of an internal error.")
			return
		}
		if user.Status != common.UserStatusEnabled {
			deny(http.StatusForbidden, "AccessDenied", "The user is disabled.")
			return
		}
		c.Set("id", token.UserId)
		c.Set("token_id", token.Id)
		c.Next()
	}
}

// AssetLibraryOpenAPI runs one Ark asset action (?Action=...&Version=...) for
// the signed-in user or the API key's user: at /volc for API keys and at
// /api/asset-library/openapi for the web console.
func AssetLibraryOpenAPI(c *gin.Context) {
	if !asset_library_setting.GetSetting().Enabled {
		RelayNotFound(c)
		return
	}
	action, requestId := c.Query("Action"), c.GetString(common.RequestIdKey)
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, assetLibraryMaxBodyBytes+1))
	if err != nil || len(body) > assetLibraryMaxBodyBytes {
		writeAssetLibraryResponse(c, service.AssetLibraryErrorResponse(action, requestId, http.StatusRequestEntityTooLarge, "InvalidParameter", "The request body is too large."))
		return
	}
	userId := c.GetInt("id")
	user, err := model.GetUserCache(userId)
	if err != nil {
		writeAssetLibraryResponse(c, service.AssetLibraryErrorResponse(action, requestId, http.StatusInternalServerError, "InternalError", "The request failed because of an internal error."))
		return
	}
	caller := service.AssetLibraryCaller{UserId: userId, UserGroup: user.Group}
	writeAssetLibraryResponse(c, service.HandleAssetLibraryAction(c.Request.Context(), caller, action, c.Query("Version"), body, requestId))
}
