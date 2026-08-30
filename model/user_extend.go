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

// deleteEmptyUserExtend removes the row once every override column is cleared,
// so "inherit group/global" users keep no record. Clearing one column must
// never delete the row while another column still holds an override.
func deleteEmptyUserExtend(userId int) error {
	return DB.Where("user_id = ? AND rate_limit = '' AND model_discount = ''", userId).Delete(&UserExtend{}).Error
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
