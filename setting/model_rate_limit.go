package setting

import (
	"fmt"
	"math"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// ModelRateLimitEnabled 是模型 RPM/TPM 限流的总开关，
// ModelRateLimitRules 存放分组/模型维度的规则；用户级覆盖存于 user_extend 表。
var ModelRateLimitEnabled = false

// ModelRateLimitGroupRules 单个分组的限流规则。
type ModelRateLimitGroupRules struct {
	Default *dto.RateLimitValues           `json:"default,omitempty"`
	Models  map[string]dto.RateLimitValues `json:"models,omitempty"`
}

// ModelRateLimitRulesConfig 全局限流规则：分组×模型 > 分组默认 > 全局模型 > 全局默认。
type ModelRateLimitRulesConfig struct {
	Default *dto.RateLimitValues                `json:"default,omitempty"`
	Groups  map[string]ModelRateLimitGroupRules `json:"groups,omitempty"`
	Models  map[string]dto.RateLimitValues      `json:"models,omitempty"`
}

var (
	modelRateLimitRules ModelRateLimitRulesConfig
	modelRateLimitMutex sync.RWMutex
)

func ModelRateLimitRules2JSONString() string {
	modelRateLimitMutex.RLock()
	defer modelRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(modelRateLimitRules)
	if err != nil {
		common.SysLog("error marshalling model rate limit rules: " + err.Error())
		return "{}"
	}
	return string(jsonBytes)
}

func UpdateModelRateLimitRulesByJSONString(jsonStr string) error {
	modelRateLimitMutex.Lock()
	defer modelRateLimitMutex.Unlock()

	modelRateLimitRules = ModelRateLimitRulesConfig{}
	if jsonStr == "" {
		return nil
	}
	return common.UnmarshalJsonStr(jsonStr, &modelRateLimitRules)
}

// CheckModelRateLimitRules validates the admin-supplied rules JSON before it is
// persisted: every rpm/tpm must be either unset or within [0, int32 max].
func CheckModelRateLimitRules(jsonStr string) error {
	if jsonStr == "" {
		return nil
	}
	var rules ModelRateLimitRulesConfig
	if err := common.UnmarshalJsonStr(jsonStr, &rules); err != nil {
		return err
	}
	if err := checkRateLimitValues("default", rules.Default); err != nil {
		return err
	}
	for model, values := range rules.Models {
		v := values
		if err := checkRateLimitValues("models."+model, &v); err != nil {
			return err
		}
	}
	for group, groupRules := range rules.Groups {
		if err := checkRateLimitValues("groups."+group+".default", groupRules.Default); err != nil {
			return err
		}
		for model, values := range groupRules.Models {
			v := values
			if err := checkRateLimitValues("groups."+group+".models."+model, &v); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckRateLimitOverride validates a per-user override before it is persisted
// to the user_extend table.
func CheckRateLimitOverride(override *dto.RateLimitOverride) error {
	if override == nil {
		return nil
	}
	if err := checkRateLimitValues("default", override.Default); err != nil {
		return err
	}
	for model, values := range override.Models {
		v := values
		if err := checkRateLimitValues("models."+model, &v); err != nil {
			return err
		}
	}
	return nil
}

func checkRateLimitValues(path string, values *dto.RateLimitValues) error {
	if values == nil {
		return nil
	}
	if values.Rpm != nil && (*values.Rpm < 0 || *values.Rpm > math.MaxInt32) {
		return fmt.Errorf("%s has invalid rpm %d, must be within [0, 2147483647]", path, *values.Rpm)
	}
	if values.Tpm != nil && (*values.Tpm < 0 || *values.Tpm > math.MaxInt32) {
		return fmt.Errorf("%s has invalid tpm %d, must be within [0, 2147483647]", path, *values.Tpm)
	}
	return nil
}

// ResolveModelRateLimit resolves the effective RPM/TPM for one request. RPM and
// TPM fall back independently through: user override (model, then default) >
// group rules (model, then default) > global (model, then default). A missing
// value everywhere, or an explicit 0, means unlimited.
func ResolveModelRateLimit(group string, model string, userOverride *dto.RateLimitOverride) (rpm int, tpm int) {
	modelRateLimitMutex.RLock()
	defer modelRateLimitMutex.RUnlock()

	candidates := make([]*dto.RateLimitValues, 0, 6)
	if userOverride != nil {
		if v, ok := userOverride.Models[model]; ok && model != "" {
			values := v
			candidates = append(candidates, &values)
		}
		candidates = append(candidates, userOverride.Default)
	}
	if groupRules, ok := modelRateLimitRules.Groups[group]; ok {
		if v, ok := groupRules.Models[model]; ok && model != "" {
			values := v
			candidates = append(candidates, &values)
		}
		candidates = append(candidates, groupRules.Default)
	}
	if v, ok := modelRateLimitRules.Models[model]; ok && model != "" {
		values := v
		candidates = append(candidates, &values)
	}
	candidates = append(candidates, modelRateLimitRules.Default)

	rpmSet, tpmSet := false, false
	for _, values := range candidates {
		if values == nil {
			continue
		}
		if !rpmSet && values.Rpm != nil {
			rpm = *values.Rpm
			rpmSet = true
		}
		if !tpmSet && values.Tpm != nil {
			tpm = *values.Tpm
			tpmSet = true
		}
		if rpmSet && tpmSet {
			break
		}
	}
	return rpm, tpm
}
