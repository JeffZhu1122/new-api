package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/volcark"
	"github.com/QuantumNous/new-api/setting/asset_library_setting"
)

// AssetLibraryUserConfig is what the Asset Library page shows a user about
// their own library.
type AssetLibraryUserConfig struct {
	Allowed          bool   `json:"allowed"`
	ComplianceNotice string `json:"compliance_notice"`
	MaxGroups        int    `json:"max_groups_per_user"`
	MaxAssets        int    `json:"max_assets_per_user"`
	GroupCount       int64  `json:"group_count"`
	AssetCount       int64  `json:"asset_count"`
	// Group types an enabled account accepts from the user's group.
	GroupTypes []string `json:"group_types"`
	// Real-person groups can only be created through face verification: no
	// account that takes them from the user's group is allowlisted.
	LivenessFaceVerify    bool `json:"liveness_face_verify"`
	VerifySessionsPerHour int  `json:"verify_sessions_per_hour"`
}

func GetAssetLibraryUserConfig(userId int, userGroup string) (*AssetLibraryUserConfig, error) {
	setting := asset_library_setting.GetSetting()
	config := &AssetLibraryUserConfig{
		Allowed:          setting.UserGroupAllowed(userGroup),
		ComplianceNotice: setting.ComplianceNotice,
		GroupTypes:       []string{},

		VerifySessionsPerHour: setting.VerifySessionsPerHour,
	}
	var err error
	if config.MaxGroups, config.MaxAssets, err = assetLibraryUserLimits(userId); err != nil {
		return nil, err
	}
	if config.GroupCount, err = model.CountAssetLibraryGroups("user_id", userId); err != nil {
		return nil, err
	}
	if config.AssetCount, err = model.CountAssetLibraryAssets("user_id", userId); err != nil {
		return nil, err
	}
	accounts, err := model.ListAssetLibraryAccounts()
	if err != nil {
		return nil, err
	}
	aigc, peopleDirect, peopleVerified := false, false, false
	for _, account := range accounts {
		if account.Enabled && account.UserGroupAllowed(userGroup) {
			aigc = aigc || accountTakesGroups(account, volcark.GroupTypeAIGC, false)
			peopleDirect = peopleDirect || accountTakesGroups(account, volcark.GroupTypeLivenessFace, false)
			peopleVerified = peopleVerified || accountTakesGroups(account, volcark.GroupTypeLivenessFace, true)
		}
	}
	if aigc {
		config.GroupTypes = append(config.GroupTypes, volcark.GroupTypeAIGC)
	}
	if peopleDirect || peopleVerified {
		config.GroupTypes = append(config.GroupTypes, volcark.GroupTypeLivenessFace)
	}
	config.LivenessFaceVerify = peopleVerified && !peopleDirect
	return config, nil
}

// AssetLibraryAdminGroup is a group in the administrators' view.
type AssetLibraryAdminGroup struct {
	Id          string `json:"Id"`
	Name        string `json:"Name"`
	Description string `json:"Description"`
	GroupType   string `json:"GroupType"`
	CreateTime  string `json:"CreateTime"`
	UserId      int    `json:"UserId"`
	Username    string `json:"Username"`
	AccountName string `json:"AccountName"`
	AssetCount  int64  `json:"AssetCount"`
}

// ListAssetLibraryGroupsForAdmin returns one page of every user's groups.
func ListAssetLibraryGroupsForAdmin(filter model.AssetLibraryAdminFilter) ([]AssetLibraryAdminGroup, int64, error) {
	groups, total, err := model.ListAllAssetLibraryGroups(filter)
	if err != nil {
		return nil, 0, err
	}
	groupIds := make([]int, 0, len(groups))
	userIds := make([]int, 0, len(groups))
	for _, group := range groups {
		groupIds = append(groupIds, group.Id)
		userIds = append(userIds, group.UserId)
	}
	counts, err := model.CountAssetLibraryAssetsByGroup(groupIds)
	if err != nil {
		return nil, 0, err
	}
	usernames, err := model.GetUsernamesByIds(userIds)
	if err != nil {
		return nil, 0, err
	}
	accounts, err := model.ListAssetLibraryAccounts()
	if err != nil {
		return nil, 0, err
	}
	accountNames := make(map[int]string, len(accounts))
	for _, account := range accounts {
		accountNames[account.Id] = account.Name
	}
	items := make([]AssetLibraryAdminGroup, 0, len(groups))
	for _, group := range groups {
		items = append(items, AssetLibraryAdminGroup{
			Id:          group.RemoteId,
			Name:        group.Name,
			Description: group.Description,
			GroupType:   group.GroupType,
			CreateTime:  group.RemoteCreateTime,
			UserId:      group.UserId,
			Username:    usernames[group.UserId],
			AccountName: accountNames[group.AccountId],
			AssetCount:  counts[group.Id],
		})
	}
	return items, total, nil
}

// ErrAssetLibraryNotFound reports a group or asset the gateway has no record of.
var ErrAssetLibraryNotFound = errors.New("asset library resource not found")

// ListAssetLibraryGroupAssetsForAdmin returns a group's assets in the shape
// of Ark's ListAssets items, refreshing processing or expiring ones first.
func ListAssetLibraryGroupAssetsForAdmin(ctx context.Context, remoteGroupId string) ([]map[string]any, error) {
	group, err := model.GetAssetLibraryGroupByRemoteId(remoteGroupId)
	if err != nil {
		return nil, ErrAssetLibraryNotFound
	}
	assets, err := model.ListAssetLibraryAssetsInGroup(group.Id)
	if err != nil {
		return nil, err
	}
	refreshAssetLibraryAssets(ctx, assets)
	projects, err := accountProjects()
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(assets))
	for _, asset := range assets {
		items = append(items, assetLibraryAssetItem(asset, projects))
	}
	return items, nil
}

// DeleteAssetLibraryGroupForAdmin deletes any user's group on Ark and in the
// gateway's records.
func DeleteAssetLibraryGroupForAdmin(ctx context.Context, remoteGroupId string) error {
	group, err := model.GetAssetLibraryGroupByRemoteId(remoteGroupId)
	if err != nil {
		return ErrAssetLibraryNotFound
	}
	account, err := model.GetAssetLibraryAccount(group.AccountId)
	if err != nil {
		return err
	}
	if err := AssetLibraryClient(account).Call(ctx, "DeleteAssetGroup", map[string]any{"Id": group.RemoteId, "ProjectName": account.ProjectName}, nil); err != nil {
		return err
	}
	return model.DeleteAssetLibraryGroup(group)
}

// DeleteAssetLibraryAssetForAdmin deletes any user's asset on Ark and in the
// gateway's records.
func DeleteAssetLibraryAssetForAdmin(ctx context.Context, remoteAssetId string) error {
	asset, err := model.GetAssetLibraryAssetByRemoteId(remoteAssetId)
	if err != nil {
		return ErrAssetLibraryNotFound
	}
	account, err := model.GetAssetLibraryAccount(asset.AccountId)
	if err != nil {
		return err
	}
	if err := AssetLibraryClient(account).Call(ctx, "DeleteAsset", map[string]any{"Id": asset.RemoteId, "ProjectName": account.ProjectName}, nil); err != nil {
		return err
	}
	return model.DeleteAssetLibraryAsset(asset)
}
