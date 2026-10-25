package model

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"gorm.io/gorm"
)

// AssetLibraryGroup records which user owns an Ark asset group. Ark itself has
// no notion of the gateway's users, so every request is checked against these
// rows before it reaches the group's account.
type AssetLibraryGroup struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"index"`
	AccountId   int    `json:"account_id" gorm:"index"`
	RemoteId    string `json:"remote_id" gorm:"type:varchar(64);uniqueIndex"`
	GroupType   string `json:"group_type" gorm:"type:varchar(16);index"`
	Name        string `json:"name" gorm:"type:varchar(64)"`
	Description string `json:"description" gorm:"type:varchar(300)"`
	// Ark's RFC 3339 timestamps, returned as Ark returns them.
	RemoteCreateTime string `json:"remote_create_time" gorm:"type:varchar(40)"`
	RemoteUpdateTime string `json:"remote_update_time" gorm:"type:varchar(40)"`
	CreatedTime      int64  `json:"created_time" gorm:"bigint;index"`
	UpdatedTime      int64  `json:"updated_time" gorm:"bigint"`
}

func (AssetLibraryGroup) TableName() string {
	return "asset_library_groups"
}

// AssetLibraryAsset records which user owns an Ark asset and mirrors what Ark
// last reported about it, so lists can be answered per user across accounts.
type AssetLibraryAsset struct {
	Id            int    `json:"id"`
	UserId        int    `json:"user_id" gorm:"index"`
	AccountId     int    `json:"account_id" gorm:"index"`
	GroupId       int    `json:"group_id" gorm:"index"`
	RemoteGroupId string `json:"remote_group_id" gorm:"type:varchar(64);index"`
	RemoteId      string `json:"remote_id" gorm:"type:varchar(64);uniqueIndex"`
	AssetType     string `json:"asset_type" gorm:"type:varchar(16)"`
	Name          string `json:"name" gorm:"type:varchar(64)"`
	// Processing, Active or Failed, as Ark reports it.
	Status       string `json:"status" gorm:"type:varchar(16);index"`
	ErrorCode    string `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage string `json:"error_message" gorm:"type:text"`
	// Signed download URL from Ark and when it should be fetched again.
	URL               string `json:"-" gorm:"type:text"`
	URLRefreshAfter   int64  `json:"-" gorm:"bigint"`
	LastInferenceTime string `json:"last_inference_time" gorm:"type:varchar(40)"`
	RemoteCreateTime  string `json:"remote_create_time" gorm:"type:varchar(40)"`
	RemoteUpdateTime  string `json:"remote_update_time" gorm:"type:varchar(40)"`
	CreatedTime       int64  `json:"created_time" gorm:"bigint;index"`
	UpdatedTime       int64  `json:"updated_time" gorm:"bigint"`
}

func (AssetLibraryAsset) TableName() string {
	return "asset_library_assets"
}

const (
	AssetLibraryAssetProcessing = "Processing"
	AssetLibraryAssetActive     = "Active"
	AssetLibraryAssetFailed     = "Failed"
)

// AssetLibraryUploadAction is the task action of an asset upload.
const AssetLibraryUploadAction = "CreateAsset"

func CountAssetLibraryGroups(column string, id int) (int64, error) {
	var count int64
	err := DB.Model(&AssetLibraryGroup{}).Where(column+" = ?", id).Count(&count).Error
	return count, err
}

func CountAssetLibraryAssets(column string, id int) (int64, error) {
	var count int64
	err := DB.Model(&AssetLibraryAsset{}).Where(column+" = ?", id).Count(&count).Error
	return count, err
}

