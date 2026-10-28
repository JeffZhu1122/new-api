package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/volcark"
	"github.com/QuantumNous/new-api/setting/asset_library_setting"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Seedance 2.x requests on Ark's native video task API may reference images,
// videos and audio by URL. With auto_upload_references on, the gateway adds
// those references to the caller's virtual avatar (AIGC) library first and
// sends them as asset://<id>, so callers need not upload them themselves.
// Uploads go through the same handling as /volc (ownership, limits, upload
// rate, task log) and Ark still reviews each one: a reference Ark refuses fails
// the request with Ark's reason. One user's same URL is uploaded once and
// reused afterwards.

const (
	// The group every user's automatic uploads go to.
	AssetLibraryAutoUploadGroupName = "auto-upload"
	assetLibraryAutoUploadWait      = 120 * time.Second
	assetLibraryAutoUploadPoll      = 2 * time.Second
)

var (
	// Content item types of Ark's video task API and the asset type each becomes.
	assetLibraryAutoUploadTypes = map[string]string{"image_url": "Image", "video_url": "Video", "audio_url": "Audio"}
	// References per request that Ark's video task API takes, by asset type:
	// the Seedance 2.0 models up to 9 images, 3 videos and 3 audio clips,
	// Seedance 2.5 up to 30, 10 and 10. A request over them is left as it is,
	// so Ark answers with its own error and nothing is uploaded for it.
	seedance20ReferenceLimits = map[string]int{"Image": 9, "Video": 3, "Audio": 3}
	seedanceReferenceLimits   = map[string]int{"Image": 30, "Video": 10, "Audio": 10}
)

// AssetLibraryAutoUpload is the outcome of preparing a request: the rewritten
// body when references were uploaded, or the error to answer with.
type AssetLibraryAutoUpload struct {
	Body       []byte
	StatusCode int
	MessageID  string
	Params     map[string]any
}

type assetLibraryAutoUploadRef struct {
	path      string
	url       string
	assetType string
}

// AutoUploadAssetLibraryReferences uploads the request's URL references with a
// reference_* role when the model is Seedance 2.x and the feature is on. It
// returns an empty result when there is nothing to do.
func AutoUploadAssetLibraryReferences(ctx context.Context, caller AssetLibraryCaller, modelName string, body []byte, requestId string) AssetLibraryAutoUpload {
	setting := asset_library_setting.GetSetting()
	lowerModel := strings.ToLower(modelName)
	if !setting.Enabled || !setting.AutoUploadReferences || !setting.UserGroupAllowed(caller.UserGroup) || !strings.Contains(lowerModel, "seedance-2") {
		return AssetLibraryAutoUpload{}
	}
	content := gjson.GetBytes(body, "content")
	if !content.IsArray() {
		return AssetLibraryAutoUpload{}
	}
	var refs []assetLibraryAutoUploadRef
	counts := map[string]int{}
	content.ForEach(func(index, item gjson.Result) bool {
		field := item.Get("type").String()
		assetType, ok := assetLibraryAutoUploadTypes[field]
		if !ok || !strings.HasPrefix(item.Get("role").String(), "reference_") {
			return true
		}
		counts[assetType]++
		url := strings.TrimSpace(item.Get(field + ".url").String())
		lower := strings.ToLower(url)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			refs = append(refs, assetLibraryAutoUploadRef{path: fmt.Sprintf("content.%d.%s.url", index.Int(), field), url: url, assetType: assetType})
		}
		return true
	})
	if len(refs) == 0 {
		return AssetLibraryAutoUpload{}
	}
	limits := seedanceReferenceLimits
	if strings.Contains(lowerModel, "seedance-2-0") {
		limits = seedance20ReferenceLimits
	}
	for assetType, count := range counts {
		if count > limits[assetType] {
			return AssetLibraryAutoUpload{}
		}
	}

	group, reason := assetLibraryAutoUploadGroup(ctx, caller, requestId)
	if reason != "" {
		return assetLibraryAutoUploadFailed(1, reason)
	}
	ids := make([]string, len(refs))
	for i, ref := range refs {
		// The name lets the same URL be found again; prompts never refer to
		// assets by name.
		digest := sha256.Sum256([]byte(ref.url))
		name := "auto-" + hex.EncodeToString(digest[:])[:16]
		if existing, err := model.GetAssetLibraryAssetInGroupByName(group.Id, name); err == nil {
			ids[i] = existing.RemoteId
			continue
		}
		var created volcark.CreatedResource
		payload := map[string]any{"GroupId": group.RemoteId, "URL": ref.url, "AssetType": ref.assetType, "Name": name}
		if reason := callAssetLibraryAction(ctx, caller, "CreateAsset", payload, requestId, &created); reason != "" {
			return assetLibraryAutoUploadFailed(i+1, reason)
		}
		ids[i] = created.Id
	}

	deadline := time.Now().Add(assetLibraryAutoUploadWait)
	for i, id := range ids {
		for {
			var asset volcark.Asset
			if reason := callAssetLibraryAction(ctx, caller, "GetAsset", map[string]any{"Id": id}, requestId, &asset); reason != "" {
				return assetLibraryAutoUploadFailed(i+1, reason)
			}
			if asset.Status == model.AssetLibraryAssetActive {
				break
			}
			if asset.Status == model.AssetLibraryAssetFailed {
				reason := "processing failed"
				if asset.Error != nil {
					reason = strings.Trim(asset.Error.Code+": "+asset.Error.Message, ": ")
				}
				return assetLibraryAutoUploadFailed(i+1, reason)
			}
			if time.Now().Add(assetLibraryAutoUploadPoll).After(deadline) || ctx.Err() != nil {
				return AssetLibraryAutoUpload{StatusCode: http.StatusGatewayTimeout, MessageID: i18n.MsgAssetLibraryAutoUploadPending, Params: map[string]any{"Index": i + 1}}
			}
			time.Sleep(assetLibraryAutoUploadPoll)
		}
	}

	rewritten := body
	for i, ref := range refs {
		next, err := sjson.SetBytes(rewritten, ref.path, "asset://"+ids[i])
		if err != nil {
			return assetLibraryAutoUploadFailed(i+1, err.Error())
		}
		rewritten = next
	}
	return AssetLibraryAutoUpload{Body: rewritten}
}

