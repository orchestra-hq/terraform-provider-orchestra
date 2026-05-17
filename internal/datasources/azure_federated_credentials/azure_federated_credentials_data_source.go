package azure_federated_credentials

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra/client"
)

type AzureFederatedCredentialsDataSource struct {
	client *client.Client
}

var _ datasource.DataSource = &AzureFederatedCredentialsDataSource{}

func (d *AzureFederatedCredentialsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_azure_federated_credentials"
}

func (d *AzureFederatedCredentialsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = AzureFederatedCredentialsDataSourceSchema(ctx)
}

func (d *AzureFederatedCredentialsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

type azureFederatedCredentialsResponse struct {
	SubjectId *string `json:"subject_id"`
	Audience  *string `json:"audience"`
}

func (d *AzureFederatedCredentialsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	_ = req
	var model AzureFederatedCredentialsModel

	var api azureFederatedCredentialsResponse
	if err := d.client.DoJSON(ctx, http.MethodGet, "/compute_resources/azure/federated_credentials", nil, nil, &api); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to get Azure federated credentials: %s", err))
		return
	}

	model.SubjectId = types.StringValue(pointerToString(api.SubjectId))
	model.Audience = types.StringValue(pointerToString(api.Audience))

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func pointerToString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

