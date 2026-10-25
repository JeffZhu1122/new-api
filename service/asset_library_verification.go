package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/volcark"
	"github.com/QuantumNous/new-api/setting/asset_library_setting"

	"gorm.io/gorm"
)

// Accounts Ark has not allowlisted create real-person groups through face
// verification: CreateVisualValidateSession returns a one-time H5 link where
// the person passes a liveness check, after which Ark creates the group and
// GetVisualValidateResult returns its id for the session's BytedToken. The
// gateway records every verification against the user who started it, so only
// they can collect the group, and records the group under them as soon as it
// is known: from their own GetVisualValidateResult, from the callback page the
// H5 link returns to, or from the background sync. Without that a person who
// passed but whose caller never asked again would leave an unowned group.

const (
	// BytedToken lifetime documented by Ark.
	assetLibraryVerificationTTL = 30 * time.Minute
	// Ark may report the group shortly after the token expires; keep asking a
	// little longer before giving up.
	assetLibraryVerificationGrace = 2 * time.Minute
	// How often the background sync asks Ark about one pending verification.
	assetLibraryVerificationRecheck = 30 * time.Second
	assetLibraryVerificationBatch   = 20
	assetLibraryCallbackURLMaxLen   = 1024
	assetLibraryBytedTokenMaxLen    = 128

	assetLibraryCodeVerificationFailed = "VerificationFailed"

	// AssetLibraryVerificationPassed is the resultCode the H5 page appends to
	// the callback URL when the person passed.
	AssetLibraryVerificationPassed = "10000"
)

// Reasons a verification ends without a group: fixed sentences that are also
// frontend locale keys.
const (
	assetLibraryVerificationExpired     = "Face verification was not completed within 30 minutes."
	assetLibraryVerificationNotPassed   = "Face verification did not pass."
	assetLibraryVerificationAccountGone = "The Volcengine account of this verification was removed."
)

