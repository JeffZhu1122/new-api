package controller

import (
	"maps"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Root management of other users' API keys under /api/user/:id/tokens. The
// routes are RootAuth and browser-session only. Creates and updates run the
// self-service token handlers with the managed user as the owner, so quota,
// count and group rules stay identical. Revealing a key also needs an
// admin.user.token.read proof bound to the user and the key.

// adminTokenOwner loads the user named by :id and makes the shared token
// group checks validate against that user's group instead of the root user's.
func adminTokenOwner(c *gin.Context) (*model.User, bool) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorT(c, "Invalid user ID")
		return nil, false
	}
	owner, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiErrorT(c, "User does not exist")
		return nil, false
	}
	group := owner.Group
	if group == "" {
		// users.group defaults to "default"; never fall back to the operator's group.
		group = "default"
	}
	common.SetContextKey(c, constant.ContextKeyUserGroup, group)
	c.Set("group", group)
	return owner, true
}

// adminOwnedToken loads :token_id only when it belongs to owner.
func adminOwnedToken(c *gin.Context, owner *model.User) (*model.Token, bool) {
	tokenId, err := strconv.Atoi(c.Param("token_id"))
	if err != nil || tokenId <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return nil, false
	}
	token, err := model.GetTokenByIds(tokenId, owner.Id)
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	return token, true
}

// adminTokenNotices are the system log lines that tell a user the root user
// acted on one of their API keys. Root audit entries stay hidden from other
// roles, so the owner learns about it from their own logs instead.
var adminTokenNotices = map[string]string{
	"user.token_create":        "An administrator created the API key {{name}} for you",
	"user.token_update":        "An administrator updated your API key {{name}}",
	"user.token_status_update": "An administrator changed the status of your API key {{name}}",
	"user.token_delete":        "An administrator deleted your API key {{name}}",
	"user.token_key_view":      "An administrator viewed your API key {{name}}",
}

// recordAdminTokenAudit records a successful root operation on another user's
// API key in the audit log and notifies the owner. Failed writes are left to
// the RootAuth fallback audit. Key values are never recorded.
func recordAdminTokenAudit(c *gin.Context, owner *model.User, action string) {
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenAuditSucceeded) {
		return
	}
	params := map[string]any{}
	maps.Copy(params, tokenAuditParams(c))
	params["target_user_id"] = owner.Id
	params["target_username"] = owner.Username
	recordManageAuditFor(c, owner.Id, action, params)
	if owner.Id != c.GetInt("id") {
		model.RecordLog(owner.Id, model.LogTypeSystem, common.NewMessage(adminTokenNotices[action], map[string]any{"name": params["name"]}))
	}
}

func AdminGetUserTokens(c *gin.Context) {
	owner, ok := adminTokenOwner(c)
	if !ok {
		return
	}
	pageInfo := common.GetPageQuery(c)
	keyword, key := c.Query("keyword"), c.Query("token")
	var tokens []*model.Token
	var total int64
	var err error
	if keyword != "" || key != "" {
		tokens, total, err = model.SearchUserTokens(owner.Id, keyword, key, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	} else {
		tokens, err = model.GetAllUserTokens(owner.Id, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
		if err == nil {
			total, err = model.CountUserTokens(owner.Id)
		}
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(buildMaskedTokenResponses(tokens))
	common.ApiSuccess(c, pageInfo)
}

func AdminGetUserToken(c *gin.Context) {
	owner, ok := adminTokenOwner(c)
	if !ok {
		return
	}
	token, ok := adminOwnedToken(c, owner)
	if !ok {
		return
	}
	common.ApiSuccess(c, buildMaskedTokenResponse(token))
}

// AdminGetUserTokenGroups returns the groups the managed user may bind a key
// to, in the shape of GET /api/user/self/groups.
func AdminGetUserTokenGroups(c *gin.Context) {
	if _, ok := adminTokenOwner(c); !ok {
		return
	}
	groups := make(map[string]map[string]any)
	// adminTokenOwner stored the owner's group under "group".
	for _, entry := range userSelectableGroups(c.GetString("group")) {
		groups[entry.Group] = map[string]any{"ratio": entry.Ratio, "desc": entry.Desc}
	}
	common.ApiSuccess(c, groups)
}

func AdminGetUserTokenAutoGroups(c *gin.Context) {
	if _, ok := adminTokenOwner(c); !ok {
		return
	}
	GetTokenAutoGroups(c)
}

// AdminGetUserTokenModels lists the models the managed user can call, as
// GET /api/user/models does for the signed-in user (it reads :id).
func AdminGetUserTokenModels(c *gin.Context) {
	if _, ok := adminTokenOwner(c); !ok {
		return
	}
	GetUserModels(c)
}

func AdminAddUserToken(c *gin.Context) {
	owner, ok := adminTokenOwner(c)
	if !ok {
		return
	}
	addTokenFor(c, owner.Id)
	recordAdminTokenAudit(c, owner, "user.token_create")
}

func AdminUpdateUserToken(c *gin.Context) {
	owner, ok := adminTokenOwner(c)
	if !ok {
		return
	}
	updateTokenFor(c, owner.Id)
	action := "user.token_update"
	if c.Query("status_only") != "" {
		action = "user.token_status_update"
	}
	recordAdminTokenAudit(c, owner, action)
}

func AdminDeleteUserToken(c *gin.Context) {
	owner, ok := adminTokenOwner(c)
	if !ok {
		return
	}
	token, ok := adminOwnedToken(c, owner)
	if !ok {
		return
	}
	if err := token.Delete(); err != nil {
		common.ApiError(c, err)
		return
	}
	params := tokenAuditParams(c)
	params["id"], params["name"] = token.Id, token.Name
	common.SetContextKey(c, constant.ContextKeyTokenAuditSucceeded, true)
	recordAdminTokenAudit(c, owner, "user.token_delete")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// AdminGetUserTokenKey reveals one key of the managed user after a step-up
// proof bound to that user and key.
func AdminGetUserTokenKey(c *gin.Context) {
	owner, ok := adminTokenOwner(c)
	if !ok {
		return
	}
	token, ok := adminOwnedToken(c, owner)
	if !ok {
		return
	}
	authorization := requireAdminUserProof(c, service.VerificationScopeAdminUserTokenRead, service.AdminUserTokenContext{UserID: owner.Id, TokenID: token.Id})
	if authorization == nil {
		return
	}
	params := tokenAuditParams(c)
	params["id"], params["name"] = token.Id, token.Name
	params["verification_method"] = authorization.Method
	common.SetContextKey(c, constant.ContextKeyTokenAuditSucceeded, true)
	recordAdminTokenAudit(c, owner, "user.token_key_view")
	common.ApiSuccess(c, gin.H{
		"key": token.GetFullKey(),
	})
}
