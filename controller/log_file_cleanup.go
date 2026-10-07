package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetLogFileCleanupSchedule reports this node's scheduled server log file
// cleanup: the time zone the run time is read in, the next run and the last
// run's outcome. The schedule itself is stored as log_file_cleanup_setting.*
// options and edited through the regular option API.
func GetLogFileCleanupSchedule(c *gin.Context) {
	common.ApiSuccess(c, service.GetLogFileCleanupScheduleStatus())
}
