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
	UserId    int    `json:"user_id" gorm:"primaryKey"`
	RateLimit string `json:"rate_limit" gorm:"type:text"` // JSON dto.RateLimitOverride, "" = 无覆盖
}

func (UserExtend) TableName() string {
	return "user_extend"
}

func userRateLimitCacheKey(userId int) string {
	return fmt.Sprintf("user_extend_rl:%d", userId)
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
// no content deletes the row so "inherit group/global" users keep no record.
// The cache is written through, so other instances see the change immediately.
func UpdateUserRateLimitOverride(userId int, override *dto.RateLimitOverride) error {
	if userId == 0 {
		return errors.New("user id is required")
	}
	if override != nil && override.Default == nil && len(override.Models) == 0 {
		override = nil
	}
	if override == nil {
		if err := DB.Where("user_id = ?", userId).Delete(&UserExtend{}).Error; err != nil {
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
