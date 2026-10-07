package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/log_file_cleanup_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogFileCleanupNextRun(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	// Wednesday 2026-10-07 10:00 in UTC+8.
	now := time.Date(2026, 10, 7, 10, 0, 0, 0, loc)
	base := log_file_cleanup_setting.LogFileCleanupSetting{
		Enabled:   true,
		Frequency: log_file_cleanup_setting.FrequencyDaily,
		Weekday:   1,
		MonthDay:  1,
		Time:      "03:00",
		Mode:      log_file_cleanup_setting.ModeByDays,
		Value:     30,
	}

	cases := []struct {
		name   string
		modify func(s *log_file_cleanup_setting.LogFileCleanupSetting)
		want   time.Time
		ok     bool
	}{
		{"daily time already passed today runs tomorrow", func(s *log_file_cleanup_setting.LogFileCleanupSetting) {}, time.Date(2026, 10, 8, 3, 0, 0, 0, loc), true},
		{"daily time later today runs today", func(s *log_file_cleanup_setting.LogFileCleanupSetting) { s.Time = "23:30" }, time.Date(2026, 10, 7, 23, 30, 0, 0, loc), true},
		{"daily exactly now runs tomorrow", func(s *log_file_cleanup_setting.LogFileCleanupSetting) { s.Time = "10:00" }, time.Date(2026, 10, 8, 10, 0, 0, 0, loc), true},
		{"weekly later this week", func(s *log_file_cleanup_setting.LogFileCleanupSetting) {
			s.Frequency = log_file_cleanup_setting.FrequencyWeekly
			s.Weekday = int(time.Friday)
		}, time.Date(2026, 10, 9, 3, 0, 0, 0, loc), true},
		{"weekly same weekday after the time waits a week", func(s *log_file_cleanup_setting.LogFileCleanupSetting) {
			s.Frequency = log_file_cleanup_setting.FrequencyWeekly
			s.Weekday = int(time.Wednesday)
		}, time.Date(2026, 10, 14, 3, 0, 0, 0, loc), true},
		{"weekly earlier weekday wraps to next week", func(s *log_file_cleanup_setting.LogFileCleanupSetting) {
			s.Frequency = log_file_cleanup_setting.FrequencyWeekly
			s.Weekday = int(time.Monday)
		}, time.Date(2026, 10, 12, 3, 0, 0, 0, loc), true},
		{"monthly later this month", func(s *log_file_cleanup_setting.LogFileCleanupSetting) {
			s.Frequency = log_file_cleanup_setting.FrequencyMonthly
			s.MonthDay = 15
		}, time.Date(2026, 10, 15, 3, 0, 0, 0, loc), true},
		{"monthly passed this month runs next month", func(s *log_file_cleanup_setting.LogFileCleanupSetting) {
			s.Frequency = log_file_cleanup_setting.FrequencyMonthly
			s.MonthDay = 1
		}, time.Date(2026, 11, 1, 3, 0, 0, 0, loc), true},
		{"disabled has no run", func(s *log_file_cleanup_setting.LogFileCleanupSetting) { s.Enabled = false }, time.Time{}, false},
		{"invalid time has no run", func(s *log_file_cleanup_setting.LogFileCleanupSetting) { s.Time = "25:00" }, time.Time{}, false},
		{"invalid mode has no run", func(s *log_file_cleanup_setting.LogFileCleanupSetting) { s.Mode = "all" }, time.Time{}, false},
		{"non-positive value has no run", func(s *log_file_cleanup_setting.LogFileCleanupSetting) { s.Value = 0 }, time.Time{}, false},
		{"unknown frequency has no run", func(s *log_file_cleanup_setting.LogFileCleanupSetting) { s.Frequency = "hourly" }, time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting := base
			tc.modify(&setting)
			got, ok := setting.NextRun(now)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.True(t, tc.want.Equal(got), "want %s, got %s", tc.want, got)
			}
		})
	}
}

func TestLogFileCleanupValidateOption(t *testing.T) {
	cases := []struct {
		key   string
		value string
		valid bool
	}{
		{"log_file_cleanup_setting.enabled", "true", true},
		{"log_file_cleanup_setting.enabled", "yes", false},
		{"log_file_cleanup_setting.frequency", "weekly", true},
		{"log_file_cleanup_setting.frequency", "hourly", false},
		{"log_file_cleanup_setting.weekday", "6", true},
		{"log_file_cleanup_setting.weekday", "7", false},
		{"log_file_cleanup_setting.month_day", "28", true},
		{"log_file_cleanup_setting.month_day", "29", false},
		{"log_file_cleanup_setting.time", "03:05", true},
		{"log_file_cleanup_setting.time", "3pm", false},
		{"log_file_cleanup_setting.time", "24:00", false},
		{"log_file_cleanup_setting.mode", "by_count", true},
		{"log_file_cleanup_setting.mode", "by_size", false},
		{"log_file_cleanup_setting.value", "1", true},
		{"log_file_cleanup_setting.value", "0", false},
		{"log_file_cleanup_setting.unknown", "1", false},
		{"LogConsumeEnabled", "anything", true},
	}
	for _, tc := range cases {
		err := log_file_cleanup_setting.ValidateOption(tc.key, tc.value)
		if tc.valid {
			assert.NoError(t, err, "%s=%s", tc.key, tc.value)
		} else {
			assert.Error(t, err, "%s=%s", tc.key, tc.value)
		}
	}
}

