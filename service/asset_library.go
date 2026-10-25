package service

import (
	"context"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/volcark"
	"github.com/QuantumNous/new-api/setting/asset_library_setting"
)

var assetLibraryHTTPClient = &http.Client{Timeout: 30 * time.Second}

// AssetLibraryClient returns the Ark OpenAPI client of a library account.
func AssetLibraryClient(account *model.AssetLibraryAccount) *volcark.Client {
	return volcark.NewClient(volcark.Credentials{
		AccessKeyID:     account.AccessKeyId,
		SecretAccessKey: account.SecretAccessKey,
		Region:          account.Region,
		Endpoint:        account.Endpoint,
	}, assetLibraryHTTPClient)
}

// TestAssetLibraryAccount lists one asset group of every group type the
// account enables, which checks the Access Key, its Ark asset permissions and
// the project in one call per type.
func TestAssetLibraryAccount(ctx context.Context, account *model.AssetLibraryAccount) error {
	groupTypes := make([]string, 0, 2)
	if account.AigcEnabled {
		groupTypes = append(groupTypes, volcark.GroupTypeAIGC)
	}
	if account.LivenessFaceEnabled {
		groupTypes = append(groupTypes, volcark.GroupTypeLivenessFace)
	}
	if len(groupTypes) == 0 {
		groupTypes = append(groupTypes, volcark.GroupTypeAIGC)
	}
	client := AssetLibraryClient(account)
	for _, groupType := range groupTypes {
		_, err := client.ListAssetGroups(ctx, volcark.ListAssetGroupsRequest{
			Filter:      volcark.ListAssetGroupsFilter{GroupType: groupType},
			MaxResults:  1,
			ProjectName: account.ProjectName,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

const (
	assetLibrarySyncInterval = 15 * time.Second
	assetLibrarySyncBatch    = 100
)

// StartAssetLibrarySync polls Ark for assets still being processed, so their
// status and upload tasks settle even when nobody reads them. It runs on the
// master node while the asset library is enabled.
func StartAssetLibrarySync() {
	go func() {
		ticker := time.NewTicker(assetLibrarySyncInterval)
		defer ticker.Stop()
		for range ticker.C {
			if !asset_library_setting.GetSetting().Enabled {
				continue
			}
			assets, err := model.ListProcessingAssetLibraryAssets(assetLibrarySyncBatch)
			if err != nil {
				common.SysError(common.LogText("asset library sync could not list processing assets: %s", err.Error()))
				continue
			}
			refreshAssetLibraryAssets(context.Background(), assets)
		}
	}()
}
