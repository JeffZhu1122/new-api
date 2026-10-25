package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserExtend stores per-user overrides outside the users table so the original
// schema stays untouched. A missing row means every override inherits the
// group/global configuration.
type UserExtend struct {
	UserId        int    `json:"user_id" gorm:"primaryKey"`
	RateLimit     string `json:"rate_limit" gorm:"type:text"`     // JSON dto.RateLimitOverride, "" = 无覆盖
	ModelDiscount string `json:"model_discount" gorm:"type:text"` // JSON map[模型名]折扣, "" = 无折扣
	AssetLibrary  string `json:"asset_library" gorm:"type:text"`  // JSON AssetLibraryLimits, "" = 继承全局
}

// AssetLibraryLimits overrides the asset library's global per-user limits for
// one user. A nil field inherits the global value, 0 means unlimited.
type AssetLibraryLimits struct {
	MaxGroups *int `json:"max_groups_per_user,omitempty"`
	MaxAssets *int `json:"max_assets_per_user,omitempty"`
}

func (UserExtend) TableName() string {
	return "user_extend"
}

func userRateLimitCacheKey(userId int) string {
	return fmt.Sprintf("user_extend_rl:%d", userId)
}

func userModelDiscountCacheKey(userId int) string {
	return fmt.Sprintf("user_extend_md:%d", userId)
}

func userAssetLibraryLimitsCacheKey(userId int) string {
	return fmt.Sprintf("user_extend_al:%d", userId)
}

// deleteEmptyUserExtend removes the row once every override column is cleared,
// so "inherit group/global" users keep no record. Clearing one column must
// never delete the row while another column still holds an override. A column
// added after a row was written is NULL in that row, which counts as empty.
func deleteEmptyUserExtend(userId int) error {
	return DB.Where("user_id = ? AND COALESCE(rate_limit, '') = '' AND COALESCE(model_discount, '') = '' AND COALESCE(asset_library, '') = ''", userId).Delete(&UserExtend{}).Error
}

// GetUserRateLimitOverride returns the per-user RPM/TPM override, or nil when
// the user has none. The cached value is always common.Marshal(override), so a
// stored "null" doubles as the negative cache for users without an override.
func GetUserRateLimitOverride(userId int) (*dto.RateLimitOverride, error) {
	if userId == 0 {
		return nil, errors.New("user id is required")
	}
	if common.RedisEnabled {
		if cached, err := common.RedisGet(userRateLimitCacheKey(userId)); err == nil {
			var override *dto.RateLimitOverride
			if err := common.UnmarshalJsonStr(cached, &override); err == nil {
				return override, nil
			}
		}
	}

	var extend UserExtend
	err := DB.Where("user_id = ?", userId).Take(&extend).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var override *dto.RateLimitOverride
	if extend.RateLimit != "" {
		if err := common.UnmarshalJsonStr(extend.RateLimit, &override); err != nil {
			return nil, err
		}
	}
	cacheUserRateLimitOverride(userId, override)
	return override, nil
}

func cacheUserRateLimitOverride(userId int, override *dto.RateLimitOverride) {
	if !common.RedisEnabled {
		return
	}
	data, err := common.Marshal(override)
	if err != nil {
		common.SysError("failed to marshal user rate limit override for cache: " + err.Error())
		return
	}
	if err := common.RedisSet(userRateLimitCacheKey(userId), string(data), time.Duration(common.RedisKeyCacheSeconds())*time.Second); err != nil {
		common.SysError("failed to cache user rate limit override: " + err.Error())
	}
}

// UpdateUserRateLimitOverride persists the per-user override. An override with
// no content clears the column; the row is deleted only when every override
// column is empty. The cache is written through, so other instances see the
// change immediately.
func UpdateUserRateLimitOverride(userId int, override *dto.RateLimitOverride) error {
	if userId == 0 {
		return errors.New("user id is required")
	}
	if override != nil && override.Default == nil && len(override.Models) == 0 {
		override = nil
	}
	if override == nil {
		if err := DB.Model(&UserExtend{}).Where("user_id = ?", userId).Update("rate_limit", "").Error; err != nil {
			return err
		}
		if err := deleteEmptyUserExtend(userId); err != nil {
			return err
		}
		cacheUserRateLimitOverride(userId, nil)
		return nil
	}
	data, err := common.Marshal(override)
	if err != nil {
		return err
	}
	extend := UserExtend{
		UserId:    userId,
		RateLimit: string(data),
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"rate_limit"}),
	}).Create(&extend).Error; err != nil {
		return err
	}
	cacheUserRateLimitOverride(userId, override)
	return nil
}

