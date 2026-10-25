package asset_library_setting

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// The asset library lets users keep their own Volcengine Ark private asset
// groups (virtual avatars and verified real people) behind the gateway's
// shared Volcengine accounts. While it is disabled the gateway behaves as if
// the feature did not exist, except that BlockAssetRefsWhenDisabled can still
// refuse asset:// references, which any key of the account's project could
// otherwise use.

// MaxPerUserLimit caps the per-user group and asset limits.
const MaxPerUserLimit = 1_000_000

type AssetLibrarySetting struct {
	Enabled bool `json:"enabled"`
	// Refuse requests carrying asset:// references while the library is off.
	BlockAssetRefsWhenDisabled bool `json:"block_asset_refs_when_disabled"`
	// Per-user limits; 0 = unlimited.
	MaxGroupsPerUser int `json:"max_groups_per_user"`
	MaxAssetsPerUser int `json:"max_assets_per_user"`
	// User groups that may use the library; empty = every group.
	AllowedUserGroups []string `json:"allowed_user_groups"`
	// Base URL the real-person verification page returns to; empty = the
	// server address.
	CallbackBaseURL string `json:"callback_base_url"`
	// Text shown before a user creates a group or starts a verification.
	ComplianceNotice string `json:"compliance_notice"`
	// Real-person verifications a user may start per hour; 0 = unlimited.
	VerifySessionsPerHour int `json:"verify_sessions_per_hour"`
}

var assetLibrarySetting = AssetLibrarySetting{
	Enabled:                    false,
	BlockAssetRefsWhenDisabled: true,
	MaxGroupsPerUser:           20,
	MaxAssetsPerUser:           200,
	AllowedUserGroups:          []string{},
	VerifySessionsPerHour:      5,
}

func init() {
	config.GlobalConfig.Register("asset_library_setting", &assetLibrarySetting)
}

func GetSetting() AssetLibrarySetting {
	return assetLibrarySetting
}

// UserGroupAllowed reports whether users of the group may use the library.
func (s AssetLibrarySetting) UserGroupAllowed(group string) bool {
	return len(s.AllowedUserGroups) == 0 || slices.Contains(s.AllowedUserGroups, group)
}

// ValidateOption checks an asset_library_setting.* option before it is saved.
func ValidateOption(key string, value string) error {
	field, ok := strings.CutPrefix(key, "asset_library_setting.")
	if !ok {
		return nil
	}
	switch field {
	case "enabled", "block_asset_refs_when_disabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s must be true or false", field)
		}
	case "max_groups_per_user", "max_assets_per_user", "verify_sessions_per_hour":
		number, err := strconv.Atoi(value)
		if err != nil || number < 0 || number > MaxPerUserLimit {
			return fmt.Errorf("%s must be an integer from 0 to %d", field, MaxPerUserLimit)
		}
	case "allowed_user_groups":
		var groups []string
		if err := common.UnmarshalJsonStr(value, &groups); err != nil {
			return fmt.Errorf("allowed_user_groups must be a JSON array of group names")
		}
		for _, group := range groups {
			if strings.TrimSpace(group) == "" {
				return fmt.Errorf("allowed_user_groups must not contain empty group names")
			}
		}
	case "callback_base_url":
		if value == "" {
			return nil
		}
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("callback_base_url must be an http(s) URL without a query")
		}
	case "compliance_notice":
		if len([]rune(value)) > 5000 {
			return fmt.Errorf("compliance_notice must be at most 5000 characters")
		}
	default:
		return fmt.Errorf("unknown asset library option %q", key)
	}
	return nil
}
