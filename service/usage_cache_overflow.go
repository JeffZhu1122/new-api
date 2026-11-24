package service

import (
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// In the usage semantics whose input includes the cache (OpenAI, Gemini), an
// upstream cannot report more cache reads and writes than input. Kimi on one
// channel did, 2026-11-15 to 11-17: a reference tokenizer undercounted the
// input against the engine's cache blocks, and some responses counted one
// prefix both as read and as written. Billing used to clamp the remaining
// input at zero without a trace. Every such response is now recorded in
// admin_info.usage_anomaly and on the request summary line, and a channel
// with usage_correction bills the cache as at most the input.
const (
	// UsageAnomalyCacheReadOverInput: the cache reads alone exceed the input,
	// which no upstream can mean.
	UsageAnomalyCacheReadOverInput = "cache_read_over_input"
	// UsageAnomalyCacheSumOverInput: only reads and writes together exceed
	// it. OpenAI's cache write counts may overlap its reads, so a few are
	// expected there.
	UsageAnomalyCacheSumOverInput = "cache_sum_over_input"
)

// usageCacheOverflow is upstream usage whose cache reads and writes exceed
// the input that includes them.
type usageCacheOverflow struct {
	kind       string
	input      int
	cacheRead  int
	cacheWrite int
	overflow   int
}

// usageCacheSemantic is the semantic the request summary line reports:
// "anthropic" whenever the input excludes the cache, legacy Claude-derived
// OpenAI usage included, otherwise the usage's own semantic.
func usageCacheSemantic(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) string {
	semantic := usageSemanticFromUsage(relayInfo, usage)
	if isLegacyClaudeDerivedOpenAIUsage(relayInfo, usage) {
		return "anthropic"
	}
	return semantic
}

// detectUsageCacheOverflow returns the overflow of usage in a semantic whose
// input includes the cache, or nil when the cache fits in the input.
func detectUsageCacheOverflow(usage *dto.Usage, semantic string) *usageCacheOverflow {
	if usage == nil || semantic == "anthropic" {
		return nil
	}
	read := max(usage.PromptTokensDetails.CachedTokens, 0)
	write := usage.PromptTokensDetails.CacheCreationTokensTotal()
	overflow := read + write - usage.PromptTokens
	if overflow <= 0 {
		return nil
	}
	kind := UsageAnomalyCacheSumOverInput
	if read > usage.PromptTokens {
		kind = UsageAnomalyCacheReadOverInput
	}
	return &usageCacheOverflow{kind: kind, input: usage.PromptTokens, cacheRead: read, cacheWrite: write, overflow: overflow}
}

// correctedUsage returns a copy of usage whose cache fits in its input: the
// reads are cut to the input first, then the writes to what is left. A cut
// read count drops its modality split, which then no longer adds up.
func (o *usageCacheOverflow) correctedUsage(usage *dto.Usage) *dto.Usage {
	corrected := *usage
	details := usage.PromptTokensDetails
	input := max(usage.PromptTokens, 0)
	read := min(o.cacheRead, input)
	write := min(o.cacheWrite, input-read)
	if read != details.CachedTokens {
		details.CachedTokens = read
		details.CachedTokensDetails = nil
	}
	details.CacheWriteTokens = min(details.CacheWriteTokens, write)
	details.CachedCreationTokens = min(details.CachedCreationTokens, write)
	details.CacheCreationInputTokens = min(details.CacheCreationInputTokens, write)
	corrected.PromptTokensDetails = details
	return &corrected
}

// recordUsageCacheOverflow writes the audit of a settled text request: the
// semantic and any overflow on the summary line, and the upstream's own
// numbers under admin_info.usage_anomaly, with whether billing corrected them.
func recordUsageCacheOverflow(other *model.LogOther, semantic string, overflow *usageCacheOverflow, corrected bool) {
	if overflow == nil {
		other.SetUsageAudit(semantic, 0, "")
		return
	}
	other.SetUsageAudit(semantic, overflow.overflow, overflow.kind)
	other.SetAdmin("usage_anomaly", map[string]any{
		"kind":        overflow.kind,
		"input":       overflow.input,
		"cache_read":  overflow.cacheRead,
		"cache_write": overflow.cacheWrite,
		"overflow":    overflow.overflow,
		"corrected":   corrected,
	})
}
