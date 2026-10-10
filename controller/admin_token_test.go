package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAdminTokenTest prepares a root operator in group "ops" and a managed
// user in group "default". Only the operator may select "ops".
func setupAdminTokenTest(t *testing.T) (*model.User, service.AuthIdentity, *model.User) {
	t.Helper()
	operator, identity, target := setupAdminUserTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}))
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
	require.NoError(t, model.DB.Model(operator).Update("group", "ops").Error)
	originalUsable := setting.UserUsableGroups2JSONString()
	originalRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsable))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatio))
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default group"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"ops":1}`))
	return operator, identity, target
}

func adminTokenParams(target *model.User, tokenId int) gin.Params {
	params := gin.Params{{Key: "id", Value: strconv.Itoa(target.Id)}}
	if tokenId > 0 {
		params = append(params, gin.Param{Key: "token_id", Value: strconv.Itoa(tokenId)})
	}
	return params
}

func TestAdminUserTokensActOnTheManagedUser(t *testing.T) {
	operator, identity, target := setupAdminTokenTest(t)
	ownToken := seedToken(t, model.DB, operator.Id, "operator key", "operatorkey0000000000000000000000000000000000000")

	// create: the key belongs to the managed user and both audit trails are written
	response := adminUserRequest(http.MethodPost, "/api/user/tokens", `{"name":"managed key","unlimited_quota":true,"expired_time":-1,"group":"default"}`, "", identity, common.RoleRootUser, adminTokenParams(target, 0), AdminAddUserToken)
	require.True(t, decodeSecurityEnrollmentResponse(t, response).Success, response.Body.String())
	var created model.Token
	var stored *model.Token
	require.NoError(t, model.DB.Where("name = ?", "managed key").First(&created).Error)
	assert.Equal(t, target.Id, created.UserId)
	var operatorAudit model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action = ? AND user_id = ?", "user.token_create", operator.Id).Last(&operatorAudit).Error)
	require.NotNil(t, operatorAudit.Other.Op)
	targetUserID, err := common.Marshal(operatorAudit.Other.Op.Params["target_user_id"])
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprint(target.Id), string(targetUserID))
	var notice model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", target.Id, model.LogTypeSystem).Last(&notice).Error)
	assert.Equal(t, "An administrator created the API key managed key for you", notice.Content)

	// fallback groups are checked against the managed user, not the operator
	response = adminUserRequest(http.MethodPost, "/api/user/tokens", `{"name":"ops fallback","unlimited_quota":true,"expired_time":-1,"group":"default","auto_groups":["ops"]}`, "", identity, common.RoleRootUser, adminTokenParams(target, 0), AdminAddUserToken)
	body := decodeSecurityEnrollmentResponse(t, response)
	assert.False(t, body.Success)
	assert.Contains(t, body.Message, "ops")
	var count int64
	require.NoError(t, model.DB.Model(&model.Token{}).Where("name = ?", "ops fallback").Count(&count).Error)
	assert.Zero(t, count)

	// the list holds only the managed user's keys, masked
	response = adminUserRequest(http.MethodGet, "/api/user/tokens", "", "", identity, common.RoleRootUser, adminTokenParams(target, 0), AdminGetUserTokens)
	assert.Contains(t, response.Body.String(), "managed key")
	assert.NotContains(t, response.Body.String(), "operator key")
	assert.NotContains(t, response.Body.String(), created.Key)

	// a key of another user cannot be read, updated or deleted through this user
	for _, handler := range []gin.HandlerFunc{AdminGetUserToken, AdminDeleteUserToken} {
		response = adminUserRequest(http.MethodGet, "/api/user/tokens", "", "", identity, common.RoleRootUser, adminTokenParams(target, ownToken.Id), handler)
		assert.False(t, decodeSecurityEnrollmentResponse(t, response).Success)
	}
	response = adminUserRequest(http.MethodPut, "/api/user/tokens", fmt.Sprintf(`{"id":%d,"name":"hijacked","unlimited_quota":true,"expired_time":-1}`, ownToken.Id), "", identity, common.RoleRootUser, adminTokenParams(target, 0), AdminUpdateUserToken)
	assert.False(t, decodeSecurityEnrollmentResponse(t, response).Success)
	stored, err = model.GetTokenById(ownToken.Id)
	require.NoError(t, err)
	assert.Equal(t, "operator key", stored.Name)

	// update the managed key
	response = adminUserRequest(http.MethodPut, "/api/user/tokens", fmt.Sprintf(`{"id":%d,"name":"renamed","unlimited_quota":true,"expired_time":-1,"group":"default"}`, created.Id), "", identity, common.RoleRootUser, adminTokenParams(target, 0), AdminUpdateUserToken)
	require.True(t, decodeSecurityEnrollmentResponse(t, response).Success, response.Body.String())
	stored, err = model.GetTokenById(created.Id)
	require.NoError(t, err)
	assert.Equal(t, "renamed", stored.Name)

	// delete the managed key
	response = adminUserRequest(http.MethodDelete, "/api/user/tokens", "", "", identity, common.RoleRootUser, adminTokenParams(target, created.Id), AdminDeleteUserToken)
	require.True(t, decodeSecurityEnrollmentResponse(t, response).Success, response.Body.String())
	_, err = model.GetTokenById(created.Id)
	assert.Error(t, err)
}

