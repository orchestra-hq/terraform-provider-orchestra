package pipelines

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

type PipelinesDataSource struct {
	client *client.Client
}

var _ datasource.DataSource = &PipelinesDataSource{}

func (d *PipelinesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipelines"
}

func (d *PipelinesDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = PipelinesDataSourceSchema(ctx)
}

func (d *PipelinesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

type pipelineResponse struct {
	Alias                  *string         `json:"alias"`
	CreatedAt              *string         `json:"createdAt"`
	DefaultBranch          *string         `json:"defaultBranch"`
	Id                     *string         `json:"id"`
	Inputs                 json.RawMessage `json:"inputs"`
	LatestRunCompletedAt   *string         `json:"latestRunCompletedAt"`
	LatestRunCreatedAt     *string         `json:"latestRunCreatedAt"`
	LatestRunId            *string         `json:"latestRunId"`
	LatestRunMessage       *string         `json:"latestRunMessage"`
	LatestRunStartedAt     *string         `json:"latestRunStartedAt"`
	LatestRunStatus        *string         `json:"latestRunStatus"`
	LatestVersionNumber    *int64          `json:"latestVersionNumber"`
	Name                   *string         `json:"name"`
	NumTasks               *int64          `json:"numTasks"`
	Paused                 *bool           `json:"paused"`
	ProductId              *string         `json:"productId"`
	ProductName            *string         `json:"productName"`
	PublishedVersionNumber *int64          `json:"publishedVersionNumber"`
	Repository             *string         `json:"repository"`
	Schedule               json.RawMessage `json:"schedule"`
	Sensors                json.RawMessage `json:"sensors"`
	StorageProvider        *string         `json:"storageProvider"`
	TriggerEvents          json.RawMessage `json:"triggerEvents"`
	UpdatedAt              *string         `json:"updatedAt"`
	Webhook                json.RawMessage `json:"webhook"`
	YamlPath               *string         `json:"yamlPath"`
}

func strPtr(v *string) basetypes.StringValue {
	if v == nil {
		return basetypes.NewStringNull()
	}
	return basetypes.NewStringValue(*v)
}

func boolPtr(v *bool) basetypes.BoolValue {
	if v == nil {
		return basetypes.NewBoolNull()
	}
	return basetypes.NewBoolValue(*v)
}

func int64Ptr(v *int64) basetypes.Int64Value {
	if v == nil {
		return basetypes.NewInt64Null()
	}
	return basetypes.NewInt64Value(*v)
}

func rawToJSONString(raw json.RawMessage) basetypes.StringValue {
	if len(raw) == 0 || string(raw) == "null" {
		// Many schema fields store JSON strings; default null->empty array.
		return basetypes.NewStringValue("[]")
	}

	// Raw is already JSON; store it as-is.
	return basetypes.NewStringValue(string(raw))
}

func (d *PipelinesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	// Datasource has no input configuration today, but framework requires we still decode.
	var model PipelinesModel
	_ = req.Config.Get(ctx, &model)

	var apiPipelines []pipelineResponse
	if err := d.client.DoJSON(ctx, http.MethodGet, "/pipelines", nil, nil, &apiPipelines); err != nil {
		resp.Diagnostics.AddError("API error", err.Error())
		return
	}

	// Inputs/Sensors/schedules are stored as JSON strings or empty objects in this provider's schema.
	_jsiiInputsObject := types.ObjectValueMust(InputsValue{}.AttributeTypes(ctx), map[string]attr.Value{})

	elements := make([]attr.Value, 0, len(apiPipelines))
	for _, p := range apiPipelines {
		el := PipelinesValue{
			Alias:                  strPtr(p.Alias),
			CreatedAt:              strPtr(p.CreatedAt),
			DefaultBranch:          strPtr(p.DefaultBranch),
			Id:                     strPtr(p.Id),
			Inputs:                 _jsiiInputsObject,
			LatestRunCompletedAt:   strPtr(p.LatestRunCompletedAt),
			LatestRunCreatedAt:     strPtr(p.LatestRunCreatedAt),
			LatestRunId:            strPtr(p.LatestRunId),
			LatestRunMessage:       strPtr(p.LatestRunMessage),
			LatestRunStartedAt:     strPtr(p.LatestRunStartedAt),
			LatestRunStatus:        strPtr(p.LatestRunStatus),
			LatestVersionNumber:    int64Ptr(p.LatestVersionNumber),
			Name:                   strPtr(p.Name),
			NumTasks:               int64Ptr(p.NumTasks),
			Paused:                 boolPtr(p.Paused),
			ProductId:              strPtr(p.ProductId),
			ProductName:            strPtr(p.ProductName),
			PublishedVersionNumber: int64Ptr(p.PublishedVersionNumber),
			Repository:             strPtr(p.Repository),
			Schedule:               rawToJSONString(p.Schedule),
			Sensors:                rawToJSONString(p.Sensors),
			StorageProvider:        strPtr(p.StorageProvider),
			TriggerEvents:          rawToJSONString(p.TriggerEvents),
			UpdatedAt:              strPtr(p.UpdatedAt),
			Webhook:                rawToJSONString(p.Webhook),
			YamlPath:               strPtr(p.YamlPath),
			state:                  attr.ValueStateKnown,
		}
		elements = append(elements, el)
	}

	// No explicit sorting; Terraform's set semantics treat ordering as irrelevant.
	model.Pipelines = types.SetValueMust(PipelinesType{}, elements)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