// GetUserModelDiscount returns the per-user {model: discount} overrides, or nil
// when the user has none. The cached value is always common.Marshal(discounts),
// so a stored "null" doubles as the negative cache for users without discounts.
func GetUserModelDiscount(userId int) (map[string]float64, error) {
	if userId == 0 {
		return nil, errors.New("user id is required")
	}
	if common.RedisEnabled {
		if cached, err := common.RedisGet(userModelDiscountCacheKey(userId)); err == nil {
			var discounts map[string]float64
			if err := common.UnmarshalJsonStr(cached, &discounts); err == nil {
				return discounts, nil
			}
		}
	}

	var extend UserExtend
	err := DB.Where("user_id = ?", userId).Take(&extend).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var discounts map[string]float64
	if extend.ModelDiscount != "" {
		if err := common.UnmarshalJsonStr(extend.ModelDiscount, &discounts); err != nil {
			return nil, err
		}
	}
	cacheUserModelDiscount(userId, discounts)
	return discounts, nil
}

func cacheUserModelDiscount(userId int, discounts map[string]float64) {
	if !common.RedisEnabled {
		return
	}
	data, err := common.Marshal(discounts)
	if err != nil {
		common.SysError("failed to marshal user model discount for cache: " + err.Error())
		return
	}
	if err := common.RedisSet(userModelDiscountCacheKey(userId), string(data), time.Duration(common.RedisKeyCacheSeconds())*time.Second); err != nil {
		common.SysError("failed to cache user model discount: " + err.Error())
	}
}

// UpdateUserModelDiscount persists the per-user model discounts. An empty map
// clears the column; the row is deleted only when every override column is
// empty. The cache is written through, so other instances see the change
// immediately.
func UpdateUserModelDiscount(userId int, discounts map[string]float64) error {
	if userId == 0 {
		return errors.New("user id is required")
	}
	if len(discounts) == 0 {
		if err := DB.Model(&UserExtend{}).Where("user_id = ?", userId).Update("model_discount", "").Error; err != nil {
			return err
		}
		if err := deleteEmptyUserExtend(userId); err != nil {
			return err
		}
		cacheUserModelDiscount(userId, nil)
		return nil
	}
	data, err := common.Marshal(discounts)
	if err != nil {
		return err
	}
	extend := UserExtend{
		UserId:        userId,
		ModelDiscount: string(data),
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"model_discount"}),
	}).Create(&extend).Error; err != nil {
		return err
	}
	cacheUserModelDiscount(userId, discounts)
	return nil
}

// GetUserAssetLibraryLimits returns the user's asset library limit override,
// or nil when the user has none. The cached value is always
// common.Marshal(limits), so a stored "null" doubles as the negative cache.
func GetUserAssetLibraryLimits(userId int) (*AssetLibraryLimits, error) {
	if userId == 0 {
		return nil, errors.New("user id is required")
	}
	if common.RedisEnabled {
		if cached, err := common.RedisGet(userAssetLibraryLimitsCacheKey(userId)); err == nil {
			var limits *AssetLibraryLimits
			if err := common.UnmarshalJsonStr(cached, &limits); err == nil {
				return limits, nil
			}
		}
	}

	var extend UserExtend
	err := DB.Where("user_id = ?", userId).Take(&extend).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var limits *AssetLibraryLimits
	if extend.AssetLibrary != "" {
		if err := common.UnmarshalJsonStr(extend.AssetLibrary, &limits); err != nil {
			return nil, err
		}
	}
	cacheUserAssetLibraryLimits(userId, limits)
	return limits, nil
}

func cacheUserAssetLibraryLimits(userId int, limits *AssetLibraryLimits) {
	if !common.RedisEnabled {
		return
	}
	data, err := common.Marshal(limits)
	if err != nil {
		common.SysError("failed to marshal user asset library limits for cache: " + err.Error())
		return
	}
	if err := common.RedisSet(userAssetLibraryLimitsCacheKey(userId), string(data), time.Duration(common.RedisKeyCacheSeconds())*time.Second); err != nil {
		common.SysError("failed to cache user asset library limits: " + err.Error())
	}
}

// UpdateUserAssetLibraryLimits persists the user's asset library limit
// override. An override with neither limit clears the column; the row is
// deleted only when every override column is empty. The cache is written
// through, so other instances see the change immediately.
func UpdateUserAssetLibraryLimits(userId int, limits *AssetLibraryLimits) error {
	if userId == 0 {
		return errors.New("user id is required")
	}
	if limits != nil && limits.MaxGroups == nil && limits.MaxAssets == nil {
		limits = nil
	}
	if limits == nil {
		if err := DB.Model(&UserExtend{}).Where("user_id = ?", userId).Update("asset_library", "").Error; err != nil {
			return err
		}
		if err := deleteEmptyUserExtend(userId); err != nil {
			return err
		}
		cacheUserAssetLibraryLimits(userId, nil)
		return nil
	}
	data, err := common.Marshal(limits)
	if err != nil {
		return err
	}
	extend := UserExtend{
		UserId:       userId,
		AssetLibrary: string(data),
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"asset_library"}),
	}).Create(&extend).Error; err != nil {
		return err
	}
	cacheUserAssetLibraryLimits(userId, limits)
	return nil
}
