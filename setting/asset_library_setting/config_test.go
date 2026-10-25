package asset_library_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateOptionAcceptsValidValuesAndRejectsInvalidOnes(t *testing.T) {
	cases := []struct {
		key     string
		value   string
		wantErr bool
	}{
		{"asset_library_setting.enabled", "true", false},
		{"asset_library_setting.enabled", "yes", true},
		{"asset_library_setting.block_asset_refs_when_disabled", "false", false},
		{"asset_library_setting.max_groups_per_user", "0", false},
		{"asset_library_setting.max_assets_per_user", "-1", true},
		{"asset_library_setting.max_assets_per_user", "1000001", true},
		{"asset_library_setting.allowed_user_groups", `["default","vip"]`, false},
		{"asset_library_setting.allowed_user_groups", `["default",""]`, true},
		{"asset_library_setting.allowed_user_groups", `default`, true},
		{"asset_library_setting.compliance_notice", "I own these assets.", false},
		{"asset_library_setting.verify_sessions_per_hour", "5", true},
		{"asset_library_setting.unknown", "1", true},
		{"other_setting.enabled", "anything", false},
	}
	for _, tc := range cases {
		err := ValidateOption(tc.key, tc.value)
		assert.Equal(t, tc.wantErr, err != nil, "%s=%q: %v", tc.key, tc.value, err)
	}
}

func TestUserGroupAllowedTreatsAnEmptyListAsEveryGroup(t *testing.T) {
	assert.True(t, AssetLibrarySetting{}.UserGroupAllowed("default"))
	assert.True(t, AssetLibrarySetting{AllowedUserGroups: []string{"vip"}}.UserGroupAllowed("vip"))
	assert.False(t, AssetLibrarySetting{AllowedUserGroups: []string{"vip"}}.UserGroupAllowed("default"))
}
