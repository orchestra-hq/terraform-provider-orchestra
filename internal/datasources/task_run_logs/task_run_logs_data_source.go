package task_run_logs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

type TaskRunLogsDataSource struct {
	client *client.Client
}

var _ datasource.DataSource = &TaskRunLogsDataSource{}

func (d *TaskRunLogsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_task_run_logs"
}

func (d *TaskRunLogsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = TaskRunLogsDataSourceSchema(ctx)
}

func (d *TaskRunLogsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

type taskRunFilesResponse struct {
	Filenames []string `json:"filenames"`
}

func (d *TaskRunLogsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model TaskRunLogsModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	if model.PipelineRunId.IsNull() || model.PipelineRunId.IsUnknown() || model.PipelineRunId.ValueString() == "" {
		resp.Diagnostics.AddError("Missing pipeline_run_id", "`pipeline_run_id` is required")
		return
	}
	if model.TaskRunId.IsNull() || model.TaskRunId.IsUnknown() || model.TaskRunId.ValueString() == "" {
		resp.Diagnostics.AddError("Missing task_run_id", "`task_run_id` is required")
		return
	}

	var api taskRunFilesResponse
	p := fmt.Sprintf("/pipeline_runs/%s/task_runs/%s/logs", model.PipelineRunId.ValueString(), model.TaskRunId.ValueString())
	if err := d.client.DoJSON(ctx, http.MethodGet, p, url.Values{}, nil, &api); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to list task run logs: %s", err))
		return
	}

	elements := make([]attr.Value, 0, len(api.Filenames))
	for _, f := range api.Filenames {
		elements = append(elements, basetypes.NewStringValue(f))
	}

	model.Filenames = types.ListValueMust(types.StringType, elements)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