// GetUserAssetLibraryGroup returns the user's group with the Ark id; another
// user's group reads as not found.
func GetUserAssetLibraryGroup(userId int, remoteId string) (*AssetLibraryGroup, error) {
	var group AssetLibraryGroup
	err := DB.Where("user_id = ? AND remote_id = ?", userId, remoteId).First(&group).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// GetUserAssetLibraryAsset returns the user's asset with the Ark id; another
// user's asset reads as not found.
func GetUserAssetLibraryAsset(userId int, remoteId string) (*AssetLibraryAsset, error) {
	var asset AssetLibraryAsset
	err := DB.Where("user_id = ? AND remote_id = ?", userId, remoteId).First(&asset).Error
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func CreateAssetLibraryGroup(group *AssetLibraryGroup) error {
	now := common.GetTimestamp()
	group.CreatedTime, group.UpdatedTime = now, now
	return DB.Create(group).Error
}

func UpdateAssetLibraryGroup(group *AssetLibraryGroup) error {
	group.UpdatedTime = common.GetTimestamp()
	return DB.Model(&AssetLibraryGroup{}).Where("id = ?", group.Id).
		Select("name", "description", "remote_create_time", "remote_update_time", "updated_time").
		Updates(group).Error
}

// DeleteAssetLibraryGroup removes the group and the assets recorded in it.
func DeleteAssetLibraryGroup(group *AssetLibraryGroup) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", group.Id).Delete(&AssetLibraryAsset{}).Error; err != nil {
			return err
		}
		return tx.Delete(&AssetLibraryGroup{}, group.Id).Error
	})
}

// CreateAssetLibraryUpload records a new asset and its upload task together.
func CreateAssetLibraryUpload(asset *AssetLibraryAsset, userGroup string) error {
	now := common.GetTimestamp()
	asset.CreatedTime, asset.UpdatedTime = now, now
	task := &Task{
		TaskID:     asset.RemoteId,
		Platform:   constant.TaskPlatformAssetLibrary,
		UserId:     asset.UserId,
		Group:      userGroup,
		Action:     AssetLibraryUploadAction,
		Status:     TaskStatusInProgress,
		Progress:   "10%",
		SubmitTime: now,
		StartTime:  now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	task.SetData(assetLibraryUploadTaskData(asset))
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(asset).Error; err != nil {
			return err
		}
		return tx.Create(task).Error
	})
}

func assetLibraryUploadTaskData(asset *AssetLibraryAsset) map[string]any {
	data := map[string]any{
		"Id":        asset.RemoteId,
		"GroupId":   asset.RemoteGroupId,
		"AssetType": asset.AssetType,
		"Name":      asset.Name,
		"Status":    asset.Status,
	}
	if asset.ErrorCode != "" || asset.ErrorMessage != "" {
		data["Error"] = map[string]any{"Code": asset.ErrorCode, "Message": asset.ErrorMessage}
	}
	return data
}

// SaveAssetLibraryAsset stores what Ark reported about the asset. When the
// status leaves Processing, the upload task finishes with it.
func SaveAssetLibraryAsset(asset *AssetLibraryAsset, previousStatus string) error {
	asset.UpdatedTime = common.GetTimestamp()
	err := DB.Model(&AssetLibraryAsset{}).Where("id = ?", asset.Id).Select(
		"name", "status", "error_code", "error_message", "url", "url_refresh_after",
		"last_inference_time", "remote_create_time", "remote_update_time", "updated_time",
	).Updates(asset).Error
	if err != nil || previousStatus != AssetLibraryAssetProcessing || asset.Status == AssetLibraryAssetProcessing {
		return err
	}
	return finishAssetLibraryUploadTask(asset)
}

func finishAssetLibraryUploadTask(asset *AssetLibraryAsset) error {
	var task Task
	err := DB.Where("user_id = ? AND platform = ? AND task_id = ?", asset.UserId, constant.TaskPlatformAssetLibrary, asset.RemoteId).First(&task).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return err
	}
	if task.Status == TaskStatusSuccess || task.Status == TaskStatusFailure {
		return nil
	}
	previous := task.Status
	now := time.Now().Unix()
	task.Progress = "100%"
	task.FinishTime = now
	task.UpdatedAt = now
	if asset.Status == AssetLibraryAssetActive {
		task.Status = TaskStatusSuccess
		task.FailReason = ""
	} else {
		task.Status = TaskStatusFailure
		task.FailReason = strings.TrimSpace(strings.Trim(asset.ErrorCode+": "+asset.ErrorMessage, ": "))
		if task.FailReason == "" {
			task.FailReason = "Asset processing failed"
		}
	}
	task.SetData(assetLibraryUploadTaskData(asset))
	_, err = task.UpdateWithStatus(previous)
	return err
}

