// Hand-maintained. `freshnessConfig` and `meta` are additionalProperties: true
// in the OpenAPI spec, which the codegen renders as empty objects that can
// never carry a value; they are normalized JSON strings here instead.

package assets

import (
	"context"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/client"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/enums"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

type AssetsDataSource struct {
	client *client.Client
}

var (
	_ datasource.DataSource              = &AssetsDataSource{}
	_ datasource.DataSourceWithConfigure = &AssetsDataSource{}
)

func (d *AssetsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_assets"
	_ = ctx
}

func (d *AssetsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	_ = ctx
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}
	d.client = c
}

func (d *AssetsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	_ = ctx
	resp.Schema = schema.Schema{
		Description:         "Data assets registered in Orchestra, optionally filtered. All matching pages are fetched.",
		MarkdownDescription: "Data assets registered in Orchestra, optionally filtered. All matching pages are fetched.",
		Attributes: map[string]schema.Attribute{
			"asset_ids": schema.StringAttribute{
				Optional:    true,
				Description: "Comma-separated asset IDs to restrict the result to.",
			},
			"asset_type": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by asset type.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.AssetTypes...)},
			},
			"database_name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by database name.",
			},
			"integration": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by the integration the asset came from.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.Integrations...)},
			},
			"integration_account_id": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by the account ID within the integration.",
			},
			"schema_name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by schema name.",
			},
			"status": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by asset health status.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.AssetStatuses...)},
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by workspace ID.",
			},
			"sort_column": schema.StringAttribute{
				Optional:    true,
				Description: "Column to sort by.",
				Validators: []validator.String{stringvalidator.OneOf(
					"asset_name", "asset_type", "status", "integration",
					"created_in_integration", "last_updated_in_integration",
				)},
			},
			"sort_direction": schema.StringAttribute{
				Optional:    true,
				Description: "Sort direction.",
				Validators:  []validator.String{stringvalidator.OneOf("asc", "desc")},
			},
			"page_size": schema.Int64Attribute{
				Optional:            true,
				Description:         "Items fetched per request (1-100). Every page is fetched regardless; this only tunes request size.",
				MarkdownDescription: "Items fetched per request (1-100, default 100). Every matching page is fetched regardless; this only tunes the request size.",
			},
			"results": schema.ListNestedAttribute{
				Computed:     true,
				Description:  "Matching assets.",
				NestedObject: schema.NestedAttributeObject{Attributes: assetAttributes()},
			},
		},
	}
}

func assetAttributes() map[string]schema.Attribute {
	str := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, Description: desc}
	}
	num := func(desc string) schema.Attribute {
		return schema.Int64Attribute{Computed: true, Description: desc}
	}
	strList := func(desc string) schema.Attribute {
		return schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: desc}
	}
	jsonStr := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, CustomType: jsontypes.NormalizedType{}, Description: desc}
	}

	return map[string]schema.Attribute{
		"asset_id":                    str("Orchestra's ID for the asset."),
		"account_id":                  str("Account the asset belongs to."),
		"asset_name":                  str("Asset name."),
		"asset_type":                  str("Asset type."),
		"external_id":                 str("The asset's ID in the source integration."),
		"integration":                 str("Integration the asset came from."),
		"integration_account_id":      str("Account ID within the integration."),
		"integration_asset_type":      str("The integration's own name for this asset type."),
		"connection":                  str("Connection the asset was discovered through."),
		"connection_integration":      str("Integration of that connection."),
		"status":                      str("Asset health status."),
		"database_name":               str("Database name."),
		"schema_name":                 str("Schema name."),
		"table_name":                  str("Table name."),
		"workspace_id":                str("Workspace ID."),
		"workspace_name":              str("Workspace name."),
		"description":                 str("Asset description."),
		"url":                         str("Link to the asset in the source system."),
		"owner":                       str("Primary owner."),
		"owners":                      strList("All owners."),
		"upstream_dependencies":       strList("Asset IDs this asset depends on."),
		"downstream_dependencies":     strList("Asset IDs that depend on this asset."),
		"created_at":                  str("When Orchestra first saw the asset."),
		"updated_at":                  str("When Orchestra last updated the asset."),
		"created_in_integration":      str("When the asset was created in the source system."),
		"last_updated_in_integration": str("When the asset was last updated in the source system."),
		"last_viewed":                 str("When the asset was last viewed."),
		"row_count":                   num("Row count."),
		"bytes":                       num("Size in bytes."),
		"unique_viewers":              num("Distinct viewers."),
		"view_count":                  num("View count."),
		"view_count_from_timestamp":   str("Start of the window view_count covers."),
		"usage_percentage":            schema.NumberAttribute{Computed: true, Description: "Usage as a percentage."},
		"freshness_config":            jsonStr("Freshness configuration, as JSON. The API declares this free-form."),
		"meta":                        jsonStr("Arbitrary asset metadata, as JSON. The API declares this free-form."),
	}
}

