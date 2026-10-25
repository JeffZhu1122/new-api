package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"gorm.io/gorm"
)

// AssetLibraryVerification is a face verification a user started for a
// real-person group on an account Ark has not allowlisted for direct
// creation. Ark creates the group once the person passes; the gateway then
// records the group under the user who started the verification. BytedToken
// is kept only to ask Ark for that group and is never shown.
type AssetLibraryVerification struct {
	Id         int    `json:"id"`
	UserId     int    `json:"user_id" gorm:"index"`
	AccountId  int    `json:"account_id" gorm:"index"`
	BytedToken string `json:"-" gorm:"type:varchar(128);uniqueIndex"`
	TaskId     string `json:"task_id" gorm:"type:varchar(64)"`
	// Name and description the group gets once Ark has created it.
	Name        string `json:"name" gorm:"type:varchar(64)"`
	Description string `json:"description" gorm:"type:varchar(300)"`
	// pending, succeeded or failed.
	Status          string `json:"status" gorm:"type:varchar(16);index"`
	RemoteGroupId   string `json:"remote_group_id" gorm:"type:varchar(64)"`
	FailReason      string `json:"fail_reason" gorm:"type:varchar(255)"`
	LastCheckedTime int64  `json:"last_checked_time" gorm:"bigint"`
	CreatedTime     int64  `json:"created_time" gorm:"bigint;index"`
	UpdatedTime     int64  `json:"updated_time" gorm:"bigint"`
}

func (AssetLibraryVerification) TableName() string {
	return "asset_library_verifications"
}

const (
	AssetLibraryVerificationPending   = "pending"
	AssetLibraryVerificationSucceeded = "succeeded"
	AssetLibraryVerificationFailed    = "failed"
)

// AssetLibraryVerifyAction is the task action of a face verification.
const AssetLibraryVerifyAction = "CreateVisualValidateSession"

// CreateAssetLibraryVerification records a new verification and its task
// together.
func CreateAssetLibraryVerification(verification *AssetLibraryVerification, userGroup string) error {
	now := common.GetTimestamp()
	verification.Status = AssetLibraryVerificationPending
	verification.TaskId = "verify-" + common.GetUUID()
	verification.LastCheckedTime, verification.CreatedTime, verification.UpdatedTime = now, now, now
	task := &Task{
		TaskID:     verification.TaskId,
		Platform:   constant.TaskPlatformAssetLibrary,
		UserId:     verification.UserId,
		Group:      userGroup,
		Action:     AssetLibraryVerifyAction,
		Status:     TaskStatusInProgress,
		Progress:   "10%",
		SubmitTime: now,
		StartTime:  now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	task.SetData(assetLibraryVerificationTaskData(verification, ""))
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(verification).Error; err != nil {
			return err
		}
		return tx.Create(task).Error
	})
}

func assetLibraryVerificationTaskData(verification *AssetLibraryVerification, resultCode string) map[string]any {
	data := map[string]any{"Name": verification.Name, "Status": verification.Status}
	if verification.RemoteGroupId != "" {
		data["GroupId"] = verification.RemoteGroupId
	}
	if resultCode != "" {
		data["ResultCode"] = resultCode
	}
	return data
}

// GetUserAssetLibraryVerification returns the user's verification with the
// token; another user's verification reads as not found.
func GetUserAssetLibraryVerification(userId int, bytedToken string) (*AssetLibraryVerification, error) {
	var verification AssetLibraryVerification
	err := DB.Where("user_id = ? AND byted_token = ?", userId, bytedToken).First(&verification).Error
	if err != nil {
		return nil, err
	}
	return &verification, nil
}

func GetAssetLibraryVerificationByToken(bytedToken string) (*AssetLibraryVerification, error) {
	var verification AssetLibraryVerification
	err := DB.Where("byted_token = ?", bytedToken).First(&verification).Error
	if err != nil {
		return nil, err
	}
	return &verification, nil
}

// CountAssetLibraryVerificationsSince counts the verifications the user
// started at or after since, whatever became of them.
func CountAssetLibraryVerificationsSince(userId int, since int64) (int64, error) {
	var count int64
	err := DB.Model(&AssetLibraryVerification{}).Where("user_id = ? AND created_time >= ?", userId, since).Count(&count).Error
	return count, err
}

