package controller

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	// rpmStatsMaxIds bounds one batch reading; it covers the largest table page.
	rpmStatsMaxIds       = 200
	rpmStatsDefaultLimit = 20
	rpmStatsMaxLimit     = 100
)

type rpmStatsTotals struct {
	Source      string        `json:"source"`
	WindowStart int64         `json:"window_start"`
	WindowEnd   int64         `json:"window_end"`
	Items       map[int]int64 `json:"items"`
}

type rpmStatsBreakdownItem struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
	Rpm  int64  `json:"rpm"`
}

type rpmStatsBreakdown struct {
	Source      string                  `json:"source"`
	WindowStart int64                   `json:"window_start"`
	WindowEnd   int64                   `json:"window_end"`
	Total       int64                   `json:"total"`
	Items       []rpmStatsBreakdownItem `json:"items"`
}

// GetChannelRpm returns the live RPM of the channels listed in ?ids=1,2,3.
func GetChannelRpm(c *gin.Context) {
	respondRpmTotals(c, service.ChannelRpm)
}

// GetUserRpm returns the live RPM of the users listed in ?ids=1,2,3.
func GetUserRpm(c *gin.Context) {
	respondRpmTotals(c, service.UserRpm)
}

func respondRpmTotals(c *gin.Context, read func([]int) (service.RpmReading, error)) {
	ids := make([]int, 0)
	for part := range strings.SplitSeq(c.Query("ids"), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > rpmStatsMaxIds {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	reading, err := read(ids)
	if err != nil {
		common.SysError("rpm stats: read failed: " + err.Error())
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rpmStatsTotals{
		Source:      reading.Source,
		WindowStart: reading.WindowStart,
		WindowEnd:   reading.WindowEnd,
		Items:       reading.Counts,
	})
}

// GetChannelRpmUsers lists the users with the highest RPM on one channel.
func GetChannelRpmUsers(c *gin.Context) {
	channelID, limit, ok := parseRpmBreakdownParams(c)
	if !ok {
		return
	}
	reading, err := service.ChannelUserRpm(channelID)
	if err != nil {
		common.SysError("rpm stats: read failed: " + err.Error())
		common.ApiError(c, err)
		return
	}
	breakdown := newRpmStatsBreakdown(reading, limit)
	for i := range breakdown.Items {
		if user, err := model.GetUserCache(breakdown.Items[i].Id); err == nil {
			breakdown.Items[i].Name = user.Username
		}
	}
	common.ApiSuccess(c, breakdown)
}

// GetUserRpmChannels lists the channels one user's requests went to, highest
// RPM first.
func GetUserRpmChannels(c *gin.Context) {
	userID, limit, ok := parseRpmBreakdownParams(c)
	if !ok {
		return
	}
	reading, err := service.UserChannelRpm(userID)
	if err != nil {
		common.SysError("rpm stats: read failed: " + err.Error())
		common.ApiError(c, err)
		return
	}
	breakdown := newRpmStatsBreakdown(reading, limit)
	if len(breakdown.Items) > 0 {
		ids := make([]int, len(breakdown.Items))
		for i, item := range breakdown.Items {
			ids[i] = item.Id
		}
		channels, err := model.GetChannelsByIds(ids)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		names := make(map[int]string, len(channels))
		for _, channel := range channels {
			names[channel.Id] = channel.Name
		}
		for i := range breakdown.Items {
			breakdown.Items[i].Name = names[breakdown.Items[i].Id]
		}
	}
	common.ApiSuccess(c, breakdown)
}

func parseRpmBreakdownParams(c *gin.Context) (id int, limit int, ok bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return 0, 0, false
	}
	limit = rpmStatsDefaultLimit
	if raw := c.Query("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return 0, 0, false
		}
		limit = min(limit, rpmStatsMaxLimit)
	}
	return id, limit, true
}

// newRpmStatsBreakdown keeps the limit busiest ids (ties by id) and the total
// over all of them.
func newRpmStatsBreakdown(reading service.RpmReading, limit int) rpmStatsBreakdown {
	breakdown := rpmStatsBreakdown{
		Source:      reading.Source,
		WindowStart: reading.WindowStart,
		WindowEnd:   reading.WindowEnd,
		Items:       make([]rpmStatsBreakdownItem, 0, len(reading.Counts)),
	}
	for id, rpm := range reading.Counts {
		if rpm <= 0 {
			continue
		}
		breakdown.Total += rpm
		breakdown.Items = append(breakdown.Items, rpmStatsBreakdownItem{Id: id, Rpm: rpm})
	}
	slices.SortFunc(breakdown.Items, func(a, b rpmStatsBreakdownItem) int {
		return cmp.Or(cmp.Compare(b.Rpm, a.Rpm), cmp.Compare(a.Id, b.Id))
	})
	if len(breakdown.Items) > limit {
		breakdown.Items = breakdown.Items[:limit]
	}
	return breakdown
}