type assetsModel struct {
	AssetIds             types.String `tfsdk:"asset_ids"`
	AssetType            types.String `tfsdk:"asset_type"`
	DatabaseName         types.String `tfsdk:"database_name"`
	Integration          types.String `tfsdk:"integration"`
	IntegrationAccountId types.String `tfsdk:"integration_account_id"`
	SchemaName           types.String `tfsdk:"schema_name"`
	Status               types.String `tfsdk:"status"`
	WorkspaceId          types.String `tfsdk:"workspace_id"`
	SortColumn           types.String `tfsdk:"sort_column"`
	SortDirection        types.String `tfsdk:"sort_direction"`
	PageSize             types.Int64  `tfsdk:"page_size"`
	Results              []AssetTF    `tfsdk:"results"`
}

// AssetTF is components.schemas.AssetResponse in Terraform types.
type AssetTF struct {
	AssetId                  types.String         `tfsdk:"asset_id"`
	AccountId                types.String         `tfsdk:"account_id"`
	AssetName                types.String         `tfsdk:"asset_name"`
	AssetType                types.String         `tfsdk:"asset_type"`
	ExternalId               types.String         `tfsdk:"external_id"`
	Integration              types.String         `tfsdk:"integration"`
	IntegrationAccountId     types.String         `tfsdk:"integration_account_id"`
	IntegrationAssetType     types.String         `tfsdk:"integration_asset_type"`
	Connection               types.String         `tfsdk:"connection"`
	ConnectionIntegration    types.String         `tfsdk:"connection_integration"`
	Status                   types.String         `tfsdk:"status"`
	DatabaseName             types.String         `tfsdk:"database_name"`
	SchemaName               types.String         `tfsdk:"schema_name"`
	TableName                types.String         `tfsdk:"table_name"`
	WorkspaceId              types.String         `tfsdk:"workspace_id"`
	WorkspaceName            types.String         `tfsdk:"workspace_name"`
	Description              types.String         `tfsdk:"description"`
	Url                      types.String         `tfsdk:"url"`
	Owner                    types.String         `tfsdk:"owner"`
	Owners                   []types.String       `tfsdk:"owners"`
	UpstreamDependencies     []types.String       `tfsdk:"upstream_dependencies"`
	DownstreamDependencies   []types.String       `tfsdk:"downstream_dependencies"`
	CreatedAt                types.String         `tfsdk:"created_at"`
	UpdatedAt                types.String         `tfsdk:"updated_at"`
	CreatedInIntegration     types.String         `tfsdk:"created_in_integration"`
	LastUpdatedInIntegration types.String         `tfsdk:"last_updated_in_integration"`
	LastViewed               types.String         `tfsdk:"last_viewed"`
	RowCount                 types.Int64          `tfsdk:"row_count"`
	Bytes                    types.Int64          `tfsdk:"bytes"`
	UniqueViewers            types.Int64          `tfsdk:"unique_viewers"`
	ViewCount                types.Int64          `tfsdk:"view_count"`
	ViewCountFromTimestamp   types.String         `tfsdk:"view_count_from_timestamp"`
	UsagePercentage          types.Number         `tfsdk:"usage_percentage"`
	FreshnessConfig          jsontypes.Normalized `tfsdk:"freshness_config"`
	Meta                     jsontypes.Normalized `tfsdk:"meta"`
}

