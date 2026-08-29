package setting

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(v int) *int {
	return &v
}

func useModelRateLimitRules(t *testing.T, jsonStr string) {
	t.Helper()
	require.NoError(t, UpdateModelRateLimitRulesByJSONString(jsonStr))
	t.Cleanup(func() {
		require.NoError(t, UpdateModelRateLimitRulesByJSONString(""))
	})
}

func TestResolveModelRateLimitPriority(t *testing.T) {
	useModelRateLimitRules(t, `{
		"default": {"rpm": 10, "tpm": 1000},
		"models": {"gpt-4o": {"rpm": 20, "tpm": 2000}},
		"groups": {
			"vip": {
				"default": {"rpm": 30, "tpm": 3000},
				"models": {"gpt-4o": {"rpm": 40, "tpm": 4000}}
			},
			"tpm-only": {
				"default": {"tpm": 5000}
			}
		}
	}`)

	userOverride := &dto.RateLimitOverride{
		Default: &dto.RateLimitValues{Rpm: intPtr(50)},
		Models: map[string]dto.RateLimitValues{
			"gpt-4o": {Rpm: intPtr(60)},
		},
	}

	tests := []struct {
		name         string
		group        string
		model        string
		userOverride *dto.RateLimitOverride
		wantRpm      int
		wantTpm      int
	}{
		{name: "global default", group: "unknown", model: "unknown-model", wantRpm: 10, wantTpm: 1000},
		{name: "global model beats global default", group: "unknown", model: "gpt-4o", wantRpm: 20, wantTpm: 2000},
		{name: "group default beats global model", group: "vip", model: "gpt-4o-mini", wantRpm: 30, wantTpm: 3000},
		{name: "group model beats group default", group: "vip", model: "gpt-4o", wantRpm: 40, wantTpm: 4000},
		{name: "empty model uses defaults", group: "vip", model: "", wantRpm: 30, wantTpm: 3000},
		{
			name:  "user override model wins, tpm falls back independently",
			group: "vip", model: "gpt-4o", userOverride: userOverride,
			wantRpm: 60, wantTpm: 4000,
		},
		{
			name:  "user override default beats group rules",
			group: "vip", model: "gpt-4o-mini", userOverride: userOverride,
			wantRpm: 50, wantTpm: 3000,
		},
		{
			name:  "rpm and tpm fall back independently across levels",
			group: "tpm-only", model: "claude-sonnet-5",
			wantRpm: 10, wantTpm: 5000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpm, tpm := ResolveModelRateLimit(tt.group, tt.model, tt.userOverride)
			assert.Equal(t, tt.wantRpm, rpm)
			assert.Equal(t, tt.wantTpm, tpm)
		})
	}
}

func TestResolveModelRateLimitExplicitZeroDisables(t *testing.T) {
	useModelRateLimitRules(t, `{
		"default": {"rpm": 10, "tpm": 1000},
		"groups": {"vip": {"default": {"rpm": 0}}}
	}`)

	rpm, tpm := ResolveModelRateLimit("vip", "gpt-4o", nil)
	assert.Equal(t, 0, rpm, "explicit 0 must stop the fallback and mean unlimited")
	assert.Equal(t, 1000, tpm)
}

func TestResolveModelRateLimitNoRules(t *testing.T) {
	useModelRateLimitRules(t, "")

	rpm, tpm := ResolveModelRateLimit("default", "gpt-4o", nil)
	assert.Equal(t, 0, rpm)
	assert.Equal(t, 0, tpm)
}

func TestCheckModelRateLimitRules(t *testing.T) {
	assert.NoError(t, CheckModelRateLimitRules(""))
	assert.NoError(t, CheckModelRateLimitRules(`{"default":{"rpm":60,"tpm":100000}}`))
	assert.NoError(t, CheckModelRateLimitRules(`{"groups":{"vip":{"models":{"gpt-4o":{"rpm":0}}}}}`))

	assert.Error(t, CheckModelRateLimitRules(`{invalid`))
	assert.Error(t, CheckModelRateLimitRules(`{"default":{"rpm":-1}}`))
	assert.Error(t, CheckModelRateLimitRules(`{"models":{"gpt-4o":{"tpm":-5}}}`))
	assert.Error(t, CheckModelRateLimitRules(`{"groups":{"vip":{"default":{"rpm":2147483648}}}}`))
}

func TestCheckRateLimitOverride(t *testing.T) {
	assert.NoError(t, CheckRateLimitOverride(nil))
	assert.NoError(t, CheckRateLimitOverride(&dto.RateLimitOverride{
		Default: &dto.RateLimitValues{Rpm: intPtr(10), Tpm: intPtr(0)},
		Models:  map[string]dto.RateLimitValues{"gpt-4o": {Tpm: intPtr(100)}},
	}))
	assert.Error(t, CheckRateLimitOverride(&dto.RateLimitOverride{
		Default: &dto.RateLimitValues{Rpm: intPtr(-1)},
	}))
	assert.Error(t, CheckRateLimitOverride(&dto.RateLimitOverride{
		Models: map[string]dto.RateLimitValues{"gpt-4o": {Tpm: intPtr(-100)}},
	}))
}
