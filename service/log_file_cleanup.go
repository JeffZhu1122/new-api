package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/log_file_cleanup_setting"
)

// ServerLogFile is one rotated server log file under --log-dir.
type ServerLogFile struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// ServerLogCleanupResult reports one cleanup of the server log files.
type ServerLogCleanupResult struct {
	// Planned is the number of files selected for deletion.
	Planned      int      `json:"planned"`
	DeletedCount int      `json:"deleted_count"`
	FreedBytes   int64    `json:"freed_bytes"`
	FailedFiles  []string `json:"failed_files"`
}

// ListServerLogFiles returns the server log files, newest first. It returns
// nil when no log directory is configured.
func ListServerLogFiles() ([]ServerLogFile, error) {
	if *common.LogDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(*common.LogDir)
	if err != nil {
		return nil, err
	}
	var files []ServerLogFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "oneapi-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, ServerLogFile{
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	// Names embed the rotation time, so a descending name sort is newest first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name > files[j].Name
	})
	return files, nil
}

// serverLogCleanupMu serialises manual and scheduled cleanups of the same
// log directory.
var serverLogCleanupMu sync.Mutex

// CleanupServerLogFiles deletes server log files by the given retention rule:
// by_count keeps the newest value files, by_days keeps files modified within
// the last value days. The file currently being written is never deleted.
func CleanupServerLogFiles(mode string, value int) (ServerLogCleanupResult, error) {
	var result ServerLogCleanupResult
	if mode != log_file_cleanup_setting.ModeByCount && mode != log_file_cleanup_setting.ModeByDays {
		return result, fmt.Errorf("invalid mode, must be by_count or by_days")
	}
	if value < 1 {
		return result, fmt.Errorf("invalid value, must be a positive integer")
	}
	if *common.LogDir == "" {
		return result, fmt.Errorf("log directory not configured")
	}

	serverLogCleanupMu.Lock()
	defer serverLogCleanupMu.Unlock()

	files, err := ListServerLogFiles()
	if err != nil {
		return result, err
	}

	activeLogPath := logger.GetCurrentLogPath()
	var toDelete []ServerLogFile
	switch mode {
	case log_file_cleanup_setting.ModeByCount:
		for i, f := range files {
			if i < value {
				continue
			}
			if filepath.Join(*common.LogDir, f.Name) == activeLogPath {
				continue
			}
			toDelete = append(toDelete, f)
		}
	case log_file_cleanup_setting.ModeByDays:
		cutoff := time.Now().AddDate(0, 0, -value)
		for _, f := range files {
			if !f.ModTime.Before(cutoff) {
				continue
			}
			if filepath.Join(*common.LogDir, f.Name) == activeLogPath {
				continue
			}
			toDelete = append(toDelete, f)
		}
	}

	result.Planned = len(toDelete)
	for _, f := range toDelete {
		if err := os.Remove(filepath.Join(*common.LogDir, f.Name)); err != nil {
			result.FailedFiles = append(result.FailedFiles, f.Name)
			continue
		}
		result.DeletedCount++
		result.FreedBytes += f.Size
	}
	return result, nil
}

// LogFileCleanupRun records the outcome of one scheduled cleanup.
type LogFileCleanupRun struct {
	StartedAt    int64    `json:"started_at"`
	Mode         string   `json:"mode"`
	Value        int      `json:"value"`
	DeletedCount int      `json:"deleted_count"`
	FreedBytes   int64    `json:"freed_bytes"`
	FailedFiles  []string `json:"failed_files"`
	Error        string   `json:"error,omitempty"`
}

// LogFileCleanupScheduleStatus is this node's view of the schedule.
type LogFileCleanupScheduleStatus struct {
	// LogDirConfigured is false when --log-dir is not set; nothing runs then.
	LogDirConfigured bool `json:"log_dir_configured"`
	// Timezone and UTCOffset describe the zone the run time is read in.
	Timezone  string `json:"timezone"`
	UTCOffset string `json:"utc_offset"`
	// NextRunAt is a Unix timestamp, or 0 when no run is scheduled.
	NextRunAt int64              `json:"next_run_at"`
	LastRun   *LogFileCleanupRun `json:"last_run"`
}

