package ratio_setting

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// 分组×模型折扣：{分组: {模型名: 折扣}}，按请求实际使用的分组（UsingGroup）匹配，
// 与分组倍率、用户×模型折扣相乘叠加。模型名精确匹配优先，"*" 作兜底；未配置视为 1.0。
var groupModelDiscountMap = types.NewRWMap[string, map[string]float64]()

func GroupModelDiscount2JSONString() string {
	return groupModelDiscountMap.MarshalJSONString()
}

func UpdateGroupModelDiscountByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(groupModelDiscountMap, jsonStr)
}

func GetGroupModelDiscountCopy() map[string]map[string]float64 {
	return groupModelDiscountMap.ReadAll()
}

// MatchModelDiscount looks up a discount for modelName in a {model: discount}
// map, falling back to the "*" wildcard entry. The returned bool reports
// whether an entry matched; on a miss the discount is the neutral 1.
func MatchModelDiscount(discounts map[string]float64, modelName string) (float64, bool) {
	if discount, ok := discounts[modelName]; ok {
		return discount, true
	}
	if discount, ok := discounts["*"]; ok {
		return discount, true
	}
	return 1, false
}

// GetGroupModelDiscount returns the discount usingGroup gets on modelName,
// or 1 when none is configured.
func GetGroupModelDiscount(usingGroup string, modelName string) float64 {
	discounts, ok := groupModelDiscountMap.Get(usingGroup)
	if !ok {
		return 1
	}
	discount, _ := MatchModelDiscount(discounts, FormatMatchingModelName(modelName))
	return discount
}

func CheckGroupModelDiscount(jsonStr string) error {
	checkMap := make(map[string]map[string]float64)
	if err := common.UnmarshalJsonStr(jsonStr, &checkMap); err != nil {
		return err
	}
	for group, discounts := range checkMap {
		if err := CheckUserModelDiscountMap(discounts); err != nil {
			return fmt.Errorf("group %s: %w", group, err)
		}
	}
	return nil
}

// CheckUserModelDiscountMap validates a {model: discount} map. A discount of 0
// is rejected so free access stays an explicit GroupRatio/model-ratio decision,
// and values above 10 are rejected to catch percentage-style typos like 80.
func CheckUserModelDiscountMap(discounts map[string]float64) error {
	for name, discount := range discounts {
		if math.IsNaN(discount) || discount <= 0 || discount > 10 {
			return fmt.Errorf("model discount must be in (0, 10]: %s", name)
		}
	}
	return nil
}
