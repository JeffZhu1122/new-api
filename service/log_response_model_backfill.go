package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// logResponseModelBackfillWindow is how many log ids one step of the backfill
// reads, so each query stays bounded however sparse the earlier logs are.
const logResponseModelBackfillWindow = 5000

// ErrLogResponseModelBackfillClickHouse refuses a backfill of a ClickHouse log
// database, where an update rewrites table parts.
var ErrLogResponseModelBackfillClickHouse = common.NewMessage("ClickHouse log databases are not backfilled")

// logResponseModelBackfillHandler fills the response model columns of the
// logs written before they existed (model.FillLogResponseModels), reading
// every log in id order. The scheduler queues it after the upgrade until a run
// reads every log; root can run it again from the log maintenance settings.
type logResponseModelBackfillHandler struct{}

func init() {
	RegisterSystemTaskHandler(logResponseModelBackfillHandler{})
}

func (logResponseModelBackfillHandler) Type() string {
	return model.SystemTaskTypeLogResponseModelBackfill
}

// Enabled keeps queuing the backfill until one run reads every log, so a run
// cut off by a restart or an error is tried again.
func (logResponseModelBackfillHandler) Enabled() bool {
	return !common.UsingLogDatabase(common.DatabaseTypeClickHouse) && !model.LogResponseModelBackfillDone()
}

func (logResponseModelBackfillHandler) Interval() time.Duration { return 10 * time.Minute }

func (logResponseModelBackfillHandler) NewPayload() any { return nil }

// LogResponseModelBackfillState is the progress of a backfill run: the log ids
// it reads, the last id read and the logs it filled so far.
type LogResponseModelBackfillState struct {
	FromID   int   `json:"from_id"`
	ToID     int   `json:"to_id"`
	Cursor   int   `json:"cursor"`
	Progress int   `json:"progress"`
	Filled   int64 `json:"filled"`
}

type LogResponseModelBackfillResult struct {
	Filled int64 `json:"filled"`
}

func (logResponseModelBackfillHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		failSystemTask(task, runnerID, ErrLogResponseModelBackfillClickHouse)
		return
	}
	fromID, toID, err := model.LogIDRange()
	if err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	// Logs written after the run starts carry the columns already.
	state := LogResponseModelBackfillState{FromID: fromID, ToID: toID, Cursor: max(fromID-1, 0)}
	for state.Cursor < toID {
		through := min(state.Cursor+logResponseModelBackfillWindow, toID)
		filled, err := model.FillLogResponseModels(ctx, state.Cursor, through)
		state.Filled += filled
		if err != nil {
			if ctx.Err() == nil {
				failSystemTask(task, runnerID, err)
			}
			return
		}
		state.Cursor = through
		state.Progress = logCleanupProgress(int64(state.Cursor-fromID+1), int64(toID-fromID+1))
		if err := model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
			logSystemTaskLockError(ctx, task, err)
			return
		}
	}
	if err := model.MarkLogResponseModelBackfillDone(); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	state.Progress = 100
	if err := model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		logSystemTaskLockError(ctx, task, err)
		return
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, LogResponseModelBackfillResult{Filled: state.Filled}, ""); err != nil {
		logSystemTaskLockError(ctx, task, err)
	}
}
