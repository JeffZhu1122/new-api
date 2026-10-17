package controller

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// maxLogChannelFilter bounds how many channel IDs one usage-log query filters by.
const maxLogChannelFilter = 100

// parseLogChannelFilter reads the channel filter of the usage-log queries: one
// channel ID or several separated by commas or spaces, such as "3,5 8". A 0
// means no filter, as it always has.
func parseLogChannelFilter(raw string) ([]int, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || unicode.IsSpace(r)
	})
	ids := make([]int, 0, len(fields))
	for _, field := range fields {
		id, err := strconv.Atoi(field)
		if err != nil || id < 0 {
			return nil, common.NewMessage("Invalid channel ID")
		}
		if id > 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) > maxLogChannelFilter {
		return nil, common.NewMessage("Search at most {{max}} channel IDs at once", map[string]any{"max": maxLogChannelFilter})
	}
	return ids, nil
}

// parseLogTokenRange reads the input / output token bounds of the usage-log
// queries. Each is a whole number of 0 or more, and a minimum may not exceed
// its maximum.
func parseLogTokenRange(c *gin.Context) (model.LogTokenRange, error) {
	var tokens model.LogTokenRange
	for _, bound := range []struct {
		param  string
		target **int
	}{
		{"prompt_tokens_min", &tokens.PromptMin},
		{"prompt_tokens_max", &tokens.PromptMax},
		{"completion_tokens_min", &tokens.CompletionMin},
		{"completion_tokens_max", &tokens.CompletionMax},
	} {
		raw := strings.TrimSpace(c.Query(bound.param))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return model.LogTokenRange{}, common.NewMessage("Token counts must be whole numbers of 0 or more")
		}
		*bound.target = &value
	}
	for _, pair := range [][2]*int{{tokens.PromptMin, tokens.PromptMax}, {tokens.CompletionMin, tokens.CompletionMax}} {
		if pair[0] != nil && pair[1] != nil && *pair[0] > *pair[1] {
			return model.LogTokenRange{}, common.NewMessage("The minimum token count cannot be greater than the maximum")
		}
	}
	return tokens, nil
}

// logRate answers the live RPM / TPM for the usage-log filters. The live
// statistics keep no per-request token counts, so a token range is reported
// as a filter they cannot apply instead of an unfiltered reading.
func logRate(filter service.LogRateFilter, tokens model.LogTokenRange) (service.LogRateReading, error) {
	if !tokens.IsZero() {
		return service.LogRateReading{Source: service.RpmStatsSourceFilterUnsupported}, nil
	}
	return service.LogRate(filter)
}

// logFilterFromQuery reads the log search shared by the log list and the log
// export. Username and channel are read only for administrators.
func logFilterFromQuery(c *gin.Context, admin bool) (model.LogFilter, error) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	filter := model.LogFilter{
		Type:              logType,
		StartTimestamp:    startTimestamp,
		EndTimestamp:      endTimestamp,
		ModelName:         c.Query("model_name"),
		TokenName:         c.Query("token_name"),
		Group:             c.Query("group"),
		RequestId:         c.Query("request_id"),
		UpstreamRequestId: c.Query("upstream_request_id"),
	}
	tokens, err := parseLogTokenRange(c)
	if err != nil {
		return filter, err
	}
	filter.Tokens = tokens
	if admin {
		filter.Username = c.Query("username")
		channels, err := parseLogChannelFilter(c.Query("channel"))
		if err != nil {
			return filter, err
		}
		filter.Channels = channels
	}
	return filter, nil
}

// limitLogTimeRange returns the start of a log or dashboard search kept within
// the longest log time range. A missing start reaches back the whole range, and
// time after now does not count toward it.
func limitLogTimeRange(startTimestamp int64, endTimestamp int64) (int64, error) {
	maxRange := operation_setting.GetLogSetting().MaxRangeSeconds()
	end := common.GetTimestamp()
	if endTimestamp != 0 {
		end = min(end, endTimestamp)
	}
	if startTimestamp == 0 {
		return end - maxRange, nil
	}
	if end-startTimestamp > maxRange {
		return 0, common.NewMessage("The time range cannot exceed {{days}} days", map[string]any{"days": maxRange / (24 * 60 * 60)})
	}
	return startTimestamp, nil
}

func GetAllLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	filter, err := logFilterFromQuery(c, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	logs, total, err := model.GetAllLogs(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if c.GetInt("role") < common.RoleRootUser {
		model.FormatAdminLogs(logs)
	} else {
		model.FormatRootLogs(logs)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	filter, err := logFilterFromQuery(c, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if c.GetInt("role") < common.RoleAdminUser {
		startTimestamp, err := limitLogTimeRange(filter.StartTimestamp, filter.EndTimestamp)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		filter.StartTimestamp = startTimestamp
	}
	logs, total, err := model.GetUserLogs(c.GetInt("id"), filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

// ExportAllLogs streams every user's logs matching the search.
func ExportAllLogs(c *gin.Context) {
	exportLogs(c, true)
}

// ExportUserLogs streams the caller's own logs matching the search.
func ExportUserLogs(c *gin.Context) {
	if c.GetInt("role") < common.RoleAdminUser && !operation_setting.GetLogSetting().UserExportEnabled {
		common.ApiErrorT(c, "Log export is disabled")
		return
	}
	exportLogs(c, false)
}

// exportLogs writes one JSON log per line, newest first, and ends with an API
// response object: success with the log count, or the failure. Each user runs
// one export at a time, and every export stays within the longest log time
// range.
func exportLogs(c *gin.Context, all bool) {
	filter, err := logFilterFromQuery(c, all)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	startTimestamp, err := limitLogTimeRange(filter.StartTimestamp, filter.EndTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	filter.StartTimestamp = startTimestamp

	userId := c.GetInt("id")
	release, ok, err := service.AcquireLogExportLock(userId)
	if err != nil {
		common.SysError(common.LogText("failed to lock log export: %s", err.Error()))
		common.ApiErrorT(c, "Failed to export logs")
		return
	}
	if !ok {
		common.ApiErrorT(c, "Another log export is running. Try again after it finishes.")
		return
	}
	defer release()

	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	count := 0
	writeLog := func(log *model.Log) error {
		line, err := common.Marshal(log)
		if err != nil {
			return err
		}
		if _, err := c.Writer.Write(append(line, '\n')); err != nil {
			return err
		}
		count++
		return nil
	}
	ctx := c.Request.Context()
	if all {
		err = model.StreamAllLogs(ctx, filter, c.GetInt("role"), writeLog)
	} else {
		err = model.StreamUserLogs(ctx, userId, filter, writeLog)
	}
	if ctx.Err() != nil {
		return
	}
	// The status is already sent, so the result object becomes the last line.
	if err != nil {
		common.SysError(common.LogText("failed to export logs: %s", err.Error()))
		common.ApiErrorT(c, "Failed to export logs")
		return
	}
	common.ApiSuccess(c, gin.H{"count": count})
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c *gin.Context) {
	common.ApiErrorT(c, "This API is deprecated")
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c *gin.Context) {
	common.ApiErrorT(c, "This API is deprecated")
}

func GetLogByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		common.ApiErrorI18n(c, i18n.MsgTokenInvalid)
		return
	}
	logs, err := model.GetLogByTokenId(tokenId)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetLogsStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	channels, err := parseLogChannelFilter(c.Query("channel"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tokens, err := parseLogTokenRange(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	group := c.Query("group")
	var stat model.Stat
	if c.Query("rate_only") != "true" {
		var err error
		stat, err = model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channels, group, tokens)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	rate, err := logRate(service.LogRateFilter{Username: username, TokenName: tokenName, ModelName: modelName, ChannelIDs: channels, Group: group}, tokens)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    logStatData(stat.Quota, rate),
	})
}

func GetLogsSelfStat(c *gin.Context) {
	username := c.GetString("username")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if c.GetInt("role") < common.RoleAdminUser {
		var err error
		if startTimestamp, err = limitLogTimeRange(startTimestamp, endTimestamp); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channels, err := parseLogChannelFilter(c.Query("channel"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tokens, err := parseLogTokenRange(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	group := c.Query("group")
	var quotaNum model.Stat
	if c.Query("rate_only") != "true" {
		var err error
		quotaNum, err = model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channels, group, tokens)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	rate, err := logRate(service.LogRateFilter{UserID: c.GetInt("id"), TokenName: tokenName, ModelName: modelName, ChannelIDs: channels, Group: group}, tokens)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    logStatData(quotaNum.Quota, rate),
	})
}

// logStatData combines the quota of the selected time range (logs table,
// skipped by rate_only polls) with the live RPM / TPM of the last settled
// minute.
func logStatData(quota int, rate service.LogRateReading) gin.H {
	return gin.H{
		"quota":             quota,
		"rpm":               rate.Stats.Requests,
		"tpm":               rate.Stats.Total(),
		"tpm_input":         rate.Stats.Input,
		"tpm_cache_read":    rate.Stats.CacheRead,
		"tpm_cache_write":   rate.Stats.CacheWrite,
		"tpm_output":        rate.Stats.Output,
		"rate_source":       rate.Source,
		"rate_window_start": rate.WindowStart,
		"rate_window_end":   rate.WindowEnd,
	}
}