// writeServerLogFile creates a rotated server log file with the given age.
func writeServerLogFile(t *testing.T, dir string, name string, age time.Duration) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("log line\n"), 0o644))
	modTime := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, modTime, modTime))
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func useTempLogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous := *common.LogDir
	*common.LogDir = dir
	t.Cleanup(func() { *common.LogDir = previous })
	return dir
}

func TestCleanupServerLogFilesKeepsNewestByCount(t *testing.T) {
	dir := useTempLogDir(t)
	writeServerLogFile(t, dir, "oneapi-20260101000000.log", 0)
	writeServerLogFile(t, dir, "oneapi-20260201000000.log", 0)
	writeServerLogFile(t, dir, "oneapi-20260301000000.log", 0)
	writeServerLogFile(t, dir, "notes.txt", 0)

	result, err := CleanupServerLogFiles(log_file_cleanup_setting.ModeByCount, 2)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Planned)
	assert.Equal(t, 1, result.DeletedCount)
	assert.Empty(t, result.FailedFiles)
	assert.ElementsMatch(t, []string{"notes.txt", "oneapi-20260201000000.log", "oneapi-20260301000000.log"}, listDir(t, dir))
}

func TestCleanupServerLogFilesRemovesOlderThanDays(t *testing.T) {
	dir := useTempLogDir(t)
	writeServerLogFile(t, dir, "oneapi-20260101000000.log", 40*24*time.Hour)
	writeServerLogFile(t, dir, "oneapi-20260201000000.log", 10*24*time.Hour)
	writeServerLogFile(t, dir, "oneapi-20260301000000.log", time.Hour)

	result, err := CleanupServerLogFiles(log_file_cleanup_setting.ModeByDays, 30)
	require.NoError(t, err)

	assert.Equal(t, 1, result.DeletedCount)
	assert.ElementsMatch(t, []string{"oneapi-20260201000000.log", "oneapi-20260301000000.log"}, listDir(t, dir))
}

func TestCleanupServerLogFilesNeverDeletesTheActiveLog(t *testing.T) {
	dir := useTempLogDir(t)
	// Rotated files named after the active one sort as newer, so the active
	// file falls outside "keep the newest 1" and must still survive.
	writeServerLogFile(t, dir, "oneapi-29990101000000.log", 0)
	writeServerLogFile(t, dir, "oneapi-29990201000000.log", 0)
	logger.SetupLogger()
	t.Cleanup(func() {
		gin.DefaultWriter = os.Stdout
		gin.DefaultErrorWriter = os.Stderr
	})
	active := filepath.Base(logger.GetCurrentLogPath())
	require.Equal(t, dir, filepath.Dir(logger.GetCurrentLogPath()))

	_, err := CleanupServerLogFiles(log_file_cleanup_setting.ModeByCount, 1)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"oneapi-29990201000000.log", active}, listDir(t, dir))
}

func TestScheduledLogFileCleanupRecordsTheRun(t *testing.T) {
	dir := useTempLogDir(t)
	writeServerLogFile(t, dir, "oneapi-20260101000000.log", 40*24*time.Hour)
	writeServerLogFile(t, dir, "oneapi-20260301000000.log", time.Hour)

	cfg := config.GlobalConfig.Get("log_file_cleanup_setting")
	require.NotNil(t, cfg)
	previous := log_file_cleanup_setting.GetSetting()
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(cfg, map[string]string{
			"enabled":   common.Interface2String(previous.Enabled),
			"frequency": previous.Frequency,
			"time":      previous.Time,
			"mode":      previous.Mode,
			"value":     common.Interface2String(previous.Value),
		}))
	})
	require.NoError(t, config.UpdateConfigFromMap(cfg, map[string]string{
		"enabled":   "true",
		"frequency": "daily",
		"time":      "03:00",
		"mode":      "by_days",
		"value":     "30",
	}))

	setting := log_file_cleanup_setting.GetSetting()
	runScheduledLogFileCleanup(setting, time.Now())

	status := GetLogFileCleanupScheduleStatus()
	require.NotNil(t, status.LastRun)
	assert.Equal(t, "by_days", status.LastRun.Mode)
	assert.Equal(t, 30, status.LastRun.Value)
	assert.Equal(t, 1, status.LastRun.DeletedCount)
	assert.Empty(t, status.LastRun.Error)
	assert.True(t, status.LogDirConfigured)
	assert.Greater(t, status.NextRunAt, time.Now().Unix())
	assert.ElementsMatch(t, []string{"oneapi-20260301000000.log"}, listDir(t, dir))
}
