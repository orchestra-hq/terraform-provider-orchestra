// Hand-maintained: provider-level configuration (auth, endpoint, account
// targeting) is not derivable from the OpenAPI spec.

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func OrchestraProviderSchema(ctx context.Context) schema.Schema {
	return schema.Schema{
		Description:         "Manage Orchestra pipelines and environments, and read workspace metadata.",
		MarkdownDescription: "Manage Orchestra pipelines and environments, and read workspace metadata.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				Description:         "Orchestra workspace API key. Falls back to the ORCHESTRA_API_KEY environment variable.",
				MarkdownDescription: "Orchestra workspace API key. Falls back to the `ORCHESTRA_API_KEY` environment variable. Find it under [Settings → API Keys](https://app.getorchestra.io/settings/workspace).",
			},
			"base_url": schema.StringAttribute{
				Optional:            true,
				Description:         "Base URL for the Orchestra API server. Falls back to the ORCHESTRA_BASE_URL environment variable, then the public endpoint. Must use https, except for localhost.",
				MarkdownDescription: "Base URL for the Orchestra API server. Falls back to the `ORCHESTRA_BASE_URL` environment variable, then `https://app.getorchestra.io/api/engine/public`. Must use `https://`, except for localhost. Agents and skills use the Agents API beside it, at `/api/ai/v1` on the same host.",
			},
			"account_id": schema.StringAttribute{
				Optional:            true,
				Description:         "Act on this account rather than the one the credential resolves to. Falls back to the ORCHESTRA_ACCOUNT_ID environment variable.",
				MarkdownDescription: "Act on this account rather than the one the credential resolves to, sent as the `X-Orchestra-Account-Id` header. Falls back to the `ORCHESTRA_ACCOUNT_ID` environment variable. An API key is issued to a single account, so it may only name that account.",
			},
		},
	}
}

type OrchestraModel struct {
	APIKey    types.String `tfsdk:"api_key"`
	BaseURL   types.String `tfsdk:"base_url"`
	AccountID types.String `tfsdk:"account_id"`
}
