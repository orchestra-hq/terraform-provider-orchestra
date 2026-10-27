package provider

import (
	"context"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	tfprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/agents"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/assets"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/azure_federated_credentials"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/environments"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/integrations"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/monitors"
	datasourcepipeline "github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/pipeline"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/pipelines"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/environment"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/incidentsettings"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/integrationconnection"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/monitor"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/pipeline"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/client"
)

const (
	envAPIKey    = "ORCHESTRA_API_KEY"
	envBaseURL   = "ORCHESTRA_BASE_URL"
	envAccountID = "ORCHESTRA_ACCOUNT_ID"
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
	var model OrchestraModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Values that are still unknown at configure time (e.g. sourced from another
	// resource) cannot be used to build the client.
	if model.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Unknown Orchestra API key",
			"The provider cannot be configured while `api_key` is unknown. Use a value known at plan time, or set the "+envAPIKey+" environment variable.",
		)
	}
	if model.BaseURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("base_url"),
			"Unknown Orchestra base URL",
			"The provider cannot be configured while `base_url` is unknown. Use a value known at plan time, or set the "+envBaseURL+" environment variable.",
		)
	}
	if model.AccountID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("account_id"),
			"Unknown Orchestra account ID",
			"The provider cannot be configured while `account_id` is unknown. Use a value known at plan time, or set the "+envAccountID+" environment variable.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := firstNonEmpty(model.APIKey.ValueString(), os.Getenv(envAPIKey))
	baseURL := firstNonEmpty(model.BaseURL.ValueString(), os.Getenv(envBaseURL))
	accountID := firstNonEmpty(model.AccountID.ValueString(), os.Getenv(envAccountID))

	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing Orchestra API key",
			"Set the provider `api_key` attribute or the "+envAPIKey+" environment variable. "+
				"Workspace API keys are available under Settings → API Keys in the Orchestra UI.",
		)
		return
	}

	c := client.New(
		apiKey,
		baseURL,
		client.WithAccountID(accountID),
		client.WithUserAgent("terraform-provider-orchestra/"+p.version),
	)

	// Expose the shared API client to all datasources and resources.
	resp.DataSourceData = c
	resp.ResourceData = c
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (p *orchestraProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		func() datasource.DataSource { return &assets.AssetsDataSource{} },
		func() datasource.DataSource {
			return &azure_federated_credentials.AzureFederatedCredentialsDataSource{}
		},
		func() datasource.DataSource { return &environments.EnvironmentDataSource{} },
		func() datasource.DataSource { return &environments.EnvironmentsDataSource{} },
		func() datasource.DataSource { return &integrations.IntegrationConnectionsDataSource{} },
		func() datasource.DataSource { return &monitors.MonitorsDataSource{} },
		func() datasource.DataSource { return &datasourcepipeline.PipelineDataSource{} },
		func() datasource.DataSource { return &pipelines.PipelinesDataSource{} },
		func() datasource.DataSource { return &agents.SkillsDataSource{} },
	}
}

func (p *orchestraProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource { return &environment.EnvironmentResource{} },
		func() resource.Resource { return &incidentsettings.IncidentSettingsResource{} },
		func() resource.Resource { return &integrationconnection.IntegrationConnectionResource{} },
		func() resource.Resource { return &monitor.MonitorResource{} },
		func() resource.Resource { return &monitor.MonitorOrderResource{} },
		func() resource.Resource { return &pipeline.PipelineResource{} },
		func() resource.Resource { return &agents.AgentResource{} },
		func() resource.Resource { return &agents.SkillResource{} },
	}
}
