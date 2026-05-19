package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/assets"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/azure_federated_credentials"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/pipelines"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/pipeline"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() tfprovider.Provider {
	return func() tfprovider.Provider {
		return &orchestraProvider{
			version: version,
		}
	}
}

type orchestraProvider struct {
	version string
	apiKey  string
}

var _ tfprovider.Provider = &orchestraProvider{}

func (p *orchestraProvider) Metadata(ctx context.Context, _ tfprovider.MetadataRequest, resp *tfprovider.MetadataResponse) {
	resp.TypeName = "orchestra"
	resp.Version = p.version
}

func (p *orchestraProvider) Schema(ctx context.Context, _ tfprovider.SchemaRequest, resp *tfprovider.SchemaResponse) {
	resp.Schema = OrchestraProviderSchema(ctx)
}

func (p *orchestraProvider) Configure(ctx context.Context, req tfprovider.ConfigureRequest, resp *tfprovider.ConfigureResponse) {
	p.apiKey = os.Getenv("ORCHESTRA_API_KEY")

	// Provider-level config is currently limited, but we keep wiring extensible.
	var model OrchestraModel
	if diags := req.Config.Get(ctx, &model); len(diags) > 0 {
		resp.Diagnostics.Append(diags...)
	}

	var baseURL string
	if !model.BaseURL.IsUnknown() {
		baseURL = model.BaseURL.ValueString()
	}

	if resp.Diagnostics.HasError() {
		return
	}

	c := client.New(p.apiKey, baseURL)

	// Expose the shared API client to all datasources and resources.
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *orchestraProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		func() datasource.DataSource { return &assets.AssetsDataSource{} },
		func() datasource.DataSource {
			return &azure_federated_credentials.AzureFederatedCredentialsDataSource{}
		},
		func() datasource.DataSource { return &pipelines.PipelinesDataSource{} },
	}
}

func (p *orchestraProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource { return &pipeline.PipelineResource{} },
	}
}
