package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

type PipelineResource struct {
	client *client.Client
}

var (
	_ resource.Resource                   = &PipelineResource{}
	_ resource.ResourceWithConfigure      = &PipelineResource{}
	_ resource.ResourceWithImportState    = &PipelineResource{}
	_ resource.ResourceWithValidateConfig = &PipelineResource{}
	_ resource.ResourceWithModifyPlan     = &PipelineResource{}
)

// uuidPattern distinguishes a pipeline ID from an alias on import.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

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
		// Configure is called with nil ProviderData during early validation.
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}

	r.client = c
}

// ---------- API response shapes ----------

// pipelineResponse is components.schemas.PipelineResponse. The create/update
// endpoints return CreateOrUpdatePipelineResponse, a superset that also carries
// the definition, so the same struct decodes both.
type pipelineResponse struct {
	Id                     *string         `json:"id"`
	Name                   *string         `json:"name"`
	NumTasks               *int64          `json:"numTasks"`
	YamlPath               *string         `json:"yamlPath"`
	CreatedAt              *string         `json:"createdAt"`
	UpdatedAt              *string         `json:"updatedAt"`
	Paused                 *bool           `json:"paused"`
	StorageProvider        *string         `json:"storageProvider"`
	Repository             *string         `json:"repository"`
	DefaultBranch          *string         `json:"defaultBranch"`
	Alias                  *string         `json:"alias"`
	Folder                 *string         `json:"folder"`
	LatestVersionNumber    *int64          `json:"latestVersionNumber"`
	PublishedVersionNumber *int64          `json:"publishedVersionNumber"`
	Data                   *wireDefinition `json:"data"`
}

// selector identifies a pipeline for the by-selector endpoints. The API accepts
// pipeline_id, alias, or the repository/yaml_path pair.
type selector struct {
	pipelineID string
	alias      string
	repository string
	yamlPath   string
}

func (s selector) query() url.Values {
	q := url.Values{}
	switch {
	case s.pipelineID != "":
		q.Set("pipeline_id", s.pipelineID)
	case s.alias != "":
		q.Set("alias", s.alias)
	case s.repository != "" && s.yamlPath != "":
		q.Set("repository", s.repository)
		q.Set("yaml_path", s.yamlPath)
	}
	return q
}

func (s selector) empty() bool {
	return s.pipelineID == "" && s.alias == "" && (s.repository == "" || s.yamlPath == "")
}

func (s selector) String() string {
	switch {
	case s.pipelineID != "":
		return "pipeline_id=" + s.pipelineID
	case s.alias != "":
		return "alias=" + s.alias
	default:
		return fmt.Sprintf("repository=%s yaml_path=%s", s.repository, s.yamlPath)
	}
}

func selectorFor(m PipelineModel) selector {
	return selector{
		pipelineID: strValue(m.Id),
		alias:      strValue(m.Alias),
		yamlPath:   strValue(m.YamlPath),
	}
}