func assetLibraryAutoUploadFailed(index int, reason string) AssetLibraryAutoUpload {
	return AssetLibraryAutoUpload{StatusCode: http.StatusBadRequest, MessageID: i18n.MsgAssetLibraryAutoUploadFailed, Params: map[string]any{"Index": index, "Reason": reason}}
}

// assetLibraryAutoUploadGroup returns the caller's group for automatic
// uploads, creating it on first use.
func assetLibraryAutoUploadGroup(ctx context.Context, caller AssetLibraryCaller, requestId string) (*model.AssetLibraryGroup, string) {
	if group, err := model.GetUserAssetLibraryGroupByName(caller.UserId, volcark.GroupTypeAIGC, AssetLibraryAutoUploadGroupName); err == nil {
		return group, ""
	}
	var created volcark.CreatedResource
	payload := map[string]any{"Name": AssetLibraryAutoUploadGroupName, "Description": "References added automatically from video requests", "GroupType": volcark.GroupTypeAIGC}
	if reason := callAssetLibraryAction(ctx, caller, "CreateAssetGroup", payload, requestId, &created); reason != "" {
		return nil, reason
	}
	group, err := model.GetUserAssetLibraryGroup(caller.UserId, created.Id)
	if err != nil {
		return nil, err.Error()
	}
	return group, ""
}

// callAssetLibraryAction runs an action as the caller through the /volc
// handling and decodes its Result into out, or returns Ark's or the gateway's
// error as "Code: Message".
func callAssetLibraryAction(ctx context.Context, caller AssetLibraryCaller, action string, payload map[string]any, requestId string, out any) string {
	encoded, err := common.Marshal(payload)
	if err != nil {
		return err.Error()
	}
	response := HandleAssetLibraryAction(ctx, caller, action, volcark.APIVersion, encoded, requestId)
	decodeErr := (&volcark.Response{StatusCode: response.StatusCode, Body: response.Body}).Decode(action, out)
	if decodeErr == nil {
		return ""
	}
	var arkErr *volcark.Error
	if errors.As(decodeErr, &arkErr) {
		return strings.Trim(arkErr.Code+": "+arkErr.Message, ": ")
	}
	return decodeErr.Error()
}
