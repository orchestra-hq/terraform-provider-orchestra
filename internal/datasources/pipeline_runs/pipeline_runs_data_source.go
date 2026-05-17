package pipeline_runs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

type PipelineRunsDataSource struct {
	client *client.Client
}

var _ datasource.DataSource = &PipelineRunsDataSource{}

func (d *PipelineRunsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline_runs"
}

func (d *PipelineRunsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = PipelineRunsDataSourceSchema(ctx)
}

func (d *PipelineRunsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

type pipelineRunResponse struct {
	AccountId             *string         `json:"accountId"`
	Branch                *string         `json:"branch"`
	ChildPipelineRuns     json.RawMessage `json:"childPipelineRuns"`
	Commit                *string         `json:"commit"`
	CompletedAt           *string         `json:"completedAt"`
	CreatedAt             *string         `json:"createdAt"`
	EnvId                 *string         `json:"envId"`
	EnvName               *string         `json:"envName"`
	Id                    *string         `json:"id"`
	Message               *string         `json:"message"`
	PipelineId            *string         `json:"pipelineId"`
	PipelineName          *string         `json:"pipelineName"`
	PipelineVersionNumber *int64          `json:"pipelineVersionNumber"`
	RunStatus             *string         `json:"runStatus"`
	StartedAt             *string         `json:"startedAt"`
	TriggeredBy           json.RawMessage `json:"triggeredBy"`
	UpdatedAt             *string         `json:"updatedAt"`
}

type paginatedPipelineRuns struct {
	Results []pipelineRunResponse `json:"results"`
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

func rawToJSONString(raw json.RawMessage) basetypes.StringValue {
	if len(raw) == 0 || string(raw) == "null" {
		return basetypes.NewStringValue("[]")
	}
	return basetypes.NewStringValue(string(raw))
}

func (d *PipelineRunsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model PipelineRunsModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	query := url.Values{}
	if !model.Status.IsNull() && !model.Status.IsUnknown() && model.Status.ValueString() != "" {
		query.Set("status", model.Status.ValueString())
	}
	if !model.PipelineIds.IsNull() && !model.PipelineIds.IsUnknown() && model.PipelineIds.ValueString() != "" {
		query.Set("pipeline_ids", model.PipelineIds.ValueString())
	}
	if !model.PipelineRunIds.IsNull() && !model.PipelineRunIds.IsUnknown() && model.PipelineRunIds.ValueString() != "" {
		query.Set("pipeline_run_ids", model.PipelineRunIds.ValueString())
	}

	if (model.TimeFrom.ValueString() != "" || !model.TimeFrom.IsNull()) != (model.TimeTo.ValueString() != "" || !model.TimeTo.IsNull()) {
		// Best-effort: only validate when exactly one is set.
		if !(model.TimeFrom.IsNull() || model.TimeFrom.IsUnknown()) && (model.TimeTo.IsNull() || model.TimeTo.IsUnknown()) {
			resp.Diagnostics.AddError("Invalid time range", "`time_from` requires `time_to`.")
			return
		}
		if !(model.TimeTo.IsNull() || model.TimeTo.IsUnknown()) && (model.TimeFrom.IsNull() || model.TimeFrom.IsUnknown()) {
			resp.Diagnostics.AddError("Invalid time range", "`time_to` requires `time_from`.")
			return
		}
	}

	if !model.TimeFrom.IsNull() && !model.TimeFrom.IsUnknown() && model.TimeFrom.ValueString() != "" {
		query.Set("time_from", model.TimeFrom.ValueString())
	}
	if !model.TimeTo.IsNull() && !model.TimeTo.IsUnknown() && model.TimeTo.ValueString() != "" {
		query.Set("time_to", model.TimeTo.ValueString())
	}

	var apiResult paginatedPipelineRuns
	if err := d.client.DoJSON(ctx, http.MethodGet, "/pipeline_runs", query, nil, &apiResult); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to list pipeline runs: %s", err))
		return
	}

	elements := make([]attr.Value, 0, len(apiResult.Results))
	for _, r := range apiResult.Results {
		el := ResultsValue{
			AccountId:             strPtr(r.AccountId),
			Branch:                strPtr(r.Branch),
			ChildPipelineRuns:     rawToJSONString(r.ChildPipelineRuns),
			Commit:                strPtr(r.Commit),
			CompletedAt:           strPtr(r.CompletedAt),
			CreatedAt:             strPtr(r.CreatedAt),
			EnvId:                 strPtr(r.EnvId),
			EnvName:               strPtr(r.EnvName),
			Id:                    strPtr(r.Id),
			Message:               strPtr(r.Message),
			PipelineId:            strPtr(r.PipelineId),
			PipelineName:          strPtr(r.PipelineName),
			PipelineVersionNumber: int64Ptr(r.PipelineVersionNumber),
			RunStatus:             strPtr(r.RunStatus),
			StartedAt:             strPtr(r.StartedAt),
			TriggeredBy:           rawToJSONString(r.TriggeredBy),
			UpdatedAt:             strPtr(r.UpdatedAt),
			state:                 attr.ValueStateKnown,
		}
		elements = append(elements, el)
	}

	model.Results = types.SetValueMust(ResultsType{}, elements)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
