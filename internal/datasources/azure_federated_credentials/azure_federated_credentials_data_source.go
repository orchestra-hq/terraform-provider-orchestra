package azure_federated_credentials

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

type AzureFederatedCredentialsDataSource struct {
	client *client.Client
}

var _ datasource.DataSource = &AzureFederatedCredentialsDataSource{}

func (d *AzureFederatedCredentialsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_azure_federated_credentials"
}

func (d *AzureFederatedCredentialsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The workspace's Azure workload-identity federation details, for granting Orchestra access to Azure compute without a stored secret.",
		MarkdownDescription: "The workspace's Azure workload-identity federation details, for granting Orchestra access to Azure compute without a stored secret.\n\n" +
			"Use these to configure a federated identity credential on an Azure app registration — for example `azuread_application_federated_identity_credential`.",
		Attributes: map[string]schema.Attribute{
			"subject_id": schema.StringAttribute{
				Computed:            true,
				Description:         "The subject identifier Azure must match on the federated credential.",
				MarkdownDescription: "The subject identifier Azure must match on the federated credential, used as the `subject` of an `azuread_application_federated_identity_credential`.",
			},
			"audience": schema.StringAttribute{
				Computed:            true,
				Description:         "The audience Azure must accept on the federated credential.",
				MarkdownDescription: "The audience Azure must accept on the federated credential, used as `audiences`.",
			},
		},
	}
}

// AzureFederatedCredentialsModel is the Terraform model for this data source.
type AzureFederatedCredentialsModel struct {
	Audience  types.String `tfsdk:"audience"`
	SubjectId types.String `tfsdk:"subject_id"`
}

func (d *AzureFederatedCredentialsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	_ = ctx
	if req.ProviderData == nil {
		// Configure runs before the provider is configured during
		// `terraform validate`, and again after. Erroring on the first call
		// makes `terraform validate` fail for every user of this data source.
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
	SubjectId *string `json:"subjectId"`
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

	model.SubjectId = tfconv.String(api.SubjectId)
	model.Audience = tfconv.String(api.Audience)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
