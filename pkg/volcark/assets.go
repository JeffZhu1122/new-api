package volcark

import "context"

// Asset group types: AIGC holds virtual avatars, LivenessFace holds one
// verified real person.
const (
	GroupTypeAIGC         = "AIGC"
	GroupTypeLivenessFace = "LivenessFace"
)

type AssetGroup struct {
	Id          string `json:"Id"`
	Name        string `json:"Name"`
	Description string `json:"Description"`
	GroupType   string `json:"GroupType"`
	ProjectName string `json:"ProjectName"`
	CreateTime  string `json:"CreateTime"`
	UpdateTime  string `json:"UpdateTime"`
}

type ListAssetGroupsRequest struct {
	Filter      ListAssetGroupsFilter `json:"Filter"`
	MaxResults  int                   `json:"MaxResults,omitempty"`
	NextToken   string                `json:"NextToken,omitempty"`
	ProjectName string                `json:"ProjectName,omitempty"`
}

type ListAssetGroupsFilter struct {
	GroupType string   `json:"GroupType"`
	GroupIds  []string `json:"GroupIds,omitempty"`
	Name      string   `json:"Name,omitempty"`
}

type ListAssetGroupsResult struct {
	Items     []AssetGroup `json:"Items"`
	NextToken string       `json:"NextToken"`
}

func (c *Client) ListAssetGroups(ctx context.Context, request ListAssetGroupsRequest) (*ListAssetGroupsResult, error) {
	var result ListAssetGroupsResult
	if err := c.Call(ctx, "ListAssetGroups", request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
