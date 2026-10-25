package middleware

import (
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// AssetLibraryAutoUpload hands the URL references of a Seedance 2.x request on
// Ark's native video task route to the asset library
// (asset_library_setting.auto_upload_references) and swaps the request body
// for one that references the uploads as asset://. It runs before the task
// plugin decodes the request, so the plugin and the asset:// check in
// Distribute both see the rewritten body.
func AssetLibraryAutoUpload() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || !strings.HasSuffix(c.Request.URL.Path, "/contents/generations/tasks") {
			return
		}
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return
		}
		body, err := storage.Bytes()
		if err != nil {
			return
		}
		caller := service.AssetLibraryCaller{UserId: c.GetInt("id"), UserGroup: common.GetContextKeyString(c, constant.ContextKeyUserGroup)}
		upload := service.AutoUploadAssetLibraryReferences(c.Request.Context(), caller, gjson.GetBytes(body, "model").String(), body, c.GetString(common.RequestIdKey))
		if upload.StatusCode != 0 {
			abortWithOpenAiMessage(c, upload.StatusCode, i18n.T(c, upload.MessageID, upload.Params))
			return
		}
		if upload.Body == nil {
			return
		}
		newStorage, err := common.CreateBodyStorage(upload.Body)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, err.Error())
			return
		}
		_ = storage.Close()
		c.Set(common.KeyBodyStorage, newStorage)
		c.Set(common.KeyRequestBody, nil)
		c.Request.Body = io.NopCloser(newStorage)
		c.Request.ContentLength = int64(len(upload.Body))
	}
}
