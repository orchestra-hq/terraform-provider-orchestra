package operations

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

type OperationsDataSource struct {
	client *client.Client
}

var _ datasource.DataSource = &OperationsDataSource{}

func (d *OperationsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_operations"
}

func (d *OperationsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = OperationsDataSourceSchema(ctx)
}

func (d *OperationsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

type operationResponse struct {
	AccountId         *string  `json:"accountId"`
	CompletedAt       *string  `json:"completedAt"`
	Dependencies      []string `json:"dependencies"`
	ExternalDetail    *string  `json:"externalDetail"`
	ExternalId        *string  `json:"externalId"`
	ExternalStatus    *string  `json:"externalStatus"`
	Id                *string  `json:"id"`
	InsertedAt        *string  `json:"insertedAt"`
	Integration       *string  `json:"integration"`
	IntegrationJob    *string  `json:"integrationJob"`
	Message           *string  `json:"message"`
	OperationDuration *float64 `json:"operationDuration"`
	OperationName     *string  `json:"operationName"`
	OperationStatus   *string  `json:"operationStatus"`
	OperationType     *string  `json:"operationType"`
	PipelineRunId     *string  `json:"pipelineRunId"`
	RowsAffected      *int64   `json:"rowsAffected"`
	StartedAt         *string  `json:"startedAt"`
	TaskRunId         *string  `json:"taskRunId"`
}

type paginatedOperations struct {
	Results []operationResponse `json:"results"`
}

func (d *OperationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model OperationsModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	query := url.Values{}
	if !model.ExternalId.IsNull() && !model.ExternalId.IsUnknown() && model.ExternalId.ValueString() != "" {
		query.Set("external_id", model.ExternalId.ValueString())
	}
	if !model.Integration.IsNull() && !model.Integration.IsUnknown() && model.Integration.ValueString() != "" {
		query.Set("integration", model.Integration.ValueString())
	}
	if !model.OperationType.IsNull() && !model.OperationType.IsUnknown() && model.OperationType.ValueString() != "" {
		query.Set("operation_type", model.OperationType.ValueString())
	}
	if !model.Status.IsNull() && !model.Status.IsUnknown() && model.Status.ValueString() != "" {
		query.Set("status", model.Status.ValueString())
	}
	if !model.TaskRunId.IsNull() && !model.TaskRunId.IsUnknown() && model.TaskRunId.ValueString() != "" {
		query.Set("task_run_id", model.TaskRunId.ValueString())
	}
	if !model.TimeFrom.IsNull() && !model.TimeFrom.IsUnknown() && model.TimeFrom.ValueString() != "" {
		query.Set("time_from", model.TimeFrom.ValueString())
	}
	if !model.TimeTo.IsNull() && !model.TimeTo.IsUnknown() && model.TimeTo.ValueString() != "" {
		query.Set("time_to", model.TimeTo.ValueString())
	}

	var api paginatedOperations
	if err := d.client.DoJSON(ctx, http.MethodGet, "/operations", query, nil, &api); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to list operations: %s", err))
		return
	}

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
	float64Ptr := func(v *float64) basetypes.NumberValue {
		if v == nil {
			return basetypes.NewNumberNull()
		}
		return basetypes.NewNumberValue(big.NewFloat(*v))
	}

	elements := make([]attr.Value, 0, len(api.Results))
	for _, r := range api.Results {
		deps := make([]attr.Value, 0, len(r.Dependencies))
		for _, dep := range r.Dependencies {
			deps = append(deps, basetypes.NewStringValue(dep))
		}

		el := ResultsValue{
			AccountId:         stringPtr(r.AccountId),
			CompletedAt:       stringPtr(r.CompletedAt),
			Dependencies:      basetypes.NewListValueMust(types.StringType, deps),
			ExternalDetail:    stringPtr(r.ExternalDetail),
			ExternalId:        stringPtr(r.ExternalId),
			ExternalStatus:    stringPtr(r.ExternalStatus),
			Id:                stringPtr(r.Id),
			InsertedAt:        stringPtr(r.InsertedAt),
			Integration:       stringPtr(r.Integration),
			IntegrationJob:    stringPtr(r.IntegrationJob),
			Message:           stringPtr(r.Message),
			OperationDuration: float64Ptr(r.OperationDuration),
			OperationName:     stringPtr(r.OperationName),
			OperationStatus:   stringPtr(r.OperationStatus),
			OperationType:     stringPtr(r.OperationType),
			PipelineRunId:     stringPtr(r.PipelineRunId),
			RowsAffected:      int64Ptr(r.RowsAffected),
			StartedAt:         stringPtr(r.StartedAt),
			TaskRunId:         stringPtr(r.TaskRunId),
			state:             attr.ValueStateKnown,
		}
		elements = append(elements, el)
	}

	model.Results = types.ListValueMust(ResultsType{}, elements)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
