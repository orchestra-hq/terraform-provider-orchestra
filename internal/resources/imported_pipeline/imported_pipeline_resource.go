package imported_pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra/client"
)

type ImportedPipelineResource struct {
	client *client.Client
}

var _ resource.Resource = &ImportedPipelineResource{}

func (r *ImportedPipelineResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_imported_pipeline"
	_ = ctx
}

func (r *ImportedPipelineResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = ImportedPipelineResourceSchema(ctx)
}

func (r *ImportedPipelineResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ImportedPipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model ImportedPipelineModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	repo := model.Repository.ValueString()
	yamlPath := model.YamlPath.ValueString()

	if repo == "" || model.Repository.IsNull() || model.Repository.IsUnknown() {
		resp.Diagnostics.AddError("Missing repository", "`repository` is required")
		return
	}
	if yamlPath == "" || model.YamlPath.IsNull() || model.YamlPath.IsUnknown() {
		resp.Diagnostics.AddError("Missing yaml_path", "`yaml_path` is required")
		return
	}

	storageProvider := model.StorageProvider.ValueString()

	body := map[string]any{
		"defaultBranch":   model.DefaultBranch.ValueString(),
		"repository":      repo,
		"storageProvider": storageProvider,
		"yamlPath":        yamlPath,
	}

	// Optional fields.
	if !model.Alias.IsNull() && !model.Alias.IsUnknown() && model.Alias.ValueString() != "" {
		body["alias"] = model.Alias.ValueString()
	}
	if !model.WorkingBranch.IsNull() && !model.WorkingBranch.IsUnknown() && model.WorkingBranch.ValueString() != "" {
		body["workingBranch"] = model.WorkingBranch.ValueString()
	}

	if err := r.client.DoJSON(ctx, http.MethodPost, "/pipelines/import", nil, body, nil); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to import pipeline: %s", err))
		return
	}

	// Hydrate by reading back the imported pipeline.
	if err := r.readIntoState(ctx, &model); err != nil {
		resp.Diagnostics.AddError("Read after create failed", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *ImportedPipelineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model ImportedPipelineModel
	if diags := req.State.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	if err := r.readIntoState(ctx, &model); err != nil {
		resp.Diagnostics.AddError("API error", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *ImportedPipelineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model ImportedPipelineModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	// Simplest safe behavior: re-import the pipeline definition.
	// Note: this depends on Orchestra making the import idempotent.
	repo := model.Repository.ValueString()
	yamlPath := model.YamlPath.ValueString()
	storageProvider := model.StorageProvider.ValueString()

	body := map[string]any{
		"defaultBranch":   model.DefaultBranch.ValueString(),
		"repository":      repo,
		"storageProvider": storageProvider,
		"yamlPath":        yamlPath,
	}

	if !model.Alias.IsNull() && !model.Alias.IsUnknown() && model.Alias.ValueString() != "" {
		body["alias"] = model.Alias.ValueString()
	}
	if !model.WorkingBranch.IsNull() && !model.WorkingBranch.IsUnknown() && model.WorkingBranch.ValueString() != "" {
		body["workingBranch"] = model.WorkingBranch.ValueString()
	}

	if err := r.client.DoJSON(ctx, http.MethodPost, "/pipelines/import", nil, body, nil); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to import pipeline: %s", err))
		return
	}

	if err := r.readIntoState(ctx, &model); err != nil {
		resp.Diagnostics.AddError("Read after update failed", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *ImportedPipelineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model ImportedPipelineModel
	if diags := req.State.Get(ctx, &model); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	alias := model.Alias.ValueString()
	if alias == "" {
		// Fallback: fetch alias from repo+yaml_path selectors.
		if err := r.readIntoState(ctx, &model); err != nil {
			resp.Diagnostics.AddError("API error", err.Error())
			return
		}
		alias = model.Alias.ValueString()
	}

	if alias == "" {
		return
	}

	if err := r.client.DoJSON(ctx, http.MethodDelete, fmt.Sprintf("/pipelines/%s", alias), nil, nil, nil); err != nil {
		resp.Diagnostics.AddError("API error", fmt.Sprintf("failed to delete pipeline: %s", err))
		return
	}
}

type pipelineReadResponse struct {
	Id              *string `json:"id"`
	Alias           *string `json:"alias"`
	Name            *string `json:"name"`
	NumTasks        *int64  `json:"numTasks"`
	CreatedAt       *string `json:"createdAt"`
	UpdatedAt       *string `json:"updatedAt"`
	Paused          *bool   `json:"paused"`
	StorageProvider *string `json:"storageProvider"`
}

func (r *ImportedPipelineResource) readIntoState(ctx context.Context, model *ImportedPipelineModel) error {
	if model.Repository.IsNull() || model.Repository.IsUnknown() || model.Repository.ValueString() == "" {
		return fmt.Errorf("repository must be set to read imported pipeline")
	}
	if model.YamlPath.IsNull() || model.YamlPath.IsUnknown() || model.YamlPath.ValueString() == "" {
		return fmt.Errorf("yaml_path must be set to read imported pipeline")
	}

	q := url.Values{}
	q.Set("repository", model.Repository.ValueString())
	q.Set("yaml_path", model.YamlPath.ValueString())

	var api []pipelineReadResponse
	if err := r.client.DoJSON(ctx, http.MethodGet, "/pipelines", q, nil, &api); err != nil {
		return err
	}

	if len(api) == 0 {
		return fmt.Errorf("pipeline not found for repository=%s yaml_path=%s", model.Repository.ValueString(), model.YamlPath.ValueString())
	}

	p := api[0]
	model.Id = types.StringValue(ptrString(p.Id))
	model.Name = types.StringValue(ptrString(p.Name))
	model.NumTasks = types.Int64Value(ptrInt64(p.NumTasks))
	model.Paused = types.BoolValue(ptrBool(p.Paused))
	model.CreatedAt = types.StringValue(ptrString(p.CreatedAt))
	model.UpdatedAt = types.StringValue(ptrString(p.UpdatedAt))
	model.Alias = types.StringValue(ptrString(p.Alias))
	model.StorageProvider = types.StringValue(ptrString(p.StorageProvider))
	model.Data = NewDataValueNull()

	return nil
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
