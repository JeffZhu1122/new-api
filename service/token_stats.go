package service

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

// Settled usage statistics: one entry per request that reached billing
// settlement (the requests that write a consume log), with its tokens split
// into input excluding cache, cache read, cache write and output. They share
// the buckets, flush and window of rpm_stats.go.
//
// Hashes per bucket, fields "<metric>\x1f<value>":
//   - s:all                       value empty: the whole site
//   - s:ch, s:u, s:m, s:g, s:t    value: channel id, user id, model, group, token name
//   - s:cu:<channelId>            value: user id
//   - s:d:<userId>                value: token \x1f model \x1f channel id \x1f group;
//     the per-user detail every usage-log filter combination is answered from
//   - s:users                     not a counter: user id -> username
const (
	statsKindAll         = "s:all"
	statsKindChannel     = "s:ch"
	statsKindUser        = "s:u"
	statsKindModel       = "s:m"
	statsKindGroup       = "s:g"
	statsKindToken       = "s:t"
	statsKindChannelUser = "s:cu"
	statsKindDetail      = "s:d"
	statsKindUsers       = "s:users"
)

const (
	statsMetricRequests   = "req"
	statsMetricInput      = "in"
	statsMetricCacheRead  = "cr"
	statsMetricCacheWrite = "cw"
	statsMetricOutput     = "out"
)

var statsMetrics = []string{statsMetricRequests, statsMetricInput, statsMetricCacheRead, statsMetricCacheWrite, statsMetricOutput}

const statsFieldSep = "\x1f"

