package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
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
	MaxInputTokens   int `json:"max_input_tokens" gorm:"default:0"`  // estimated input tokens must not exceed this to route here, 0 = no maximum
	RpmLimit         int `json:"rpm_limit" gorm:"default:0"`         // channel-wide requests per minute, 0 = no limit
	TpmLimit         int `json:"tpm_limit" gorm:"default:0"`         // channel-wide tokens per minute, 0 = no limit
	// Anthropic credential scheme: "" / api_key (x-api-key), oauth (Bearer), auto (by key prefix)
	ClaudeAuthMode string `json:"claude_auth_mode" gorm:"type:varchar(16);default:''"`
	// Upstream response header filter: "" / blacklist / whitelist
	ResponseHeaderMode string `json:"response_header_mode" gorm:"type:varchar(16);default:''"`
	// JSON array of header names for ResponseHeaderMode, "" when the filter is off
	ResponseHeaders string `json:"response_headers" gorm:"type:text"`
	// Estimated USD paid upstream per $1 of billed quota, cost reporting only, 0 = not set
	CostRatio float64 `json:"cost_ratio" gorm:"default:0"`
	// Auto-disable once channels.used_quota reaches this many quota units, 0 = no limit
	QuotaLimit int64 `json:"quota_limit" gorm:"default:0"`
	// JSON dto.ChannelSchedule restricting when the channel is selectable, "" = always
	Schedule string `json:"schedule" gorm:"type:text"`
}

func (ChannelExtend) TableName() string {
	return "channel_extend"
}

func (ce *ChannelExtend) ToSettings() dto.ChannelExtendSettings {
	if ce == nil {
		return dto.ChannelExtendSettings{}
	}
	settings := dto.ChannelExtendSettings{
		RelayTimeout:       ce.RelayTimeout,
		StreamingTimeout:   ce.StreamingTimeout,
		MinInputTokens:     ce.MinInputTokens,
		MaxInputTokens:     ce.MaxInputTokens,
		RpmLimit:           ce.RpmLimit,
		TpmLimit:           ce.TpmLimit,
		ClaudeAuthMode:     ce.ClaudeAuthMode,
		ResponseHeaderMode: ce.ResponseHeaderMode,
		CostRatio:          ce.CostRatio,
		QuotaLimit:         ce.QuotaLimit,
	}
	if ce.ResponseHeaders != "" {
		if err := common.Unmarshal([]byte(ce.ResponseHeaders), &settings.ResponseHeaders); err != nil {
			common.SysError(fmt.Sprintf("channel %d has invalid response_headers json: %s", ce.ChannelId, err.Error()))
		}
	}
	if ce.Schedule != "" {
		var schedule dto.ChannelSchedule
		if err := common.Unmarshal([]byte(ce.Schedule), &schedule); err != nil {
			common.SysError(fmt.Sprintf("channel %d has invalid schedule json: %s", ce.ChannelId, err.Error()))
		} else {
			settings.Schedule = &schedule
		}
	}
	return settings
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
	responseHeaders := ""
	if len(settings.ResponseHeaders) > 0 {
		encoded, err := common.Marshal(settings.ResponseHeaders)
		if err != nil {
			return err
		}
		responseHeaders = string(encoded)
	}
	schedule := ""
	if settings.Schedule != nil {
		encoded, err := common.Marshal(settings.Schedule)
		if err != nil {
			return err
		}
		schedule = string(encoded)
	}
	extend := ChannelExtend{
		ChannelId:          channelId,
		RelayTimeout:       settings.RelayTimeout,
		StreamingTimeout:   settings.StreamingTimeout,
		MinInputTokens:     settings.MinInputTokens,
		MaxInputTokens:     settings.MaxInputTokens,
		RpmLimit:           settings.RpmLimit,
		TpmLimit:           settings.TpmLimit,
		ClaudeAuthMode:     settings.ClaudeAuthMode,
		ResponseHeaderMode: settings.ResponseHeaderMode,
		ResponseHeaders:    responseHeaders,
		CostRatio:          settings.CostRatio,
		QuotaLimit:         settings.QuotaLimit,
		Schedule:           schedule,
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"relay_timeout", "streaming_timeout", "min_input_tokens", "max_input_tokens", "rpm_limit", "tpm_limit", "claude_auth_mode", "response_header_mode", "response_headers", "cost_ratio", "quota_limit", "schedule"}),
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

// GetChannelExtendsByIds returns the stored overrides of the given channels
// keyed by channel id. Channels without a row are absent from the result.
func GetChannelExtendsByIds(ids []int) (map[int]dto.ChannelExtendSettings, error) {
	result := make(map[int]dto.ChannelExtendSettings, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var extends []ChannelExtend
	if err := DB.Where("channel_id in (?)", ids).Find(&extends).Error; err != nil {
		return nil, err
	}
	for i := range extends {
		result[extends[i].ChannelId] = extends[i].ToSettings()
	}
	return result, nil
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