// CountPendingAssetLibraryVerifications counts the pending verifications of a
// user or account started at or after since. Each may still become a group,
// so it holds a place against the group limits.
func CountPendingAssetLibraryVerifications(column string, id int, since int64) (int64, error) {
	var count int64
	err := DB.Model(&AssetLibraryVerification{}).
		Where(column+" = ? AND status = ? AND created_time >= ?", id, AssetLibraryVerificationPending, since).
		Count(&count).Error
	return count, err
}

// ListPendingAssetLibraryVerifications returns pending verifications last
// checked before checkedBefore, least recently checked first.
func ListPendingAssetLibraryVerifications(checkedBefore int64, limit int) ([]*AssetLibraryVerification, error) {
	var verifications []*AssetLibraryVerification
	err := DB.Where("status = ? AND last_checked_time < ?", AssetLibraryVerificationPending, checkedBefore).
		Order("last_checked_time asc").Limit(limit).Find(&verifications).Error
	return verifications, err
}

// TouchAssetLibraryVerification notes a check that found no group yet.
func TouchAssetLibraryVerification(verification *AssetLibraryVerification) error {
	verification.LastCheckedTime = common.GetTimestamp()
	return DB.Model(&AssetLibraryVerification{}).Where("id = ?", verification.Id).
		Update("last_checked_time", verification.LastCheckedTime).Error
}

// CompleteAssetLibraryVerification records the group Ark created for the
// verification under its user and finishes the task. It reports false when
// the verification was no longer pending, for example because another node
// recorded the group first.
func CompleteAssetLibraryVerification(verification *AssetLibraryVerification, group *AssetLibraryGroup) (bool, error) {
	now := common.GetTimestamp()
	completed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&AssetLibraryVerification{}).
			Where("id = ? AND status = ?", verification.Id, AssetLibraryVerificationPending).
			Updates(map[string]any{
				"status":            AssetLibraryVerificationSucceeded,
				"remote_group_id":   group.RemoteId,
				"last_checked_time": now,
				"updated_time":      now,
			})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		group.CreatedTime, group.UpdatedTime = now, now
		if err := tx.Create(group).Error; err != nil {
			return err
		}
		verification.Status, verification.RemoteGroupId = AssetLibraryVerificationSucceeded, group.RemoteId
		completed = true
		return finishAssetLibraryVerificationTask(tx, verification, "")
	})
	return completed, err
}

// FailAssetLibraryVerification ends a pending verification without a group.
// reason is a fixed English sentence the frontend translates; resultCode is
// the code the verification page reported, if any.
func FailAssetLibraryVerification(verification *AssetLibraryVerification, reason, resultCode string) (bool, error) {
	now := common.GetTimestamp()
	failed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&AssetLibraryVerification{}).
			Where("id = ? AND status = ?", verification.Id, AssetLibraryVerificationPending).
			Updates(map[string]any{
				"status":            AssetLibraryVerificationFailed,
				"fail_reason":       reason,
				"last_checked_time": now,
				"updated_time":      now,
			})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		verification.Status, verification.FailReason = AssetLibraryVerificationFailed, reason
		failed = true
		return finishAssetLibraryVerificationTask(tx, verification, resultCode)
	})
	return failed, err
}

func finishAssetLibraryVerificationTask(tx *gorm.DB, verification *AssetLibraryVerification, resultCode string) error {
	var task Task
	err := tx.Where("user_id = ? AND platform = ? AND task_id = ?", verification.UserId, constant.TaskPlatformAssetLibrary, verification.TaskId).First(&task).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	previous := task.Status
	now := common.GetTimestamp()
	task.Progress = "100%"
	task.FinishTime = now
	task.UpdatedAt = now
	task.Status = TaskStatusSuccess
	task.FailReason = ""
	if verification.Status == AssetLibraryVerificationFailed {
		task.Status = TaskStatusFailure
		task.FailReason = verification.FailReason
	}
	task.SetData(assetLibraryVerificationTaskData(verification, resultCode))
	return tx.Model(&task).Where("status = ?", previous).Select("*").Updates(&task).Error
}
