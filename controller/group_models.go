package controller

import (
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// UserGroupModels lists the models one group the user may select can call.
type UserGroupModels struct {
	Group string `json:"group"`
	Desc  string `json:"desc"`
	// Ratio is the user's group ratio, or "auto" for the auto group.
	Ratio any `json:"ratio"`
	// AutoGroups is set for the auto group only: the groups it tries, in order.
	AutoGroups []string `json:"auto_groups,omitempty"`
	Models     []string `json:"models"`
}

// userSelectableGroups lists the groups a user in userGroup may select, with
// the same rules as GetUserGroups: groups that have a ratio and are usable,
// sorted by name, then auto when usable. Models are left empty.
func userSelectableGroups(userGroup string) []UserGroupModels {
	usableGroups := service.GetUserUsableGroups(userGroup)
	result := make([]UserGroupModels, 0, len(usableGroups))
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		desc, ok := usableGroups[groupName]
		if !ok {
			continue
		}
		result = append(result, UserGroupModels{
			Group: groupName,
			Desc:  desc,
			Ratio: service.GetUserGroupRatio(userGroup, groupName),
		})
	}
	slices.SortFunc(result, func(a, b UserGroupModels) int {
		return strings.Compare(a.Group, b.Group)
	})
	if _, ok := usableGroups["auto"]; ok {
		result = append(result, UserGroupModels{
			Group:      "auto",
			Desc:       setting.GetUsableGroupDescription("auto"),
			Ratio:      "auto",
			AutoGroups: service.GetUserAutoGroup(userGroup),
		})
	}
	return result
}

// GetUserGroupModels returns every group the user may select with the models
// it can call. Groups follow GetUserGroups; models follow /v1/models, so a
// model without a price is hidden unless self-use mode or the user's
// accept-unset-ratio setting allows it.
func GetUserGroupModels(c *gin.Context) {
	userId := c.GetInt("id")
	userGroup, err := model.GetUserGroup(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	acceptUnsetRatioModel := operation_setting.SelfUseModeEnabled
	if !acceptUnsetRatioModel {
		userSettings, _ := model.GetUserSetting(userId, false)
		acceptUnsetRatioModel = userSettings.AcceptUnsetRatioModel
	}

	result := userSelectableGroups(userGroup)
	for i := range result {
		groups := []string{result[i].Group}
		if result[i].Group == "auto" {
			groups = result[i].AutoGroups
		}
		models := make([]string, 0)
		for _, modelName := range service.GetGroupsEnabledModels(groups) {
			if acceptUnsetRatioModel || helper.HasModelBillingConfig(modelName) {
				models = append(models, modelName)
			}
		}
		slices.Sort(models)
		result[i].Models = models
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}