func strValue(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

func isSet(v types.String) bool { return strValue(v) != "" }

// ---------- API calls ----------

// getPipeline reads a single pipeline via GET /pipeline. Note the singular
// path: GET /pipelines is a list endpoint and ignores selector parameters.
func (r *PipelineResource) getPipeline(ctx context.Context, s selector) (pipelineResponse, error) {
	var out pipelineResponse
	if err := r.client.DoJSON(ctx, http.MethodGet, "/pipeline", s.query(), nil, &out); err != nil {
		return pipelineResponse{}, err
	}
	if out.Id == nil {
		return pipelineResponse{}, fmt.Errorf("%w: no pipeline for %s", client.ErrNotFound, s)
	}
	return out, nil
}

// getDefinition reads the pipeline definition via GET /pipelines/data.
func (r *PipelineResource) getDefinition(ctx context.Context, s selector) (*wireDefinition, error) {
	var out wireDefinition
	if err := r.client.DoJSON(ctx, http.MethodGet, "/pipelines/data", s.query(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// reconcilePaused brings the pipeline's paused flag in line with the plan.
// POST and PUT /pipelines have no paused field, so this is always a second
// call. On success api is replaced by the pause response, which is newer.
func (r *PipelineResource) reconcilePaused(ctx context.Context, plan PipelineModel, api *pipelineResponse) error {
	if plan.Paused.IsNull() || plan.Paused.IsUnknown() {
		return nil // unset: leave it as it is
	}
	want := plan.Paused.ValueBool()
	if api.Paused != nil && *api.Paused == want {
		return nil
	}
	if api.Id == nil {
		return errors.New("the API did not return a pipeline ID to pause")
	}

	var out pipelineResponse
	err := r.client.DoJSON(ctx, http.MethodPut, "/pipelines/"+url.PathEscape(*api.Id)+"/pause", nil,
		map[string]bool{"paused": want}, &out)
	if err != nil {
		return err
	}
	out.Data = api.Data // PipelineResponse carries no definition
	*api = out
	return nil
}

// reconcileFolder moves the pipeline to the plan's folder. PUT /pipelines has
// no folder, so a change on update is always a second call. On success api is
// replaced by the move response, which is newer.
func (r *PipelineResource) reconcileFolder(ctx context.Context, plan PipelineModel, api *pipelineResponse) error {
	if plan.Folder.IsNull() || plan.Folder.IsUnknown() {
		return nil // unset: leave it as it is
	}
	want := plan.Folder.ValueString()
	// The API stores "" as no folder and then omits it.
	if api.Folder == nil && want == "" || api.Folder != nil && *api.Folder == want {
		return nil
	}
	if api.Id == nil {
		return errors.New("the API did not return a pipeline ID to move")
	}

	var out pipelineResponse
	err := r.client.DoJSON(ctx, http.MethodPut, "/pipelines/"+url.PathEscape(*api.Id)+"/folder", nil,
		map[string]string{"folder": want}, &out)
	if err != nil {
		return err
	}
	out.Data = api.Data // PipelineResponse carries no definition
	*api = out
	return nil
}

// definitionUnchanged reports whether `data` and `published` are as they were.
//
// The framework blanks every Computed attribute that config leaves unset (an
// anomaly's server-defaulted numerics, say) whenever anything in the resource
// changes, so the plan's unknowns are filled from state before comparing.
func definitionUnchanged(plan, state tftypes.Value) bool {
	filled, err := tftypes.Transform(plan, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if v.IsKnown() {
			return v, nil
		}
		if prior, _, err := tftypes.WalkAttributePath(state, p); err == nil {
			if pv, ok := prior.(tftypes.Value); ok {
				return pv, nil
			}
		}
		return v, nil
	})
	if err != nil {
		return false
	}
	for _, name := range []string{"data", "published"} {
		p := tftypes.NewAttributePath().WithAttributeName(name)
		a, _, errA := tftypes.WalkAttributePath(filled, p)
		b, _, errB := tftypes.WalkAttributePath(state, p)
		av, okA := a.(tftypes.Value)
		bv, okB := b.(tftypes.Value)
		if errA != nil || errB != nil || !okA || !okB || !av.Equal(bv) {
			return false
		}
	}
	return true
}

// ---------- validation ----------

func (r *PipelineResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model PipelineModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		// A config that still holds unknown values (one wired to another
		// resource's output, say) cannot be decoded into the model. There is
		// nothing to validate yet, and the same config is checked again in
		// Create/Update once the values are known.
		return
	}

	if model.Data == nil {
		return
	}

	// IDs are merged into the API's single `pipeline` object, so they must be
	// unique across both maps.
	for id := range model.Data.Tasks {
		if _, clash := model.Data.TaskGroups[id]; clash {
			resp.Diagnostics.AddAttributeError(
				path.Root("data").AtName("tasks").AtMapKey(id),
				"Duplicate pipeline entry ID",
				fmt.Sprintf("%q is used by both `task_groups` and `tasks`. The API merges them into one object, so IDs must be unique across both.", id),
			)
		}
	}

	// The API only accepts a fixed list of values on a string input.
	for name, in := range model.Data.Inputs {
		if in.Options != nil && !in.Type.IsUnknown() && in.Type.ValueString() != "string" {
			resp.Diagnostics.AddAttributeError(
				path.Root("data").AtName("inputs").AtMapKey(name).AtName("options"),
				"Options on a non-string input",
				fmt.Sprintf("Input %q has type %q. `options` is only valid on inputs of type \"string\"; remove it or change the type.", name, in.Type.ValueString()),
			)
		}
	}

	if len(model.Data.TaskGroups) == 0 && len(model.Data.Tasks) == 0 {
		resp.Diagnostics.AddAttributeError(
			path.Root("data"),
			"Empty pipeline",
			"Set at least one entry in `task_groups` or `tasks`.",
		)
	}
}

// ModifyPlan checks the definition against POST /pipelines/schema, so an
// invalid pipeline is reported at plan time rather than failing the apply.
//
// Findings are warnings, not errors. That endpoint also validates
// account-specific references when a credential is supplied, so a definition
// naming an environment created in the same apply would otherwise fail a plan
// that is actually fine — and there would be no way to opt out.
func (r *PipelineResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if r.client == nil || req.Plan.Raw.IsNull() {
		// No client during early validation; a null plan means destroy.
		return
	}

	var plan PipelineModel
	if diags := req.Plan.Get(ctx, &plan); diags.HasError() {
		// Values still unknown at plan time; nothing to validate yet.
		return
	}
	if plan.Data == nil {
		return
	}

	var encodeDiags diag.Diagnostics
	def := definitionToWire(plan.Data, path.Root("data"), &encodeDiags)
	if encodeDiags.HasError() || def == nil {
		// Surfaced properly by ValidateConfig and again at apply.
		return
	}

	err := r.client.DoJSON(ctx, http.MethodPost, "/pipelines/schema", nil, def, nil)
	if err == nil {
		return
	}

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode == http.StatusTooManyRequests {
		// The endpoint is rate limited to 20 requests per minute per IP, and a
		// transient network failure is not a config problem either.
		return
	}

	resp.Diagnostics.AddAttributeWarning(
		path.Root("data"),
		"Orchestra rejected this pipeline definition",
		fmt.Sprintf(
			"POST /pipelines/schema returned %d:\n\n%s\n\n"+
				"The apply will fail unless this is a reference that only exists once the rest of this plan is applied.",
			apiErr.StatusCode, strings.TrimSpace(apiErr.Body),
		),
	)
}

// ---------- CRUD ----------

func (r *PipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var plan PipelineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var api pipelineResponse

	{
		def := definitionToWire(plan.Data, path.Root("data"), &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if def == nil {
			resp.Diagnostics.AddAttributeError(path.Root("data"), "Missing definition",
				"`data` is required when creating a pipeline.")
			return
		}

		body := map[string]any{
			"data":            def,
			"published":       plan.Published.ValueBool(),
			"storageProvider": "ORCHESTRA",
		}
		if isSet(plan.Alias) {
			body["alias"] = strValue(plan.Alias)
		}
		if isSet(plan.Folder) {
			body["folder"] = strValue(plan.Folder)
		}

		if err := r.client.DoJSON(ctx, http.MethodPost, "/pipelines", nil, body, &api); err != nil {
			resp.Diagnostics.AddError("Cannot create pipeline", err.Error())
			return
		}
	}

	if err := r.reconcilePaused(ctx, plan, &api); err != nil {
		// The pipeline exists, so state is still saved below. The error taints
		// it, and the next apply replaces it.
		resp.Diagnostics.AddAttributeError(path.Root("paused"), "Cannot pause pipeline",
			"The pipeline was created but its paused flag could not be set: "+err.Error())
	}

	// Checked on its own: a failed pause above must still save state.
	var applyDiags diag.Diagnostics
	r.apply(ctx, &plan, api, &applyDiags)
	resp.Diagnostics.Append(applyDiags...)
	if applyDiags.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PipelineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var state PipelineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	s := selectorFor(state)
	if s.empty() {
		resp.Diagnostics.AddError("Cannot identify pipeline",
			"State has no `id`, `alias`, or `repository`/`yaml_path` to read the pipeline by.")
		return
	}

	api, err := r.getPipeline(ctx, s)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			// Deleted outside Terraform: drop it so the next plan recreates it.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Cannot read pipeline", err.Error())
		return
	}

	def, err := r.getDefinition(ctx, s)
	if err != nil {
		if !errors.Is(err, client.ErrNotFound) {
			resp.Diagnostics.AddError("Cannot read pipeline definition", err.Error())
			return
		}
	} else {
		api.Data = def
	}

	r.apply(ctx, &state, api, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PipelineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var plan, state PipelineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Carry the server-assigned ID across, since the plan leaves it unchanged.
	plan.Id = state.Id

	var api pipelineResponse

	if definitionUnchanged(req.Plan.Raw, req.State.Raw) {
		// Only `paused` or `folder` changed. Every PUT /pipelines saves a new
		// version of the definition, so pausing or moving alone should not go
		// through it. State holds the definition with its server-computed
		// values; the plan has those as unknown.
		plan.Data = state.Data
		api = responseFrom(state)
	} else {
		def := definitionToWire(plan.Data, path.Root("data"), &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if def == nil {
			resp.Diagnostics.AddAttributeError(path.Root("data"), "Missing definition",
				"`data` is required when updating a pipeline.")
			return
		}

		body := map[string]any{
			"data":      def,
			"published": plan.Published.ValueBool(),
		}
		// The selector goes in the body for this endpoint, not the path.
		if isSet(state.Id) {
			body["pipelineId"] = strValue(state.Id)
		} else {
			body["alias"] = strValue(state.Alias)
		}

		if err := r.client.DoJSON(ctx, http.MethodPut, "/pipelines", nil, body, &api); err != nil {
			resp.Diagnostics.AddError("Cannot update pipeline", err.Error())
			return
		}
	}

	if err := r.reconcilePaused(ctx, plan, &api); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("paused"), "Cannot pause pipeline",
			"The pipeline's paused flag could not be set: "+err.Error())
	}
	if err := r.reconcileFolder(ctx, plan, &api); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("folder"), "Cannot move pipeline",
			"The pipeline could not be moved to its folder: "+err.Error())
	}

	// Checked on its own: a failed pause or move above must still save state.
	// api holds the paused flag and folder as they actually are, and any
	// definition change above has landed.
	var applyDiags diag.Diagnostics
	r.apply(ctx, &plan, api, &applyDiags)
	resp.Diagnostics.Append(applyDiags...)
	if applyDiags.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PipelineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var state PipelineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	s := selectorFor(state)
	if s.empty() {
		return
	}

	// Selector goes in the query string for DELETE /pipelines.
	if err := r.client.DoJSON(ctx, http.MethodDelete, "/pipelines", s.query(), nil, nil); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return
		}
		resp.Diagnostics.AddError("Cannot delete pipeline", err.Error())
	}
}

