package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTokenAutoGroupsContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx
}

func TestSetupContextForTokenPreservesCustomAutoGroupsOrder(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, AutoGroups: `["vip","default"]`}

	require.NoError(t, SetupContextForToken(ctx, token))
	value, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{"vip", "default"}, value)
}

func TestSetupContextForTokenTreatsStoredEmptyArrayAsInheritance(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, AutoGroups: `[]`}

	require.NoError(t, SetupContextForToken(ctx, token))
	_, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	assert.False(t, ok)
}

func TestSetupContextForTokenMalformedAutoGroupsFailsClosed(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, AutoGroups: `not-json`}

	require.NoError(t, SetupContextForToken(ctx, token))
	value, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{}, value)
}

func TestSetupContextForTokenNormalizesMultiGroupTokenToAutoSemantics(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, Group: "vip", CrossGroupRetry: true, AutoGroups: `["default","svip"]`}

	require.NoError(t, SetupContextForToken(ctx, token))
	assert.Equal(t, "auto", common.GetContextKeyString(ctx, constant.ContextKeyTokenGroup))
	assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyTokenCrossGroupRetry))
	value, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{"vip", "default", "svip"}, value)
}

func TestSetupContextForTokenDropsFallbackThatRepeatsPrimary(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, Group: "vip", AutoGroups: `["vip","default"]`}

	require.NoError(t, SetupContextForToken(ctx, token))
	assert.Equal(t, "auto", common.GetContextKeyString(ctx, constant.ContextKeyTokenGroup))
	value, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{"vip", "default"}, value)
}

func TestSetupContextForTokenKeepsSingleGroupTokenUntouched(t *testing.T) {
	tests := []struct {
		name  string
		token *model.Token
	}{
		{name: "no stored list", token: &model.Token{Id: 1, UserId: 2, Group: "vip"}},
		{name: "empty stored list", token: &model.Token{Id: 1, UserId: 2, Group: "vip", AutoGroups: `[]`}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newTokenAutoGroupsContext()

			require.NoError(t, SetupContextForToken(ctx, test.token))
			assert.Equal(t, "vip", common.GetContextKeyString(ctx, constant.ContextKeyTokenGroup))
			_, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
			assert.False(t, ok)
		})
	}
}

func TestSetupContextForTokenMalformedFallbacksFallBackToPrimaryGroup(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, Group: "vip", AutoGroups: `not-json`}

	require.NoError(t, SetupContextForToken(ctx, token))
	assert.Equal(t, "vip", common.GetContextKeyString(ctx, constant.ContextKeyTokenGroup))
	value, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{}, value)
}

func TestSetupContextForTokenKeepsAutoTokenGroupLiteral(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, Group: "auto", AutoGroups: `["vip","default"]`}

	require.NoError(t, SetupContextForToken(ctx, token))
	assert.Equal(t, "auto", common.GetContextKeyString(ctx, constant.ContextKeyTokenGroup))
	value, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{"vip", "default"}, value)
}