// TokenStats is the settled usage of one slice of traffic within a window.
// Input never contains cached tokens, whatever the upstream usage format.
type TokenStats struct {
	Requests   int64 `json:"requests"`
	Input      int64 `json:"input"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Output     int64 `json:"output"`
}

// Total is the TPM: every token category added up.
func (s TokenStats) Total() int64 {
	return s.Input + s.CacheRead + s.CacheWrite + s.Output
}

func (s TokenStats) metric(name string) int64 {
	switch name {
	case statsMetricRequests:
		return s.Requests
	case statsMetricInput:
		return s.Input
	case statsMetricCacheRead:
		return s.CacheRead
	case statsMetricCacheWrite:
		return s.CacheWrite
	case statsMetricOutput:
		return s.Output
	}
	return 0
}

func (s *TokenStats) add(name string, n int64) {
	switch name {
	case statsMetricRequests:
		s.Requests += n
	case statsMetricInput:
		s.Input += n
	case statsMetricCacheRead:
		s.CacheRead += n
	case statsMetricCacheWrite:
		s.CacheWrite += n
	case statsMetricOutput:
		s.Output += n
	}
}

// statsDetail identifies one usage-log row shape within a user's traffic.
type statsDetail struct {
	tokenName string
	modelName string
	channelID int
	group     string
}

func (d statsDetail) value() string {
	clean := strings.NewReplacer(statsFieldSep, " ")
	return strings.Join([]string{
		clean.Replace(d.tokenName),
		clean.Replace(d.modelName),
		strconv.Itoa(d.channelID),
		clean.Replace(d.group),
	}, statsFieldSep)
}

func parseDetailField(field string) (string, statsDetail, bool) {
	parts := strings.Split(field, statsFieldSep)
	if len(parts) != 5 {
		return "", statsDetail{}, false
	}
	channelID, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", statsDetail{}, false
	}
	return parts[0], statsDetail{tokenName: parts[1], modelName: parts[2], channelID: channelID, group: parts[4]}, true
}

func tokenStatsFields(values []string) []string {
	fields := make([]string, 0, len(values)*len(statsMetrics))
	for _, value := range values {
		for _, metric := range statsMetrics {
			fields = append(fields, metric+statsFieldSep+value)
		}
	}
	return fields
}

func idTokenStats(sums map[string]int64) map[int]TokenStats {
	result := map[int]TokenStats{}
	for field, n := range sums {
		metric, value, ok := strings.Cut(field, statsFieldSep)
		if !ok {
			continue
		}
		id, err := strconv.Atoi(value)
		if err != nil {
			continue
		}
		stats := result[id]
		stats.add(metric, n)
		result[id] = stats
	}
	return result
}

// RecordSettledUsage counts one settled request under the user, token, model,
// channel and group its consume log carries. tokens.Requests is ignored:
// every call is one request. Channel tests, violation fees and the free
// count_tokens probes are not traffic and are not recorded.
func RecordSettledUsage(c *gin.Context, userID int, params model.RecordConsumeLogParams, tokens TokenStats) {
	username := ""
	if c != nil {
		username = c.GetString("username")
	}
	rpmStats.recordSettled(time.Now(), userID, username, statsDetail{
		tokenName: params.TokenName,
		modelName: params.ModelName,
		channelID: params.ChannelId,
		group:     params.Group,
	}, tokens)
}

func (r *rpmStatsRecorder) recordSettled(at time.Time, userID int, username string, detail statsDetail, tokens TokenStats) {
	tokens.Requests = 1
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled {
		return
	}
	bucket := r.bucketAt(at)
	user := strconv.Itoa(userID)
	channel := strconv.Itoa(detail.channelID)
	detailValue := detail.value()
	for _, metric := range statsMetrics {
		n := tokens.metric(metric)
		if n <= 0 {
			continue
		}
		bucket.add(rpmStatsHash{kind: statsKindAll}, metric+statsFieldSep, n)
		if detail.modelName != "" {
			bucket.add(rpmStatsHash{kind: statsKindModel}, metric+statsFieldSep+detail.modelName, n)
		}
		if detail.group != "" {
			bucket.add(rpmStatsHash{kind: statsKindGroup}, metric+statsFieldSep+detail.group, n)
		}
		if detail.tokenName != "" {
			bucket.add(rpmStatsHash{kind: statsKindToken}, metric+statsFieldSep+detail.tokenName, n)
		}
		if detail.channelID > 0 {
			bucket.add(rpmStatsHash{kind: statsKindChannel}, metric+statsFieldSep+channel, n)
		}
		if userID <= 0 {
			continue
		}
		bucket.add(rpmStatsHash{kind: statsKindUser}, metric+statsFieldSep+user, n)
		bucket.add(rpmStatsHash{kind: statsKindDetail, owner: userID}, metric+statsFieldSep+detailValue, n)
		if detail.channelID > 0 {
			bucket.add(rpmStatsHash{kind: statsKindChannelUser, owner: detail.channelID}, metric+statsFieldSep+user, n)
		}
	}
	if userID > 0 {
		bucket.users[userID] = username
	}
}

// settledTextTokens splits a text settlement the way billing reads its usage:
// OpenAI-style prompt counts include cache reads and writes, Claude-style
// (and legacy Claude-derived) prompt counts exclude them.
func settledTextTokens(summary textQuotaSummary, legacyClaudeDerived bool) TokenStats {
	tokens := TokenStats{
		Input:      int64(max(summary.PromptTokens, 0)),
		CacheRead:  int64(max(summary.CacheTokens, 0)),
		CacheWrite: int64(max(cacheWriteTokensTotal(summary), 0)),
		Output:     int64(max(summary.CompletionTokens, 0)),
	}
	if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
		// 缓存写入上报的是未调整的前缀计数，命中 + 写入可能超过输入总数，差值按 0 计
		tokens.Input = int64(max(summary.PromptTokens-summary.CacheTokens-summary.CacheCreationTokens, 0))
	}
	return tokens
}

// settledOpenAITokens splits OpenAI-style usage, whose input count includes
// cached reads and writes.
func settledOpenAITokens(input int, output int, details dto.InputTokenDetails) TokenStats {
	cacheWrite := details.CacheCreationTokensTotal()
	return TokenStats{
		Input:      int64(max(input-details.CachedTokens-cacheWrite, 0)),
		CacheRead:  int64(max(details.CachedTokens, 0)),
		CacheWrite: int64(cacheWrite),
		Output:     int64(max(output, 0)),
	}
}

// LogRateFilter mirrors the usage-log filters. Username and ModelName accept
// the same % patterns as the log queries; UserID pins the self view.
type LogRateFilter struct {
	UserID    int
	Username  string
	TokenName string
	ModelName string
	ChannelID int
	Group     string
}

// LogRateReading is the usage-log RPM / TPM: settled requests and their
// tokens within [WindowStart, WindowEnd).
type LogRateReading struct {
	Source      string
	WindowStart int64
	WindowEnd   int64
	Stats       TokenStats
}

// LogRate answers the usage-log statistics for any filter combination. It
// refuses the % patterns the log queries refuse; a Redis failure is logged and
// reported as an unavailable reading instead of an error.
func LogRate(filter LogRateFilter) (LogRateReading, error) {
	for _, pattern := range []string{filter.Username, filter.ModelName} {
		if err := model.ValidateLogTextPattern(pattern); err != nil {
			return LogRateReading{}, err
		}
	}
	reading, err := rpmStats.logRate(time.Now(), filter)
	if err != nil {
		common.SysError("log rate: live statistics unavailable: " + err.Error())
		return LogRateReading{Source: RpmStatsSourceUnavailable}, nil
	}
	return reading, nil
}

func (r *rpmStatsRecorder) logRate(at time.Time, f LogRateFilter) (LogRateReading, error) {
	w := r.window(at)
	reading := LogRateReading{Source: w.source, WindowStart: w.start, WindowEnd: w.end}

	// userIDs == nil means any user.
	var userIDs []int
	switch {
	case f.UserID > 0:
		userIDs = []int{f.UserID}
	case f.Username != "":
		users, err := r.windowUsers(w)
		if err != nil {
			return reading, err
		}
		for id, name := range users {
			if matchLogText(f.Username, name) {
				userIDs = append(userIDs, id)
			}
		}
		if len(userIDs) == 0 {
			return reading, nil
		}
	}

	// A single exact filter, or channel plus one user, has a summary hash.
	filters := 0
	for _, set := range []bool{userIDs != nil, f.TokenName != "", f.ModelName != "", f.ChannelID > 0, f.Group != ""} {
		if set {
			filters++
		}
	}
	singleUser := len(userIDs) == 1
	var summary *rpmStatsQuery
	switch {
	case filters == 0:
		summary = &rpmStatsQuery{hash: rpmStatsHash{kind: statsKindAll}, fields: tokenStatsFields([]string{""})}
	case filters == 1 && singleUser:
		summary = &rpmStatsQuery{hash: rpmStatsHash{kind: statsKindUser}, fields: tokenStatsFields([]string{strconv.Itoa(userIDs[0])})}
	case filters == 1 && f.ChannelID > 0:
		summary = &rpmStatsQuery{hash: rpmStatsHash{kind: statsKindChannel}, fields: tokenStatsFields([]string{strconv.Itoa(f.ChannelID)})}
	case filters == 1 && f.ModelName != "" && !strings.Contains(f.ModelName, "%"):
		summary = &rpmStatsQuery{hash: rpmStatsHash{kind: statsKindModel}, fields: tokenStatsFields([]string{f.ModelName})}
	case filters == 1 && f.Group != "":
		summary = &rpmStatsQuery{hash: rpmStatsHash{kind: statsKindGroup}, fields: tokenStatsFields([]string{f.Group})}
	case filters == 1 && f.TokenName != "":
		summary = &rpmStatsQuery{hash: rpmStatsHash{kind: statsKindToken}, fields: tokenStatsFields([]string{f.TokenName})}
	case filters == 2 && singleUser && f.ChannelID > 0:
		summary = &rpmStatsQuery{hash: rpmStatsHash{kind: statsKindChannelUser, owner: f.ChannelID}, fields: tokenStatsFields([]string{strconv.Itoa(userIDs[0])})}
	}
	if summary != nil {
		sums, err := r.sum(w, []rpmStatsQuery{*summary})
		for field, n := range sums[0] {
			metric, _, _ := strings.Cut(field, statsFieldSep)
			reading.Stats.add(metric, n)
		}
		return reading, err
	}

	// Any other combination filters the per-user detail of the matching users.
	if userIDs == nil {
		users, err := r.windowUsers(w)
		if err != nil {
			return reading, err
		}
		for id := range users {
			userIDs = append(userIDs, id)
		}
	}
	slices.Sort(userIDs)
	queries := make([]rpmStatsQuery, len(userIDs))
	for i, id := range userIDs {
		queries[i] = rpmStatsQuery{hash: rpmStatsHash{kind: statsKindDetail, owner: id}}
	}
	sums, err := r.sum(w, queries)
	for _, fields := range sums {
		for field, n := range fields {
			metric, detail, ok := parseDetailField(field)
			if !ok {
				continue
			}
			if f.TokenName != "" && detail.tokenName != f.TokenName {
				continue
			}
			if f.ModelName != "" && !matchLogText(f.ModelName, detail.modelName) {
				continue
			}
			if f.ChannelID > 0 && detail.channelID != f.ChannelID {
				continue
			}
			if f.Group != "" && detail.group != f.Group {
				continue
			}
			reading.Stats.add(metric, n)
		}
	}
	return reading, err
}

// matchLogText applies a usage-log text filter: an exact match, or a pattern
// whose % wildcards match any run of characters. Patterns compare
// case-insensitively, like the MySQL and SQLite log queries.
func matchLogText(pattern string, value string) bool {
	if !strings.Contains(pattern, "%") {
		return value == pattern
	}
	parts := strings.Split(strings.ToLower(pattern), "%")
	rest, ok := strings.CutPrefix(strings.ToLower(value), parts[0])
	if !ok {
		return false
	}
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, part)
		if i < 0 {
			return false
		}
		rest = rest[i+len(part):]
	}
	return strings.HasSuffix(rest, parts[len(parts)-1])
}