func (r *PipelineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Supply a pipeline alias or pipeline ID.")
		return
	}

	if uuidPattern.MatchString(id) {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("alias"), id)...)
}

// apply copies an API response onto the model, leaving configured-only fields
// (published, working_branch) as the caller set them.
func (r *PipelineResource) apply(ctx context.Context, m *PipelineModel, api pipelineResponse, diags *diag.Diagnostics) {
	_ = ctx

	m.Id = tfString(api.Id)
	m.Name = tfString(api.Name)
	m.NumTasks = tfInt64(api.NumTasks)
	m.Paused = tfBool(api.Paused)
	m.CreatedAt = tfString(api.CreatedAt)
	m.UpdatedAt = tfString(api.UpdatedAt)
	m.YamlPath = tfString(api.YamlPath)
	m.LatestVersionNumber = tfInt64(api.LatestVersionNumber)
	m.PublishedVersionNumber = tfInt64(api.PublishedVersionNumber)

	// These are Optional+Computed and the API omits them for some pipeline
	// shapes (a pipeline with no alias, say).
	// settle() makes sure an unset one lands in state as null rather than
	// staying unknown, which Terraform rejects after apply.
	m.Alias = settle(api.Alias, m.Alias)
	m.Folder = folderOf(api.Folder, m.Folder)

	// `published` is write-only: PipelineResponse does not carry it, so there is
	// nothing to refresh from and the configured value stands.
	if m.Published.IsNull() || m.Published.IsUnknown() {
		m.Published = types.BoolValue(false)
	}

	if api.Data != nil {
		def, err := definitionFromWire(api.Data)
		if err != nil {
			diags.AddAttributeError(path.Root("data"), "Cannot decode pipeline definition", err.Error())
			return
		}
		m.Data = def
	}
}

