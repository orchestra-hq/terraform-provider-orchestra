package task_runs

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

type TaskRunsDataSource struct {
	client *client.Client
}

var _ datasource.DataSource = &TaskRunsDataSource{}

func (d *TaskRunsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_task_runs"
}

func (d *TaskRunsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = TaskRunsDataSourceSchema(ctx)
}

func (d *TaskRunsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

type taskRunResponse struct {
	AccountId        *string         `json:"accountId"`
	CompletedAt      *string         `json:"completedAt"`
	ConnectionId     *string         `json:"connectionId"`
	CreatedAt        *string         `json:"createdAt"`
	ExternalMessage  *string         `json:"externalMessage"`
	ExternalStatus   *string         `json:"externalStatus"`
	Id               *string         `json:"id"`
	Integration      *string         `json:"integration"`
	IntegrationJob   *string         `json:"integrationJob"`
	Message          *string         `json:"message"`
	NumberOfAttempts *int64          `json:"numberOfAttempts"`
	PipelineId       *string         `json:"pipelineId"`
	PipelineRunId    *string         `json:"pipelineRunId"`
	PlatformLink     *string         `json:"platformLink"`
	RunParameters    json.RawMessage `json:"runParameters"`
	StartedAt        *string         `json:"startedAt"`
	Status           *string         `json:"status"`
	TaskId           *string         `json:"taskId"`
	TaskName         *string         `json:"taskName"`
	TaskParameters   json.RawMessage `json:"taskParameters"`
	UpdatedAt        *string         `json:"updatedAt"`
}

type paginatedTaskRuns struct {
	Results []taskRunResponse `json:"results"`
}

func (d *TaskRunsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model TaskRunsModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	query := url.Values{}
	if !model.Integration.IsNull() && !model.Integration.IsUnknown() && model.Integration.ValueString() != "" {
		query.Set("integration", model.Integration.ValueString())
	}
	if !model.PipelineIds.IsNull() && !model.PipelineIds.IsUnknown() && model.PipelineIds.ValueString() != "" {
		query.Set("pipeline_ids", model.PipelineIds.ValueString())
	}
	if !model.Status.IsNull() && !model.Status.IsUnknown() && model.Status.ValueString() != "" {
		query.Set("status", model.Status.ValueString())
	}
	if !model.TaskRunIds.IsNull() && !model.TaskRunIds.IsUnknown() && model.TaskRunIds.ValueString() != "" {
		query.Set("task_run_ids", model.TaskRunIds.ValueString())
	}
	if !model.TimeFrom.IsNull() && !model.TimeFrom.IsUnknown() && model.TimeFrom.ValueString() != "" {
		query.Set("time_from", model.TimeFrom.ValueString())
	}
	if !model.TimeTo.IsNull() && !model.TimeTo.IsUnknown() && model.TimeTo.ValueString() != "" {
		query.Set("time_to", model.TimeTo.ValueString())
	}

	var api paginatedTaskRuns
	if err := d.client.DoJSON(ctx, http.MethodGet, "/task_runs", query, nil, &api); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to list task runs: %s", err))
		return
	}

	elements := make([]attr.Value, 0, len(api.Results))
	for _, r := range api.Results {
		stringPtr := func(v *string) basetypes.StringValue {
			if v == nil {
				return basetypes.NewStringNull()
			}
			return basetypes.NewStringValue(*v)
		}
		int64Ptr := func(v *int64) basetypes.Int64Value {
			if v == nil {
				return basetypes.NewInt64Null()
			}
			return basetypes.NewInt64Value(*v)
		}
		rawPtrToString := func(raw json.RawMessage) basetypes.StringValue {
			if len(raw) == 0 || string(raw) == "null" {
				return basetypes.NewStringNull()
			}
			return basetypes.NewStringValue(string(raw))
		}

		el := ResultsValue{
			AccountId:        stringPtr(r.AccountId),
			CompletedAt:      stringPtr(r.CompletedAt),
			ConnectionId:     stringPtr(r.ConnectionId),
			CreatedAt:        stringPtr(r.CreatedAt),
			ExternalMessage:  stringPtr(r.ExternalMessage),
			ExternalStatus:   stringPtr(r.ExternalStatus),
			Id:               stringPtr(r.Id),
			Integration:      stringPtr(r.Integration),
			IntegrationJob:   stringPtr(r.IntegrationJob),
			Message:          stringPtr(r.Message),
			NumberOfAttempts: int64Ptr(r.NumberOfAttempts),
			PipelineId:       stringPtr(r.PipelineId),
			PipelineRunId:    stringPtr(r.PipelineRunId),
			PlatformLink:     stringPtr(r.PlatformLink),
			RunParameters:    rawPtrToString(r.RunParameters),
			StartedAt:        stringPtr(r.StartedAt),
			Status:           stringPtr(r.Status),
			TaskId:           stringPtr(r.TaskId),
			TaskName:         stringPtr(r.TaskName),
			TaskParameters:   rawPtrToString(r.TaskParameters),
			UpdatedAt:        stringPtr(r.UpdatedAt),
			state:            attr.ValueStateKnown,
		}
		elements = append(elements, el)
	}

	model.Results = types.SetValueMust(ResultsType{}, elements)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