func (r *assetLibraryRequest) createVerification() AssetLibraryResponse {
	var request struct {
		CallbackURL string `json:"CallbackURL"`
		// Gateway additions: the group is given this name and description once
		// Ark has created it.
		Name        string `json:"Name"`
		Description string `json:"Description"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	callback := strings.TrimSpace(request.CallbackURL)
	parsed, err := url.Parse(callback)
	if err != nil || len(callback) > assetLibraryCallbackURLMaxLen || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "CallbackURL is required and must be an http(s) URL of at most 1024 characters.")
	}
	if !validAssetLibraryName(request.Name, false) {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "Name must be at most 64 characters.")
	}
	if utf8.RuneCountInString(request.Description) > assetLibraryDescriptionMaxRunes {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "Description must be at most 300 characters.")
	}
	if limit := asset_library_setting.GetSetting().VerifySessionsPerHour; limit > 0 {
		count, err := model.CountAssetLibraryVerificationsSince(r.caller.UserId, time.Now().Add(-time.Hour).Unix())
		if err != nil {
			return r.internalError(err)
		}
		if count >= int64(limit) {
			return r.fail(http.StatusTooManyRequests, assetLibraryCodeRequestLimit, fmt.Sprintf("You can start at most %d face verifications per hour.", limit))
		}
	}
	if response, ok := r.checkUserGroupLimit(); !ok {
		return response
	}
	account, response, ok := r.accountForNewGroup(volcark.GroupTypeLivenessFace, true)
	if !ok {
		return response
	}
	arkResponse, response, ok := r.forward(account, map[string]any{"CallbackURL": callback})
	if !ok {
		return response
	}
	var session struct {
		BytedToken string `json:"BytedToken"`
	}
	if arkResponse.Decode(r.action, &session) != nil || session.BytedToken == "" {
		return passThrough(arkResponse)
	}
	verification := &model.AssetLibraryVerification{
		UserId:      r.caller.UserId,
		AccountId:   account.Id,
		BytedToken:  session.BytedToken,
		Name:        strings.TrimSpace(request.Name),
		Description: request.Description,
	}
	if err := model.CreateAssetLibraryVerification(verification, r.caller.UserGroup); err != nil {
		return r.internalError(err)
	}
	return passThrough(arkResponse)
}

func (r *assetLibraryRequest) getVerificationResult() AssetLibraryResponse {
	var request struct {
		BytedToken string `json:"BytedToken"`
	}
	if response, ok := r.decode(&request); !ok {
		return response
	}
	if strings.TrimSpace(request.BytedToken) == "" {
		return r.fail(http.StatusBadRequest, assetLibraryCodeInvalidParam, "BytedToken is required.")
	}
	verification, err := model.GetUserAssetLibraryVerification(r.caller.UserId, request.BytedToken)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.fail(http.StatusNotFound, assetLibraryCodeNotFound, "The verification does not exist.")
	}
	if err != nil {
		return r.internalError(err)
	}
	switch verification.Status {
	case model.AssetLibraryVerificationSucceeded:
		return r.respond(http.StatusOK, "", map[string]any{"GroupId": verification.RemoteGroupId}, nil)
	case model.AssetLibraryVerificationFailed:
		return r.fail(http.StatusBadRequest, assetLibraryCodeVerificationFailed, verification.FailReason)
	}
	account, err := model.GetAssetLibraryAccount(verification.AccountId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		failAssetLibraryVerification(verification, assetLibraryVerificationAccountGone, "")
		return r.fail(http.StatusBadRequest, assetLibraryCodeVerificationFailed, assetLibraryVerificationAccountGone)
	}
	if err != nil {
		return r.internalError(err)
	}
	arkResponse, response, ok := r.forward(account, map[string]any{"BytedToken": verification.BytedToken})
	if !ok {
		return response
	}
	var result struct {
		GroupId string `json:"GroupId"`
	}
	if arkResponse.Decode(r.action, &result) == nil && result.GroupId != "" {
		if err := recordVerifiedAssetLibraryGroup(r.ctx, account, verification, result.GroupId); err != nil {
			return r.internalError(err)
		}
		return passThrough(arkResponse)
	}
	settlePendingAssetLibraryVerification(verification)
	return passThrough(arkResponse)
}

// recordVerifiedAssetLibraryGroup records the group Ark created for the
// verification under its user, after giving it the name and description the
// user asked for.
func recordVerifiedAssetLibraryGroup(ctx context.Context, account *model.AssetLibraryAccount, verification *model.AssetLibraryVerification, remoteGroupId string) error {
	client := AssetLibraryClient(account)
	if verification.Name != "" || verification.Description != "" {
		payload := map[string]any{"Id": remoteGroupId, "ProjectName": account.ProjectName}
		if verification.Name != "" {
			payload["Name"] = verification.Name
		}
		if verification.Description != "" {
			payload["Description"] = verification.Description
		}
		if err := client.Call(ctx, "UpdateAssetGroup", payload, nil); err != nil {
			common.SysError(common.LogText("asset library could not name verified group %s: %s", remoteGroupId, err.Error()))
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	group := &model.AssetLibraryGroup{
		UserId:           verification.UserId,
		AccountId:        account.Id,
		RemoteId:         remoteGroupId,
		GroupType:        volcark.GroupTypeLivenessFace,
		Name:             verification.Name,
		Description:      verification.Description,
		RemoteCreateTime: now,
		RemoteUpdateTime: now,
	}
	var remote volcark.AssetGroup
	err := client.Call(ctx, "GetAssetGroup", map[string]any{"Id": remoteGroupId, "ProjectName": account.ProjectName}, &remote)
	if err == nil && remote.Id == remoteGroupId {
		group.Name, group.Description = remote.Name, remote.Description
		group.RemoteCreateTime, group.RemoteUpdateTime = remote.CreateTime, remote.UpdateTime
	} else if err != nil {
		common.SysError(common.LogText("asset library could not read verified group %s: %s", remoteGroupId, err.Error()))
	}
	_, err = model.CompleteAssetLibraryVerification(verification, group)
	return err
}

func failAssetLibraryVerification(verification *model.AssetLibraryVerification, reason, resultCode string) {
	if _, err := model.FailAssetLibraryVerification(verification, reason, resultCode); err != nil {
		common.SysError(common.LogText("asset library could not end verification %d: %s", verification.Id, err.Error()))
	}
}

// settlePendingAssetLibraryVerification notes a check that found no group
// yet, and gives up on a verification past the token's lifetime.
func settlePendingAssetLibraryVerification(verification *model.AssetLibraryVerification) {
	if time.Since(time.Unix(verification.CreatedTime, 0)) > assetLibraryVerificationTTL+assetLibraryVerificationGrace {
		failAssetLibraryVerification(verification, assetLibraryVerificationExpired, "")
		return
	}
	if err := model.TouchAssetLibraryVerification(verification); err != nil {
		common.SysError(common.LogText("asset library could not update verification %d: %s", verification.Id, err.Error()))
	}
}

// checkAssetLibraryVerification asks Ark whether the verification produced a
// group and records it. resultCode is what the H5 page reported, if anything:
// any code but a pass ends the verification when Ark has no group for it.
func checkAssetLibraryVerification(ctx context.Context, verification *model.AssetLibraryVerification, resultCode string) {
	account, err := model.GetAssetLibraryAccount(verification.AccountId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		failAssetLibraryVerification(verification, assetLibraryVerificationAccountGone, resultCode)
		return
	}
	if err != nil {
		common.SysError(common.LogText("asset library account %d for verification %d: %s", verification.AccountId, verification.Id, err.Error()))
		return
	}
	var result struct {
		GroupId string `json:"GroupId"`
	}
	err = AssetLibraryClient(account).Call(ctx, "GetVisualValidateResult", map[string]any{"BytedToken": verification.BytedToken, "ProjectName": account.ProjectName}, &result)
	if err == nil && result.GroupId != "" {
		if err := recordVerifiedAssetLibraryGroup(ctx, account, verification, result.GroupId); err != nil {
			common.SysError(common.LogText("asset library could not record verified group %s: %s", result.GroupId, err.Error()))
		}
		return
	}
	// Ark answers with an error until the person has passed; only failures
	// to reach it are worth a log line.
	var arkErr *volcark.Error
	if err != nil && !errors.As(err, &arkErr) {
		common.SysError(common.LogText("asset library could not check verification %d: %s", verification.Id, err.Error()))
	}
	if resultCode != "" && resultCode != AssetLibraryVerificationPassed {
		failAssetLibraryVerification(verification, assetLibraryVerificationNotPassed, resultCode)
		return
	}
	settlePendingAssetLibraryVerification(verification)
}

// HandleAssetLibraryVerificationCallback takes the result the H5 page
// appended to the gateway's callback page, which the person's phone opens
// without signing in. Unknown or settled tokens are ignored, so the caller
// learns nothing about them.
func HandleAssetLibraryVerificationCallback(ctx context.Context, bytedToken, resultCode string) {
	if bytedToken == "" || len(bytedToken) > assetLibraryBytedTokenMaxLen {
		return
	}
	verification, err := model.GetAssetLibraryVerificationByToken(bytedToken)
	if err != nil || verification.Status != model.AssetLibraryVerificationPending {
		return
	}
	checkAssetLibraryVerification(ctx, verification, resultCode)
}

// SyncAssetLibraryVerifications asks Ark about pending verifications nobody
// has checked lately.
func SyncAssetLibraryVerifications(ctx context.Context) {
	verifications, err := model.ListPendingAssetLibraryVerifications(time.Now().Add(-assetLibraryVerificationRecheck).Unix(), assetLibraryVerificationBatch)
	if err != nil {
		common.SysError(common.LogText("asset library sync could not list pending verifications: %s", err.Error()))
		return
	}
	for _, verification := range verifications {
		checkAssetLibraryVerification(ctx, verification, "")
	}
}
