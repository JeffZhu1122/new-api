package log_file_cleanup_setting

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

// Scheduled cleanup of the server log files under --log-dir. Each node runs
// the schedule against its own log directory, with the same retention rules
// as the manual cleanup (keep the newest N files, or files from the last N
// days). The time of day is interpreted in the server's local time zone.

const (
	FrequencyDaily   = "daily"
	FrequencyWeekly  = "weekly"
	FrequencyMonthly = "monthly"

	ModeByCount = "by_count"
	ModeByDays  = "by_days"

	// Month days stop at 28 so a monthly run exists in every month.
	MaxMonthDay = 28
)

type LogFileCleanupSetting struct {
	Enabled   bool   `json:"enabled"`
	Frequency string `json:"frequency"`
	// Weekday is used by the weekly frequency: 0 = Sunday ... 6 = Saturday.
	Weekday int `json:"weekday"`
	// MonthDay is used by the monthly frequency: 1 ... MaxMonthDay.
	MonthDay int `json:"month_day"`
	// Time is the run time of day, "HH:MM" in 24-hour form.
	Time  string `json:"time"`
	Mode  string `json:"mode"`
	Value int    `json:"value"`
}

var logFileCleanupSetting = LogFileCleanupSetting{
	Enabled:   false,
	Frequency: FrequencyDaily,
	Weekday:   1,
	MonthDay:  1,
	Time:      "03:00",
	Mode:      ModeByDays,
	Value:     30,
}

func init() {
	config.GlobalConfig.Register("log_file_cleanup_setting", &logFileCleanupSetting)
}

func GetSetting() LogFileCleanupSetting {
	return logFileCleanupSetting
}

// ParseTimeOfDay parses "HH:MM" (24-hour) into hour and minute.
func ParseTimeOfDay(value string) (int, int, error) {
	hourText, minuteText, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok {
		return 0, 0, fmt.Errorf("invalid time %q, expected HH:MM", value)
	}
	hour, hourErr := strconv.Atoi(hourText)
	minute, minuteErr := strconv.Atoi(minuteText)
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid time %q, expected HH:MM", value)
	}
	return hour, minute, nil
}

// NextRun returns the first scheduled run strictly after now, in now's time
// zone. It reports false when the schedule is disabled or not runnable.
func (s LogFileCleanupSetting) NextRun(now time.Time) (time.Time, bool) {
	if !s.Enabled || (s.Mode != ModeByCount && s.Mode != ModeByDays) || s.Value < 1 {
		return time.Time{}, false
	}
	hour, minute, err := ParseTimeOfDay(s.Time)
	if err != nil {
		return time.Time{}, false
	}
	year, month, day := now.Date()
	loc := now.Location()
	switch s.Frequency {
	case FrequencyDaily:
		next := time.Date(year, month, day, hour, minute, 0, 0, loc)
		if !next.After(now) {
			next = time.Date(year, month, day+1, hour, minute, 0, 0, loc)
		}
		return next, true
	case FrequencyWeekly:
		if s.Weekday < 0 || s.Weekday > 6 {
			return time.Time{}, false
		}
		offset := (s.Weekday - int(now.Weekday()) + 7) % 7
		next := time.Date(year, month, day+offset, hour, minute, 0, 0, loc)
		if !next.After(now) {
			next = time.Date(year, month, day+offset+7, hour, minute, 0, 0, loc)
		}
		return next, true
	case FrequencyMonthly:
		if s.MonthDay < 1 || s.MonthDay > MaxMonthDay {
			return time.Time{}, false
		}
		next := time.Date(year, month, s.MonthDay, hour, minute, 0, 0, loc)
		if !next.After(now) {
			next = time.Date(year, month+1, s.MonthDay, hour, minute, 0, 0, loc)
		}
		return next, true
	}
	return time.Time{}, false
}

// ValidateOption checks a single "log_file_cleanup_setting.<field>" option
// value before it is stored.
func ValidateOption(key string, value string) error {
	field, ok := strings.CutPrefix(key, "log_file_cleanup_setting.")
	if !ok {
		return nil
	}
	switch field {
	case "enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("enabled must be true or false")
		}
	case "frequency":
		if value != FrequencyDaily && value != FrequencyWeekly && value != FrequencyMonthly {
			return fmt.Errorf("frequency must be daily, weekly or monthly")
		}
	case "weekday":
		weekday, err := strconv.Atoi(value)
		if err != nil || weekday < 0 || weekday > 6 {
			return fmt.Errorf("weekday must be an integer from 0 (Sunday) to 6 (Saturday)")
		}
	case "month_day":
		monthDay, err := strconv.Atoi(value)
		if err != nil || monthDay < 1 || monthDay > MaxMonthDay {
			return fmt.Errorf("month_day must be an integer from 1 to %d", MaxMonthDay)
		}
	case "time":
		if _, _, err := ParseTimeOfDay(value); err != nil {
			return err
		}
	case "mode":
		if value != ModeByCount && value != ModeByDays {
			return fmt.Errorf("mode must be by_count or by_days")
		}
	case "value":
		number, err := strconv.Atoi(value)
		if err != nil || number < 1 {
			return fmt.Errorf("value must be a positive integer")
		}
	default:
		return fmt.Errorf("unknown log file cleanup option %q", key)
	}
	return nil
}