// AssetResponse is components.schemas.AssetResponse.
type AssetResponse struct {
	AssetId                  *string        `json:"assetId"`
	AccountId                *string        `json:"accountId"`
	AssetName                *string        `json:"assetName"`
	AssetType                *string        `json:"assetType"`
	ExternalId               *string        `json:"externalId"`
	Integration              *string        `json:"integration"`
	IntegrationAccountId     *string        `json:"integrationAccountId"`
	IntegrationAssetType     *string        `json:"integrationAssetType"`
	Connection               *string        `json:"connection"`
	ConnectionIntegration    *string        `json:"connectionIntegration"`
	Status                   *string        `json:"status"`
	DatabaseName             *string        `json:"databaseName"`
	SchemaName               *string        `json:"schemaName"`
	TableName                *string        `json:"tableName"`
	WorkspaceId              *string        `json:"workspaceId"`
	WorkspaceName            *string        `json:"workspaceName"`
	Description              *string        `json:"description"`
	Url                      *string        `json:"url"`
	Owner                    *string        `json:"owner"`
	Owners                   []string       `json:"owners"`
	UpstreamDependencies     []string       `json:"upstreamDependencies"`
	DownstreamDependencies   []string       `json:"downstreamDependencies"`
	CreatedAt                *string        `json:"createdAt"`
	UpdatedAt                *string        `json:"updatedAt"`
	CreatedInIntegration     *string        `json:"createdInIntegration"`
	LastUpdatedInIntegration *string        `json:"lastUpdatedInIntegration"`
	LastViewed               *string        `json:"lastViewed"`
	RowCount                 *int64         `json:"rowCount"`
	Bytes                    *int64         `json:"bytes"`
	UniqueViewers            *int64         `json:"uniqueViewers"`
	ViewCount                *int64         `json:"viewCount"`
	ViewCountFromTimestamp   *string        `json:"viewCountFromTimestamp"`
	UsagePercentage          *float64       `json:"usagePercentage"`
	FreshnessConfig          map[string]any `json:"freshnessConfig"`
	Meta                     map[string]any `json:"meta"`
}

// ToTF converts an API asset to its Terraform representation.
func (a AssetResponse) ToTF() AssetTF {
	return AssetTF{
		AssetId:                  tfconv.String(a.AssetId),
		AccountId:                tfconv.String(a.AccountId),
		AssetName:                tfconv.String(a.AssetName),
		AssetType:                tfconv.String(a.AssetType),
		ExternalId:               tfconv.String(a.ExternalId),
		Integration:              tfconv.String(a.Integration),
		IntegrationAccountId:     tfconv.String(a.IntegrationAccountId),
		IntegrationAssetType:     tfconv.String(a.IntegrationAssetType),
		Connection:               tfconv.String(a.Connection),
		ConnectionIntegration:    tfconv.String(a.ConnectionIntegration),
		Status:                   tfconv.String(a.Status),
		DatabaseName:             tfconv.String(a.DatabaseName),
		SchemaName:               tfconv.String(a.SchemaName),
		TableName:                tfconv.String(a.TableName),
		WorkspaceId:              tfconv.String(a.WorkspaceId),
		WorkspaceName:            tfconv.String(a.WorkspaceName),
		Description:              tfconv.String(a.Description),
		Url:                      tfconv.String(a.Url),
		Owner:                    tfconv.String(a.Owner),
		Owners:                   tfconv.Strings(a.Owners),
		UpstreamDependencies:     tfconv.Strings(a.UpstreamDependencies),
		DownstreamDependencies:   tfconv.Strings(a.DownstreamDependencies),
		CreatedAt:                tfconv.String(a.CreatedAt),
		UpdatedAt:                tfconv.String(a.UpdatedAt),
		CreatedInIntegration:     tfconv.String(a.CreatedInIntegration),
		LastUpdatedInIntegration: tfconv.String(a.LastUpdatedInIntegration),
		LastViewed:               tfconv.String(a.LastViewed),
		RowCount:                 tfconv.Int64(a.RowCount),
		Bytes:                    tfconv.Int64(a.Bytes),
		UniqueViewers:            tfconv.Int64(a.UniqueViewers),
		ViewCount:                tfconv.Int64(a.ViewCount),
		ViewCountFromTimestamp:   tfconv.String(a.ViewCountFromTimestamp),
		UsagePercentage:          tfconv.Number(a.UsagePercentage),
		FreshnessConfig:          tfconv.JSON(a.FreshnessConfig),
		Meta:                     tfconv.JSON(a.Meta),
	}
}

func (d *AssetsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model assetsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	query := url.Values{}
	for param, value := range map[string]types.String{
		"asset_ids":              model.AssetIds,
		"asset_type":             model.AssetType,
		"database_name":          model.DatabaseName,
		"integration":            model.Integration,
		"integration_account_id": model.IntegrationAccountId,
		"schema_name":            model.SchemaName,
		"status":                 model.Status,
		"workspace_id":           model.WorkspaceId,
		"sort_column":            model.SortColumn,
		"sort_direction":         model.SortDirection,
	} {
		if v := tfconv.GoString(value); v != "" {
			query.Set(param, v)
		}
	}
	if p := tfconv.Int64Ptr(model.PageSize); p != nil {
		query.Set("page_size", strconv.FormatInt(*p, 10))
	}

	var api []AssetResponse
	if err := d.client.ListAll(ctx, "/assets", query, &api); err != nil {
		resp.Diagnostics.AddError("Cannot list assets", err.Error())
		return
	}

	model.Results = make([]AssetTF, 0, len(api))
	for _, a := range api {
		model.Results = append(model.Results, a.ToTF())
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
