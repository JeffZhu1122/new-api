package model

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"gorm.io/gorm/clause"
)

// logResponseModelBackfillKey is the option that remembers how far the
// response model backfill has gone: the id of the last log it read, or
// logResponseModelBackfillDone once it reached the end of the logs.
const logResponseModelBackfillKey = "log_response_model_backfill"

const logResponseModelBackfillDone = "done"

const logResponseModelBackfillBatch = 500

// BackfillLogResponseModels fills logs.response_model and
// logs.response_model_mismatch on the logs written before those columns
// existed, from the response model their other JSON recorded, so the
// usage-log filters by response model find them too. The columns are judged
// by the same rule as when a log is written. A declaration equal to the
// request was not recorded in other then, so those logs keep an empty
// response model; every mismatch was recorded.
//
// It reads the logs once, in id order and in batches, and resumes after a
// restart where it stopped. ClickHouse logs are left as they are, because an
// update there rewrites table parts.
func BackfillLogResponseModels() error {
	if DB == nil || LOG_DB == nil || common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return nil
	}
	var progress []Option
	if err := DB.Where(&Option{Key: logResponseModelBackfillKey}).Limit(1).Find(&progress).Error; err != nil {
		return err
	}
	cursor := 0
	if len(progress) > 0 {
		if progress[0].Value == logResponseModelBackfillDone {
			return nil
		}
		cursor, _ = strconv.Atoi(progress[0].Value)
	}

	filled := 0
	for {
		var logs []Log
		err := LOG_DB.Select("id", "other").
			Where("id > ? AND other LIKE ? AND (response_model = '' OR response_model IS NULL)", cursor, `%"response_model"%`).
			Order("id").Limit(logResponseModelBackfillBatch).Find(&logs).Error
		if err != nil {
			return err
		}
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
			err := LOG_DB.Model(&Log{}).Where("id = ?", log.Id).Updates(map[string]any{
				"response_model":          responseModel,
				"response_model_mismatch": mismatch,
			}).Error
			if err != nil {
				return err
			}
			filled++
		}
		if len(logs) < logResponseModelBackfillBatch {
			break
		}
		cursor = logs[len(logs)-1].Id
		if err := saveLogResponseModelBackfill(strconv.Itoa(cursor)); err != nil {
			return err
		}
	}
	if err := saveLogResponseModelBackfill(logResponseModelBackfillDone); err != nil {
		return err
	}
	common.SysLog(common.LogText("filled the response model columns of %d earlier usage logs", filled))
	return nil
}

func saveLogResponseModelBackfill(value string) error {
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&Option{Key: logResponseModelBackfillKey, Value: value}).Error
}
