package middleware

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// AutoUploadAssetLibraryReferences hands the URL references of a Seedance 2.x
// request on Ark's native video task route to the asset library
// (asset_library_setting.auto_upload_references) once a channel is selected.
// They go to the account the channel is bound to, the only one whose keys can
// read them, and the request body then references them as asset://; a
// channel bound to no account, such as one of another platform, gets the URLs
// as they are. The plugin decodes the rewritten body again, and the request's
// retries stay on channels bound to that account.
func AutoUploadAssetLibraryReferences(c *gin.Context, channelId int) *dto.TaskError {
	if c.Request.Method != http.MethodPost || !strings.HasSuffix(c.Request.URL.Path, "/contents/generations/tasks") {
		return nil
	}
	accountId := model.GetChannelExtendSettings(channelId).AssetLibraryAccountId
	if accountId == 0 {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil
	}
	caller := service.AssetLibraryCaller{UserId: c.GetInt("id"), UserGroup: common.GetContextKeyString(c, constant.ContextKeyUserGroup)}
	upload := service.AutoUploadAssetLibraryReferences(c.Request.Context(), caller, accountId, gjson.GetBytes(body, "model").String(), body, c.GetString(common.RequestIdKey))
	if upload.StatusCode != 0 {
		return service.TaskErrorWrapperLocal(errors.New(i18n.T(c, upload.MessageID, upload.Params)), "asset_library_auto_upload_failed", upload.StatusCode)
	}
	if upload.Body == nil {
		return nil
	}
	newStorage, err := common.CreateBodyStorage(upload.Body)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "asset_library_auto_upload_failed", http.StatusInternalServerError)
	}
	_ = storage.Close()
	c.Set(common.KeyBodyStorage, newStorage)
	c.Set(common.KeyRequestBody, nil)
	c.Request.Body = io.NopCloser(newStorage)
	c.Request.ContentLength = int64(len(upload.Body))
	service.GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterAssetLibraryAccount, AssetLibraryAccountId: accountId})
	return RefreshTaskPluginRequest(c)
}
