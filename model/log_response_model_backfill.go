package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"gorm.io/gorm/clause"
)

// logResponseModelBackfillKey is the option that records, with the value
// "done", that one response model backfill read every log, so the scheduler
// stops queuing it.
const logResponseModelBackfillKey = "log_response_model_backfill"

const logResponseModelBackfillDone = "done"

// LogIDRange returns the smallest and the largest log id; both are 0 when
// there are no logs.
func LogIDRange() (int, int, error) {
	var bounds struct {
		MinID *int64
		MaxID *int64
	}
	if err := LOG_DB.Model(&Log{}).Select("MIN(id) AS min_id, MAX(id) AS max_id").Scan(&bounds).Error; err != nil {
		return 0, 0, err
	}
	if bounds.MinID == nil || bounds.MaxID == nil {
		return 0, 0, nil
	}
	return int(*bounds.MinID), int(*bounds.MaxID), nil
}

// FillLogResponseModels fills logs.response_model and
// logs.response_model_mismatch on the logs with an id in (afterID, throughID]
// that recorded a response model in other but have none in the columns: the
// logs written before the columns existed. It judges them by the rule a log is
// written with, so the usage-log filters by response model find them. A
// response model equal to the request was not recorded in other then, so
// those logs stay empty; every mismatch was recorded. It returns how many
// logs it filled.
func FillLogResponseModels(ctx context.Context, afterID int, throughID int) (int64, error) {
	var logs []Log
	err := LOG_DB.WithContext(ctx).Select("id", "other").
		Where("id > ? AND id <= ? AND other LIKE ? AND (response_model = '' OR response_model IS NULL)", afterID, throughID, `%"response_model"%`).
		Order("id").Find(&logs).Error
	if err != nil {
		return 0, err
	}
	var filled int64
	for _, log := range logs {
		var other struct {
			ResponseModel *relaycommon.ResponseModel `json:"response_model"`
		}
		if common.UnmarshalJsonStr(log.Other, &other) != nil || other.ResponseModel == nil || other.ResponseModel.ReturnedModel == "" {
			continue
		}
		columns := NewLogOther()
		columns.SetResponseModel(other.ResponseModel.ReturnedModel, other.ResponseModel.Mismatch())
		responseModel, mismatch := columns.responseModelColumns()
		err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("id = ?", log.Id).Updates(map[string]any{
			"response_model":          responseModel,
			"response_model_mismatch": mismatch,
		}).Error
		if err != nil {
			return filled, err
		}
		filled++
	}
	return filled, nil
}

// LogResponseModelBackfillDone reports whether a response model backfill has
// read every log.
func LogResponseModelBackfillDone() bool {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[logResponseModelBackfillKey] == logResponseModelBackfillDone
}

// MarkLogResponseModelBackfillDone records that a response model backfill
// read every log, here and, through the options sync, on the other nodes.
func MarkLogResponseModelBackfillDone() error {
	err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&Option{Key: logResponseModelBackfillKey, Value: logResponseModelBackfillDone}).Error
	if err != nil {
		return err
	}
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[logResponseModelBackfillKey] = logResponseModelBackfillDone
	common.OptionMapRWMutex.Unlock()
	BumpSyncTopic(SyncTopicOptions)
	return nil
}
