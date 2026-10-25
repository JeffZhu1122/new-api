package service

import (
	"bytes"
	"net/http"
	"regexp"
	"slices"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/asset_library_setting"

	"github.com/tidwall/gjson"
)

// Any API key of a Volcengine project can use any asset ID of that project,
// so the gateway checks every asset:// reference a request carries before it
// is relayed: each must be an Active asset of the caller's own library (or
// any registered asset for an administrator), all from one account, and the
// request may only route to channels bound to that account.

var (
	assetReferencePattern = regexp.MustCompile(`(?i)asset://([A-Za-z0-9_-]*)`)
	// Anything that could hide "asset://" in a JSON string: the scheme itself,
	// a \uXXXX escape or an escaped slash.
	assetReferenceHint = regexp.MustCompile(`(?i)asset:|\\u|\\/`)
)

const maxAssetReferences = 100

// AssetLibraryReferenceCheck is the outcome of checking a request's asset
// references. A non-zero StatusCode refuses the request with the message;
// otherwise a non-zero AccountId restricts routing to that account's channels.
type AssetLibraryReferenceCheck struct {
	AccountId  int
	StatusCode int
	MessageID  string
	Params     map[string]any
}

// findAssetReferences returns the asset IDs referenced anywhere in the body,
// "" for a reference without a valid ID. JSON bodies are read through their
// decoded string values, so escapes cannot hide a reference.
func findAssetReferences(body []byte) []string {
	if !assetReferenceHint.Match(body) {
		return nil
	}
	var ids []string
	collect := func(text string) {
		for _, match := range assetReferencePattern.FindAllStringSubmatch(text, -1) {
			if !slices.Contains(ids, match[1]) {
				ids = append(ids, match[1])
			}
		}
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') && gjson.ValidBytes(trimmed) {
		var walk func(value gjson.Result)
		walk = func(value gjson.Result) {
			if value.IsObject() || value.IsArray() {
				value.ForEach(func(_, item gjson.Result) bool {
					walk(item)
					return true
				})
				return
			}
			if value.Type == gjson.String {
				collect(value.String())
			}
		}
		walk(gjson.ParseBytes(trimmed))
		return ids
	}
	collect(string(body))
	return ids
}

// CheckAssetLibraryReferences checks the asset:// references in a request
// body sent by userId.
func CheckAssetLibraryReferences(userId int, body []byte) AssetLibraryReferenceCheck {
	ids := findAssetReferences(body)
	if len(ids) == 0 {
		return AssetLibraryReferenceCheck{}
	}
	setting := asset_library_setting.GetSetting()
	if !setting.Enabled {
		if setting.BlockAssetRefsWhenDisabled {
			return AssetLibraryReferenceCheck{StatusCode: http.StatusForbidden, MessageID: i18n.MsgAssetLibraryReferencesBlocked}
		}
		return AssetLibraryReferenceCheck{}
	}
	if len(ids) > maxAssetReferences {
		return AssetLibraryReferenceCheck{StatusCode: http.StatusBadRequest, MessageID: i18n.MsgAssetLibraryTooManyReferences, Params: map[string]any{"Max": maxAssetReferences}}
	}
	for _, id := range ids {
		if id == "" {
			return AssetLibraryReferenceCheck{StatusCode: http.StatusBadRequest, MessageID: i18n.MsgAssetLibraryReferenceInvalid}
		}
	}
	assets, err := model.GetAssetLibraryAssetsByRemoteIds(ids)
	if err != nil {
		return AssetLibraryReferenceCheck{StatusCode: http.StatusInternalServerError, MessageID: i18n.MsgDatabaseError}
	}
	byId := make(map[string]*model.AssetLibraryAsset, len(assets))
	for _, asset := range assets {
		byId[asset.RemoteId] = asset
	}
	accountId := 0
	isAdmin, adminChecked := false, false
	for _, id := range ids {
		asset := byId[id]
		if asset != nil && asset.UserId != userId && !adminChecked {
			isAdmin, adminChecked = model.IsAdmin(userId), true
		}
		if asset == nil || (asset.UserId != userId && !isAdmin) {
			return AssetLibraryReferenceCheck{StatusCode: http.StatusForbidden, MessageID: i18n.MsgAssetLibraryAssetNotOwned, Params: map[string]any{"Id": id}}
		}
		if asset.Status != model.AssetLibraryAssetActive {
			return AssetLibraryReferenceCheck{StatusCode: http.StatusBadRequest, MessageID: i18n.MsgAssetLibraryAssetNotReady, Params: map[string]any{"Id": id, "Status": asset.Status}}
		}
		if accountId != 0 && asset.AccountId != accountId {
			return AssetLibraryReferenceCheck{StatusCode: http.StatusBadRequest, MessageID: i18n.MsgAssetLibraryMixedAccounts}
		}
		accountId = asset.AccountId
	}
	return AssetLibraryReferenceCheck{AccountId: accountId}
}
