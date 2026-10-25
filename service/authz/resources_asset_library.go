package authz

const ResourceAssetLibrary = "asset_library"

var (
	AssetLibraryRead  = Permission{Resource: ResourceAssetLibrary, Action: ActionRead}
	AssetLibraryWrite = Permission{Resource: ResourceAssetLibrary, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceAssetLibrary,
		LabelKey: "Asset Library",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "View every user's asset library",
				DescriptionKey: "List all users' asset groups and assets on the Asset Library page.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Manage every user's asset library",
				DescriptionKey: "Delete any user's asset groups and assets on Volcengine.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
		},
	})
}
