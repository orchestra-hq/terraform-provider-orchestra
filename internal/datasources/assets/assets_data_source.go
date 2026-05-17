package assets

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra/client"
)

type AssetsDataSource struct {
	client *client.Client
}

var _ datasource.DataSource = &AssetsDataSource{}

func (d *AssetsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_assets"
}

func (d *AssetsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = AssetsDataSourceSchema(ctx)
}

func (d *AssetsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	_ = ctx
	if req.ProviderData == nil {
		resp.Diagnostics.AddError("Missing ProviderData", "ProviderData was not set for this datasource")
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}

	d.client = c
}

type assetResponse struct {
	AccountId             *string  `json:"accountId"`
	AssetId              *string  `json:"assetId"`
	AssetName            *string  `json:"assetName"`
	AssetType            *string  `json:"assetType"`
	Bytes                *int64   `json:"bytes"`
	Connection           *string  `json:"connection"`
	ConnectionIntegration *string `json:"connectionIntegration"`
	CreatedAt            *string  `json:"createdAt"`
	CreatedInIntegration *string `json:"createdInIntegration"`
	DatabaseName         *string  `json:"databaseName"`
	DownstreamDependencies []string `json:"downstreamDependencies"`
	ExternalId           *string  `json:"externalId"`
	Integration          *string  `json:"integration"`
	IntegrationAccountId *string `json:"integrationAccountId"`
	IntegrationAssetType *string `json:"integrationAssetType"`
	LastUpdatedInIntegration *string `json:"lastUpdatedInIntegration"`
	Owner                *string   `json:"owner"`
	Owners               []string  `json:"owners"`
	RowCount             *int64    `json:"rowCount"`
	SchemaName          *string   `json:"schemaName"`
	Status               *string   `json:"status"`
	TableName           *string   `json:"tableName"`
	UpdatedAt            *string   `json:"updatedAt"`
	UpstreamDependencies []string `json:"upstreamDependencies"`
	UsagePercentage      *float64  `json:"usagePercentage"`
}

type paginatedAssets struct {
	Results []assetResponse `json:"results"`
}

func strPtr(v *string) basetypes.StringValue {
	if v == nil {
		return basetypes.NewStringNull()
	}
	return basetypes.NewStringValue(*v)
}

func int64Ptr(v *int64) basetypes.Int64Value {
	if v == nil {
		return basetypes.NewInt64Null()
	}
	return basetypes.NewInt64Value(*v)
}

func float64Ptr(v *float64) basetypes.Float64Value {
	if v == nil {
		return basetypes.NewFloat64Null()
	}
	return basetypes.NewFloat64Value(*v)
}

func stringsToListValue(values []string) basetypes.ListValue {
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, basetypes.NewStringValue(v))
	}
	return basetypes.NewListValueMust(types.StringType, elems)
}

func (d *AssetsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model AssetsModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	query := url.Values{}
	if !model.AssetType.IsNull() && !model.AssetType.IsUnknown() && model.AssetType.ValueString() != "" {
		query.Set("asset_type", model.AssetType.ValueString())
	}
	if !model.Integration.IsNull() && !model.Integration.IsUnknown() && model.Integration.ValueString() != "" {
		query.Set("integration", model.Integration.ValueString())
	}

	var api paginatedAssets
	if err := d.client.DoJSON(ctx, http.MethodGet, "/assets", query, nil, &api); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to list assets: %s", err))
		return
	}

	elements := make([]attr.Value, 0, len(api.Results))
	for _, r := range api.Results {
		el := ResultsValue{
			AccountId:              strPtr(r.AccountId),
			AssetId:                strPtr(r.AssetId),
			AssetName:              strPtr(r.AssetName),
			AssetType:              strPtr(r.AssetType),
			Bytes:                  int64Ptr(r.Bytes),
			Connection:             strPtr(r.Connection),
			ConnectionIntegration:  strPtr(r.ConnectionIntegration),
			CreatedAt:              strPtr(r.CreatedAt),
			CreatedInIntegration:  strPtr(r.CreatedInIntegration),
			DatabaseName:           strPtr(r.DatabaseName),
			DownstreamDependencies: stringsToListValue(r.DownstreamDependencies),
			ExternalId:             strPtr(r.ExternalId),
			Integration:            strPtr(r.Integration),
			IntegrationAccountId:  strPtr(r.IntegrationAccountId),
			IntegrationAssetType:  strPtr(r.IntegrationAssetType),
			LastUpdatedInIntegration: strPtr(r.LastUpdatedInIntegration),
			Owner:                  strPtr(r.Owner),
			Owners:                 stringsToListValue(r.Owners),
			RowCount:               int64Ptr(r.RowCount),
			SchemaName:             strPtr(r.SchemaName),
			Status:                 strPtr(r.Status),
			TableName:             strPtr(r.TableName),
			UpdatedAt:             strPtr(r.UpdatedAt),
			UpstreamDependencies:  stringsToListValue(r.UpstreamDependencies),
			UsagePercentage:       float64Ptr(r.UsagePercentage),
			state:                  attr.ValueStateKnown,
		}
		elements = append(elements, el)
	}

	model.Results = types.SetValueMust(ResultsType{}, elements)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