func TestAdminUserTokenKeyRequiresProofBoundToTheKey(t *testing.T) {
	operator, identity, target := setupAdminTokenTest(t)
	first := seedToken(t, model.DB, target.Id, "first", "firstmanagedkey00000000000000000000000000000000")
	second := seedToken(t, model.DB, target.Id, "second", "secondmanagedkey0000000000000000000000000000000")
	readFirst := service.VerificationOperation{Scope: service.VerificationScopeAdminUserTokenRead, Context: []byte(fmt.Sprintf(`{"user_id":%d,"token_id":%d}`, target.Id, first.Id))}

	response := adminUserRequest(http.MethodPost, "/api/user/tokens/key", "", "", identity, common.RoleRootUser, adminTokenParams(target, first.Id), AdminGetUserTokenKey)
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Equal(t, "SECURITY_PROOF_REQUIRED", decodeSecurityEnrollmentResponse(t, response).Code)

	proof := issueSecurityEnrollmentProof(t, identity, readFirst, service.VerificationMethodPassword)
	response = adminUserRequest(http.MethodPost, "/api/user/tokens/key", "", proof, identity, common.RoleRootUser, adminTokenParams(target, second.Id), AdminGetUserTokenKey)
	assert.Equal(t, "SECURITY_PROOF_CONTEXT_MISMATCH", decodeSecurityEnrollmentResponse(t, response).Code)
	assert.NotContains(t, response.Body.String(), second.Key)

	response = adminUserRequest(http.MethodPost, "/api/user/tokens/key", "", proof, identity, common.RoleRootUser, adminTokenParams(target, first.Id), AdminGetUserTokenKey)
	body := decodeSecurityEnrollmentResponse(t, response)
	require.True(t, body.Success, response.Body.String())
	assert.Contains(t, string(body.Data), first.Key)

	var audit model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action = ? AND user_id = ?", "user.token_key_view", operator.Id).Last(&audit).Error)
	verificationMethod, err := common.Marshal(audit.Other.Op.Params["verification_method"])
	require.NoError(t, err)
	assert.JSONEq(t, `"password"`, string(verificationMethod))
	auditJSON, err := common.Marshal(audit)
	require.NoError(t, err)
	assert.NotContains(t, string(auditJSON), first.Key)
	var notice model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", target.Id, model.LogTypeSystem).Last(&notice).Error)
	assert.Equal(t, "An administrator viewed your API key first", notice.Content)
	assert.NotContains(t, string(auditJSON), proof)

	response = adminUserRequest(http.MethodPost, "/api/user/tokens/key", "", proof, identity, common.RoleRootUser, adminTokenParams(target, first.Id), AdminGetUserTokenKey)
	assert.Equal(t, "SECURITY_PROOF_CONSUMED", decodeSecurityEnrollmentResponse(t, response).Code)
}

func TestAdminUserTokenKeyVerificationPolicy(t *testing.T) {
	t.Run("only the root user can request it", func(t *testing.T) {
		operator, identity, _ := setupAdminTokenTest(t)
		require.NoError(t, model.DB.Model(operator).Update("role", common.RoleAdminUser).Error)
		require.NoError(t, model.PublishUserAuthCache(operator.Id))
		_, err := service.GetVerificationRequirements(identity, service.VerificationScopeAdminUserTokenRead)
		assert.ErrorIs(t, err, service.ErrVerificationForbidden)
	})
	t.Run("a context naming no key is rejected", func(t *testing.T) {
		_, err := service.BindVerificationOperation(service.VerificationOperation{Scope: service.VerificationScopeAdminUserTokenRead, Context: []byte(`{"user_id":3}`)})
		assert.ErrorIs(t, err, service.ErrVerificationContextInvalid)
	})
	t.Run("access tokens cannot obtain the proof", func(t *testing.T) {
		scope, listed := service.AccessTokenVerificationScope(service.VerificationScopeAdminUserTokenRead)
		assert.True(t, listed)
		assert.Empty(t, scope)
	})
	t.Run("access tokens cannot call the routes", func(t *testing.T) {
		operator, _, target := setupAdminTokenTest(t)
		raw, _ := createScopedAccessToken(t, operator.Id, 0, "user:read", "user:write")
		router := gin.New()
		router.GET("/api/user/:id/tokens", middleware.RootAuth(), AdminGetUserTokens)
		response := accessTokenRequest(router, http.MethodGet, fmt.Sprintf("/api/user/%d/tokens", target.Id), raw, "", "")
		assert.Equal(t, http.StatusForbidden, response.Code)
		assert.Equal(t, "AUTH_SESSION_REQUIRED", decodeSecurityEnrollmentResponse(t, response).Code)
	})
}
