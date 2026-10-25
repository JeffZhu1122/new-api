package model

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// AssetLibraryAccount is a Volcengine account (Access Key pair and project)
// whose Ark private asset library the gateway shares out to its users. Asset
// groups always live in exactly one account, and only channels whose API key
// belongs to the same account and project can use them.
type AssetLibraryAccount struct {
	Id              int    `json:"id"`
	Name            string `json:"name" gorm:"type:varchar(64);not null"`
	AccessKeyId     string `json:"-" gorm:"type:varchar(128);not null"`
	SecretAccessKey string `json:"-" gorm:"type:varchar(256);not null"`
	Region          string `json:"region" gorm:"type:varchar(32);not null"`
	ProjectName     string `json:"project_name" gorm:"type:varchar(64);not null"`
	// Overrides https://ark.<region>.volcengineapi.com; empty = public endpoint.
	Endpoint string `json:"endpoint" gorm:"type:varchar(255)"`
	Enabled  bool   `json:"enabled"`
	// Group types users may create in this account.
	AigcEnabled         bool `json:"aigc_enabled"`
	LivenessFaceEnabled bool `json:"liveness_face_enabled"`
	// Real-person groups come from face verification
	// (CreateVisualValidateSession) instead of CreateAssetGroup, for accounts
	// Ark has not allowlisted for direct creation.
	LivenessFaceVerify bool `json:"liveness_face_verify"`
	// JSON array of user groups allowed to create groups here; "" or "[]" = all.
	AllowedUserGroups string `json:"-" gorm:"type:text"`
	// New groups go to the highest-priority account with room.
	Priority int `json:"priority" gorm:"default:0"`
	// Account-wide quotas of the subscribed tier; 0 = unlimited.
	MaxGroups int `json:"max_groups" gorm:"default:0"`
	MaxAssets int `json:"max_assets" gorm:"default:0"`
	// CreateAsset calls per minute the tier allows; 0 = unlimited.
	CreateAssetQPM int   `json:"create_asset_qpm" gorm:"default:0"`
	CreatedTime    int64 `json:"created_time" gorm:"bigint"`
	UpdatedTime    int64 `json:"updated_time" gorm:"bigint"`
}

func (AssetLibraryAccount) TableName() string {
	return "asset_library_accounts"
}

// AllowedUserGroupList decodes AllowedUserGroups; empty means every group.
func (a *AssetLibraryAccount) AllowedUserGroupList() []string {
	var groups []string
	if strings.TrimSpace(a.AllowedUserGroups) == "" {
		return []string{}
	}
	if err := common.UnmarshalJsonStr(a.AllowedUserGroups, &groups); err != nil {
		common.SysError(common.LogText("asset library account %d has invalid allowed user groups: %s", a.Id, err.Error()))
		return []string{}
	}
	return groups
}

// UserGroupAllowed reports whether users of the group may create groups here.
func (a *AssetLibraryAccount) UserGroupAllowed(group string) bool {
	groups := a.AllowedUserGroupList()
	return len(groups) == 0 || slices.Contains(groups, group)
}

func ListAssetLibraryAccounts() ([]*AssetLibraryAccount, error) {
	var accounts []*AssetLibraryAccount
	err := DB.Order("priority desc").Order("id asc").Find(&accounts).Error
	return accounts, err
}

func GetAssetLibraryAccount(id int) (*AssetLibraryAccount, error) {
	var account AssetLibraryAccount
	if err := DB.First(&account, id).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func CreateAssetLibraryAccount(account *AssetLibraryAccount) error {
	now := common.GetTimestamp()
	account.Id = 0
	account.CreatedTime = now
	account.UpdatedTime = now
	return DB.Create(account).Error
}

// UpdateAssetLibraryAccount saves every editable field, including false and
// zero values.
func UpdateAssetLibraryAccount(account *AssetLibraryAccount) error {
	account.UpdatedTime = common.GetTimestamp()
	return DB.Model(&AssetLibraryAccount{}).Where("id = ?", account.Id).Select(
		"name", "access_key_id", "secret_access_key", "region", "project_name", "endpoint",
		"enabled", "aigc_enabled", "liveness_face_enabled", "liveness_face_verify", "allowed_user_groups", "priority",
		"max_groups", "max_assets", "create_asset_qpm", "updated_time",
	).Updates(account).Error
}

func DeleteAssetLibraryAccount(id int) error {
	return DB.Delete(&AssetLibraryAccount{}, id).Error
}