const logFileCleanupTickInterval = 30 * time.Second

var (
	logFileCleanupStateMu sync.Mutex
	logFileCleanupLastRun *LogFileCleanupRun
	logFileCleanupOnce    sync.Once
)

// StartLogFileCleanupScheduler runs the scheduled server log file cleanup on
// this node. Every node cleans its own log directory, so it runs on master
// and slave nodes alike. A run time that passed while the process was down
// is skipped, not caught up.
func StartLogFileCleanupScheduler() {
	logFileCleanupOnce.Do(func() {
		go func() {
			scheduledFor := time.Time{}
			lastSchedule := log_file_cleanup_setting.LogFileCleanupSetting{}
			ticker := time.NewTicker(logFileCleanupTickInterval)
			defer ticker.Stop()
			for {
				now := time.Now()
				setting := log_file_cleanup_setting.GetSetting()
				if setting != lastSchedule || scheduledFor.IsZero() {
					// Settings changed (or first pass): re-plan from now.
					next, ok := setting.NextRun(now)
					if !ok {
						next = time.Time{}
					}
					scheduledFor = next
					lastSchedule = setting
				}
				if !scheduledFor.IsZero() && !now.Before(scheduledFor) {
					runScheduledLogFileCleanup(setting, now)
					next, ok := setting.NextRun(now)
					if !ok {
						next = time.Time{}
					}
					scheduledFor = next
				}
				<-ticker.C
			}
		}()
	})
}

func runScheduledLogFileCleanup(setting log_file_cleanup_setting.LogFileCleanupSetting, now time.Time) {
	run := &LogFileCleanupRun{
		StartedAt: now.Unix(),
		Mode:      setting.Mode,
		Value:     setting.Value,
	}
	if *common.LogDir == "" {
		run.Error = "log directory not configured"
	} else {
		result, err := CleanupServerLogFiles(setting.Mode, setting.Value)
		run.DeletedCount = result.DeletedCount
		run.FreedBytes = result.FreedBytes
		run.FailedFiles = result.FailedFiles
		switch {
		case err != nil:
			run.Error = err.Error()
		case len(result.FailedFiles) > 0:
			run.Error = fmt.Sprintf("failed to delete %d of %d files", len(result.FailedFiles), result.Planned)
		}
	}
	if run.Error != "" {
		common.SysError(fmt.Sprintf("scheduled log file cleanup (%s=%d): deleted %d, freed %d bytes, error: %s",
			run.Mode, run.Value, run.DeletedCount, run.FreedBytes, run.Error))
	} else {
		common.SysLog(fmt.Sprintf("scheduled log file cleanup (%s=%d): deleted %d, freed %d bytes",
			run.Mode, run.Value, run.DeletedCount, run.FreedBytes))
	}
	logFileCleanupStateMu.Lock()
	logFileCleanupLastRun = run
	logFileCleanupStateMu.Unlock()
}

// GetLogFileCleanupScheduleStatus returns this node's schedule status.
func GetLogFileCleanupScheduleStatus() LogFileCleanupScheduleStatus {
	now := time.Now()
	zone, offsetSeconds := now.Zone()
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	status := LogFileCleanupScheduleStatus{
		LogDirConfigured: *common.LogDir != "",
		Timezone:         now.Location().String(),
		UTCOffset:        fmt.Sprintf("%s%02d:%02d", sign, offsetSeconds/3600, offsetSeconds%3600/60),
	}
	if status.Timezone == "Local" {
		status.Timezone = zone
	}

	// The scheduler re-plans from the current settings on every change, so
	// the next run is the next slot after now (a slot that is due right now
	// runs within one tick).
	if status.LogDirConfigured {
		if next, ok := log_file_cleanup_setting.GetSetting().NextRun(now); ok {
			status.NextRunAt = next.Unix()
		}
	}

	logFileCleanupStateMu.Lock()
	if logFileCleanupLastRun != nil {
		lastRun := *logFileCleanupLastRun
		status.LastRun = &lastRun
	}
	logFileCleanupStateMu.Unlock()
	return status
}
