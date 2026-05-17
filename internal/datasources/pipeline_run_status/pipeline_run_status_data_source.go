package pipeline_run_status

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra/client"
)

type PipelineRunStatusDataSource struct {
	client *client.Client
}

var _ datasource.DataSource = &PipelineRunStatusDataSource{}

func (d *PipelineRunStatusDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline_run_status"
}

func (d *PipelineRunStatusDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = PipelineRunStatusDataSourceSchema(ctx)
}

func (d *PipelineRunStatusDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

type pipelineRunStatusResponse struct {
	Id         *string `json:"id"`
	PipelineId *string `json:"pipelineId"`
	PipelineName *string `json:"pipelineName"`
	RunStatus  *string `json:"runStatus"`
}

func (d *PipelineRunStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model PipelineRunStatusModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	if model.Id.IsNull() || model.Id.IsUnknown() || model.Id.ValueString() == "" {
		resp.Diagnostics.AddError("Missing id", "`id` is required")
		return
	}

	id := model.Id.ValueString()
	_ = url.Values{}

	var api pipelineRunStatusResponse
	if err := d.client.DoJSON(ctx, http.MethodGet, fmt.Sprintf("/pipeline_runs/%s/status", id), nil, nil, &api); err != nil {
		resp.Diagnostics.AddError("API error", err.Error())
		return
	}

	model.PipelineId = types.StringValue(strPtrToValue(api.PipelineId))
	model.PipelineName = types.StringValue(strPtrToValue(api.PipelineName))
	model.RunStatus = types.StringValue(strPtrToValue(api.RunStatus))

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func strPtrToValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

