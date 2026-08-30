package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 契约:realtime WSS 预扣必须使用 ModelPriceHelper 装配好的 relayInfo.PriceData
// (含 auto_group、分组特殊倍率与模型/用户折扣),而不是从 ratio_setting 重算倍率。
func TestPreWssConsumeQuotaUsesAssembledPriceData(t *testing.T) {
	truncate(t)
	seedUser(t, 21, 1_000_000)
	seedToken(t, 31, 21, "wss-key-1", 1_000_000)

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	savedModelRatio := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatio))
	})
	// 全局配置故意与 PriceData 不一致,预扣若走重算路径就会得出 1000*999*2
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"wss-test-model":999}`))

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		UserId:          21,
		TokenId:         31,
		TokenKey:        "wss-key-1",
		UserGroup:       "default",
		UsingGroup:      "default",
		OriginModelName: "wss-test-model",
	}
	relayInfo.PriceData = types.PriceData{
		ModelRatio:     15,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.5},
	}

	usage := &dto.RealtimeUsage{}
	usage.InputTokenDetails.TextTokens = 1000

	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, usage))

	quota, err := model.GetUserQuota(21, true)
	require.NoError(t, err)
	// 1000 tokens * 模型倍率 15 * 分组倍率 0.5 = 7500
	assert.Equal(t, 1_000_000-7500, quota)
}