// responseFrom is the pipeline as state last saw it, for an update that sends
// no definition. A pause or move replaces it with the API's response; when
// neither is needed (a planned "" folder against a null one, say) it is all
// there is to apply.
func responseFrom(m PipelineModel) pipelineResponse {
	return pipelineResponse{
		Id:                     m.Id.ValueStringPointer(),
		Name:                   m.Name.ValueStringPointer(),
		NumTasks:               m.NumTasks.ValueInt64Pointer(),
		YamlPath:               m.YamlPath.ValueStringPointer(),
		CreatedAt:              m.CreatedAt.ValueStringPointer(),
		UpdatedAt:              m.UpdatedAt.ValueStringPointer(),
		Paused:                 m.Paused.ValueBoolPointer(),
		Alias:                  m.Alias.ValueStringPointer(),
		Folder:                 m.Folder.ValueStringPointer(),
		LatestVersionNumber:    m.LatestVersionNumber.ValueInt64Pointer(),
		PublishedVersionNumber: m.PublishedVersionNumber.ValueInt64Pointer(),
	}
}

// settle resolves an Optional+Computed string: the API's value when it has
// one, the planned value when that is known, otherwise null.
func settle(fromAPI *string, planned types.String) types.String {
	if fromAPI != nil {
		return types.StringValue(*fromAPI)
	}
	return known(planned)
}

// folderOf resolves the Optional+Computed folder. The API omits a pipeline's
// folder when it has none, which is what a planned "" asks for.
func folderOf(fromAPI *string, planned types.String) types.String {
	if fromAPI != nil {
		return types.StringValue(*fromAPI)
	}
	if planned.IsNull() || !planned.IsUnknown() && planned.ValueString() == "" {
		return planned
	}
	return types.StringNull()
}

// known converts an unknown value to null. An unknown left in state after
// apply is rejected by Terraform as an inconsistent result.
func known(v types.String) types.String {
	if v.IsUnknown() {
		return types.StringNull()
	}
	return v
}