func DeleteAssetLibraryAsset(asset *AssetLibraryAsset) error {
	return DB.Delete(&AssetLibraryAsset{}, asset.Id).Error
}

// AssetLibraryListFilter narrows a user's groups or assets the way Ark's list
// APIs do.
type AssetLibraryListFilter struct {
	UserId    int
	GroupType string
	// Ark group ids; nil = any group.
	GroupIds []string
	// Fuzzy name match; "" = any name.
	Name string
	// Asset statuses; nil = any status.
	Statuses []string
	// CreateTime, UpdateTime or GroupId.
	SortBy    string
	Ascending bool
	Offset    int
	Limit     int
}

// assetLibraryNamePattern matches names containing value. "!" is the escape
// character, which needs no string escaping on MySQL, PostgreSQL or SQLite.
func assetLibraryNamePattern(value string) string {
	return "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value) + "%"
}

func (f AssetLibraryListFilter) order(groupIdColumn string) string {
	column := "created_time"
	switch f.SortBy {
	case "UpdateTime":
		column = "updated_time"
	case "GroupId":
		column = groupIdColumn
	}
	direction := " DESC"
	if f.Ascending {
		direction = " ASC"
	}
	return column + direction + ", id" + direction
}

// ListAssetLibraryGroups returns one page of the user's groups and the total.
func ListAssetLibraryGroups(filter AssetLibraryListFilter) ([]*AssetLibraryGroup, int64, error) {
	query := DB.Model(&AssetLibraryGroup{}).Where("user_id = ? AND group_type = ?", filter.UserId, filter.GroupType)
	if filter.GroupIds != nil {
		query = query.Where("remote_id IN ?", filter.GroupIds)
	}
	if filter.Name != "" {
		query = query.Where("name LIKE ? ESCAPE '!'", assetLibraryNamePattern(filter.Name))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var groups []*AssetLibraryGroup
	err := query.Order(filter.order("remote_id")).Offset(filter.Offset).Limit(filter.Limit).Find(&groups).Error
	return groups, total, err
}

// ListAssetLibraryAssets returns one page of the user's assets in groups of
// the filter's type, and the total.
func ListAssetLibraryAssets(filter AssetLibraryListFilter) ([]*AssetLibraryAsset, int64, error) {
	groupIds := DB.Model(&AssetLibraryGroup{}).Select("id").Where("user_id = ? AND group_type = ?", filter.UserId, filter.GroupType)
	query := DB.Model(&AssetLibraryAsset{}).Where("user_id = ? AND group_id IN (?)", filter.UserId, groupIds)
	if filter.GroupIds != nil {
		query = query.Where("remote_group_id IN ?", filter.GroupIds)
	}
	if filter.Name != "" {
		query = query.Where("name LIKE ? ESCAPE '!'", assetLibraryNamePattern(filter.Name))
	}
	if filter.Statuses != nil {
		query = query.Where("status IN ?", filter.Statuses)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var assets []*AssetLibraryAsset
	err := query.Order(filter.order("remote_group_id")).Offset(filter.Offset).Limit(filter.Limit).Find(&assets).Error
	return assets, total, err
}

// ListProcessingAssetLibraryAssets returns assets Ark is still processing,
// oldest first, for the background status sync.
func ListProcessingAssetLibraryAssets(limit int) ([]*AssetLibraryAsset, error) {
	var assets []*AssetLibraryAsset
	err := DB.Where("status = ?", AssetLibraryAssetProcessing).Order("updated_time ASC").Limit(limit).Find(&assets).Error
	return assets, err
}
