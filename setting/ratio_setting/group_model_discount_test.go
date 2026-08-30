package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckGroupModelDiscount(t *testing.T) {
	cases := []struct {
		name    string
		jsonStr string
		wantErr bool
	}{
		{"valid discounts", `{"default":{"gpt-4o":0.5,"*":1}}`, false},
		{"upper boundary 10 allowed", `{"default":{"gpt-4o":10}}`, false},
		{"empty map allowed", `{}`, false},
		{"zero rejected", `{"default":{"gpt-4o":0}}`, true},
		{"negative rejected", `{"default":{"gpt-4o":-0.5}}`, true},
		{"above 10 rejected", `{"default":{"gpt-4o":80}}`, true},
		{"non-numeric rejected", `{"default":{"gpt-4o":"cheap"}}`, true},
		{"flat map rejected", `{"gpt-4o":0.5}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckGroupModelDiscount(tc.jsonStr)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMatchModelDiscount(t *testing.T) {
	discounts := map[string]float64{
		"gpt-4o": 0.5,
		"*":      0.25,
	}

	discount, matched := MatchModelDiscount(discounts, "gpt-4o")
	require.True(t, matched)
	assert.Equal(t, 0.5, discount, "exact match wins over wildcard")

	discount, matched = MatchModelDiscount(discounts, "claude-3-opus")
	require.True(t, matched)
	assert.Equal(t, 0.25, discount, "wildcard catches unlisted models")

	discount, matched = MatchModelDiscount(map[string]float64{"gpt-4o": 0.5}, "claude-3-opus")
	assert.False(t, matched)
	assert.Equal(t, float64(1), discount, "miss returns the neutral discount")

	discount, matched = MatchModelDiscount(nil, "gpt-4o")
	assert.False(t, matched)
	assert.Equal(t, float64(1), discount)
}

func TestGetGroupModelDiscount(t *testing.T) {
	saved := GroupModelDiscount2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupModelDiscountByJSONString(saved))
	})

	require.NoError(t, UpdateGroupModelDiscountByJSONString(
		`{"default":{"gpt-4o":0.5,"gpt-4-gizmo-*":0.25}}`))

	assert.Equal(t, 0.5, GetGroupModelDiscount("default", "gpt-4o"))
	assert.Equal(t, float64(1), GetGroupModelDiscount("default", "claude-3-opus"), "unlisted model in configured group")
	assert.Equal(t, float64(1), GetGroupModelDiscount("vip", "gpt-4o"), "unconfigured group")
	assert.Equal(t, 0.25, GetGroupModelDiscount("default", "gpt-4-gizmo-g-abc123"),
		"model name is normalized with FormatMatchingModelName before matching")
}
