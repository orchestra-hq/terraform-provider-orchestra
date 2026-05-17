package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra/client"
)

type PipelineResource struct {
	client *client.Client
}

var _ resource.Resource = &PipelineResource{}

func (r *PipelineResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
	_ = ctx
}

func (r *PipelineResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = PipelineResourceSchema(ctx)
}

func (r *PipelineResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	_ = ctx
	if req.ProviderData == nil {
		resp.Diagnostics.AddError("Missing ProviderData", "ProviderData was not set for this resource")
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}

	r.client = c
}

type pipelineResponse struct {
	Alias           *string `json:"alias"`
	Id              *string `json:"id"`
	Name            *string `json:"name"`
	NumTasks        *int64  `json:"numTasks"`
	YamlPath        *string `json:"yamlPath"`
	CreatedAt       *string `json:"createdAt"`
	UpdatedAt       *string `json:"updatedAt"`
	Paused          *bool   `json:"paused"`
	StorageProvider *string `json:"storageProvider"`
}

func (r *PipelineResource) readByAlias(ctx context.Context, alias string) (pipelineResponse, error) {
	q := url.Values{}
	q.Set("alias", alias)

	var api []pipelineResponse
	if err := r.client.DoJSON(ctx, http.MethodGet, "/pipelines", q, nil, &api); err != nil {
		return pipelineResponse{}, err
	}

	if len(api) == 0 {
		return pipelineResponse{}, fmt.Errorf("pipeline not found for alias=%s", alias)
	}

	return api[0], nil
}

func (r *PipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model PipelineModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	alias := model.Alias.ValueString()
	if alias == "" || model.Alias.IsUnknown() || model.Alias.IsNull() {
		resp.Diagnostics.AddError("Missing alias", "`alias` is required")
		return
	}

	// NOTE: The generated TF schema represents `data` as an empty object.
	// Until the schema is extended, we can only send `{}` as the pipeline definition.
	var data map[string]any
	if !model.Data.IsNull() && !model.Data.IsUnknown() {
		data = map[string]any{}
	} else {
		data = map[string]any{}
	}

	storageProvider := "ORCHESTRA"
	if !model.StorageProvider.IsNull() && !model.StorageProvider.IsUnknown() && model.StorageProvider.ValueString() != "" {
		storageProvider = model.StorageProvider.ValueString()
	}

	body := map[string]any{
		"alias":           alias,
		"published":       model.Published.ValueBool(),
		"storageProvider": storageProvider,
		"data":            data,
	}

	if err := r.client.DoJSON(ctx, http.MethodPost, "/pipelines", nil, body, nil); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to create pipeline: %s", err))
		return
	}

	api, err := r.readByAlias(ctx, alias)
	if err != nil {
		resp.Diagnostics.AddError("Read after create failed", err.Error())
		return
	}

	model.Id = types.StringValue(ptrString(api.Id))
	model.Name = types.StringValue(ptrString(api.Name))
	model.NumTasks = types.Int64Value(ptrInt64(api.NumTasks))
	model.Paused = types.BoolValue(ptrBool(api.Paused))
	model.CreatedAt = types.StringValue(ptrString(api.CreatedAt))
	model.UpdatedAt = types.StringValue(ptrString(api.UpdatedAt))
	model.YamlPath = types.StringValue(ptrString(api.YamlPath))
	model.StorageProvider = types.StringValue(ptrString(api.StorageProvider))
	model.Alias = types.StringValue(ptrString(api.Alias))
	model.Data = NewDataValueNull()

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *PipelineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model PipelineModel
	if diags := req.State.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	alias := model.Alias.ValueString()
	if alias == "" {
		resp.Diagnostics.AddError("Missing alias", "Alias is required to read the pipeline")
		return
	}

	api, err := r.readByAlias(ctx, alias)
	if err != nil {
		resp.Diagnostics.AddError("API error", err.Error())
		return
	}

	model.Id = types.StringValue(ptrString(api.Id))
	model.Name = types.StringValue(ptrString(api.Name))
	model.NumTasks = types.Int64Value(ptrInt64(api.NumTasks))
	model.Paused = types.BoolValue(ptrBool(api.Paused))
	model.CreatedAt = types.StringValue(ptrString(api.CreatedAt))
	model.UpdatedAt = types.StringValue(ptrString(api.UpdatedAt))
	model.YamlPath = types.StringValue(ptrString(api.YamlPath))
	model.StorageProvider = types.StringValue(ptrString(api.StorageProvider))
	model.Alias = types.StringValue(ptrString(api.Alias))
	model.Data = NewDataValueNull()

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *PipelineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model PipelineModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	alias := model.Alias.ValueString()
	if alias == "" {
		resp.Diagnostics.AddError("Missing alias", "`alias` is required")
		return
	}

	body := map[string]any{
		"published":       model.Published.ValueBool(),
		"storageProvider": model.StorageProvider.ValueString(),
		"data":            map[string]any{},
	}

	if err := r.client.DoJSON(ctx, http.MethodPut, fmt.Sprintf("/pipelines/%s", alias), nil, body, nil); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to update pipeline: %s", err))
		return
	}

	api, err := r.readByAlias(ctx, alias)
	if err != nil {
		resp.Diagnostics.AddError("Read after update failed", err.Error())
		return
	}

	model.Id = types.StringValue(ptrString(api.Id))
	model.Name = types.StringValue(ptrString(api.Name))
	model.NumTasks = types.Int64Value(ptrInt64(api.NumTasks))
	model.Paused = types.BoolValue(ptrBool(api.Paused))
	model.CreatedAt = types.StringValue(ptrString(api.CreatedAt))
	model.UpdatedAt = types.StringValue(ptrString(api.UpdatedAt))
	model.YamlPath = types.StringValue(ptrString(api.YamlPath))
	model.StorageProvider = types.StringValue(ptrString(api.StorageProvider))
	model.Alias = types.StringValue(ptrString(api.Alias))
	model.Data = NewDataValueNull()

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *PipelineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model PipelineModel
	if diags := req.State.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	alias := model.Alias.ValueString()
	if alias == "" {
		return
	}

	if err := r.client.DoJSON(ctx, http.MethodDelete, fmt.Sprintf("/pipelines/%s", alias), nil, nil, nil); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to delete pipeline: %s", err))
		return
	}
}

func ptrString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func ptrInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func ptrBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
