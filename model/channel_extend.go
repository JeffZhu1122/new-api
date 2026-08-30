package model

import (
	"errors"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelExtend stores per-channel overrides outside the channels table so the
// original schema stays untouched. A missing row means every override inherits
// the global configuration.
type ChannelExtend struct {
	ChannelId        int `json:"channel_id" gorm:"primaryKey"`
	RelayTimeout     int `json:"relay_timeout" gorm:"default:0"`     // seconds, 0 = global RELAY_TIMEOUT
	StreamingTimeout int `json:"streaming_timeout" gorm:"default:0"` // seconds, 0 = global STREAMING_TIMEOUT
	MinInputTokens   int `json:"min_input_tokens" gorm:"default:0"`  // estimated input tokens must exceed this to route here, 0 = no minimum
}

func (ChannelExtend) TableName() string {
	return "channel_extend"
}

func (ce *ChannelExtend) ToSettings() dto.ChannelExtendSettings {
	if ce == nil {
		return dto.ChannelExtendSettings{}
	}
	return dto.ChannelExtendSettings{
		RelayTimeout:     ce.RelayTimeout,
		StreamingTimeout: ce.StreamingTimeout,
		MinInputTokens:   ce.MinInputTokens,
	}
}

// UpsertChannelExtend persists per-channel overrides. All-zero settings delete
// the row so "inherit global" channels keep no record. A nil tx uses DB.
func UpsertChannelExtend(tx *gorm.DB, channelId int, settings dto.ChannelExtendSettings) error {
	if channelId == 0 {
		return errors.New("channel id is required")
	}
	if tx == nil {
		tx = DB
	}
	if settings.IsZero() {
		return tx.Where("channel_id = ?", channelId).Delete(&ChannelExtend{}).Error
	}
	extend := ChannelExtend{
		ChannelId:        channelId,
		RelayTimeout:     settings.RelayTimeout,
		StreamingTimeout: settings.StreamingTimeout,
		MinInputTokens:   settings.MinInputTokens,
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"relay_timeout", "streaming_timeout", "min_input_tokens"}),
	}).Create(&extend).Error
}

// SaveExtendConfig persists the transport-only ExtendConfig payload for an
// already-inserted channel. A nil ExtendConfig leaves existing rows untouched.
func (channel *Channel) SaveExtendConfig(tx *gorm.DB) error {
	if channel.ExtendConfig == nil || channel.Id == 0 {
		return nil
	}
	return UpsertChannelExtend(tx, channel.Id, *channel.ExtendConfig)
}

// GetChannelExtend returns the stored overrides for a channel, or zero-value
// settings when no row exists.
func GetChannelExtend(channelId int) (dto.ChannelExtendSettings, error) {
	var extend ChannelExtend
	err := DB.Where("channel_id = ?", channelId).Take(&extend).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ChannelExtendSettings{}, nil
		}
		return dto.ChannelExtendSettings{}, err
	}
	return extend.ToSettings(), nil
}

func DeleteChannelExtendByIds(tx *gorm.DB, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	if tx == nil {
		tx = DB
	}
	return tx.Where("channel_id in (?)", ids).Delete(&ChannelExtend{}).Error
}
