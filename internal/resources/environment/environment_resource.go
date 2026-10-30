package environment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/client"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

type EnvironmentResource struct {
	client *client.Client
}

var (
	_ resource.Resource                = &EnvironmentResource{}
	_ resource.ResourceWithConfigure   = &EnvironmentResource{}
	_ resource.ResourceWithImportState = &EnvironmentResource{}
)

func (r *EnvironmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
	_ = ctx
}

func (r *EnvironmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	_ = ctx
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}
	r.client = c
}

func (r *EnvironmentResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	_ = ctx
	resp.Schema = schema.Schema{
		Description:         "An Orchestra environment: a named set of values that pipeline runs resolve variables and credentials from.",
		MarkdownDescription: "An Orchestra environment: a named set of values that pipeline runs resolve `${{ ENV.* }}` variables and integration credentials from.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Environment name.",
			},
			"values": schema.MapNestedAttribute{
				Required:            true,
				Sensitive:           true,
				Description:         "Environment values keyed by variable name. Marked sensitive because integration_credential entries carry secrets.",
				MarkdownDescription: "Environment values keyed by variable name. The whole map is marked sensitive because `integration_credential` entries carry secrets.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Required:    true,
							Description: "Value type.",
							Validators: []validator.String{
								stringvalidator.OneOf("string", "int", "bool", "integration_credential"),
							},
						},
						"value": schema.StringAttribute{
							Required:            true,
							Description:         "The value, always written as a string. It is sent as the type named above.",
							MarkdownDescription: "The value, always written as a string. It is converted to the type named above before being sent — `type = \"int\"` with `value = \"3\"` sends the number `3`.",
						},
					},
				},
			},
			// No client-side default: Orchestra makes a workspace's first
			// environment the default, so a planned false would drift.
			"default_env": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "Whether this is the workspace's default environment. Leave unset to keep Orchestra's choice; it makes a workspace's first environment the default.",
				MarkdownDescription: "Whether this is the workspace's default environment. Leave unset to keep Orchestra's choice; it makes a workspace's first environment the default. Set `true` to promote this one. Only one environment can be the default; promoting one demotes the other outside Terraform's view.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},

			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The environment ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"account_id": schema.StringAttribute{
				Computed:      true,
				Description:   "Account the environment belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"allowed_branches": schema.ListAttribute{
				Computed:      true,
				ElementType:   types.StringType,
				Description:   "Branches allowed to run against this environment. Managed in the Orchestra UI.",
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"allow_unpublished_pipelines": schema.BoolAttribute{
				Computed:      true,
				Description:   "Whether unpublished pipelines may run against this environment. Managed in the Orchestra UI.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "When the environment was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the environment was last updated.",
			},
		},
	}
}

type environmentModel struct {
	Name                      types.String          `tfsdk:"name"`
	Values                    map[string]valueModel `tfsdk:"values"`
	DefaultEnv                types.Bool            `tfsdk:"default_env"`
	Id                        types.String          `tfsdk:"id"`
	AccountId                 types.String          `tfsdk:"account_id"`
	AllowedBranches           types.List            `tfsdk:"allowed_branches"`
	AllowUnpublishedPipelines types.Bool            `tfsdk:"allow_unpublished_pipelines"`
	CreatedAt                 types.String          `tfsdk:"created_at"`
	UpdatedAt                 types.String          `tfsdk:"updated_at"`
}

type valueModel struct {
	Type  types.String `tfsdk:"type"`
	Value types.String `tfsdk:"value"`
}

// environmentResponse is components.schemas.EnvironmentsResponse.
type environmentResponse struct {
	AccountId                 *string                 `json:"accountId"`
	EnvironmentId             *string                 `json:"environmentId"`
	Name                      *string                 `json:"name"`
	DefaultEnv                *bool                   `json:"defaultEnv"`
	AllowedBranches           []string                `json:"allowedBranches"`
	AllowUnpublishedPipelines *bool                   `json:"allowUnpublishedPipelines"`
	Values                    map[string]wireEnvValue `json:"values"`
	CreatedAt                 *string                 `json:"createdAt"`
	UpdatedAt                 *string                 `json:"updatedAt"`
}

// wireEnvValue is components.schemas.EnvironmentValueType. `value` is a union
// of string, bool, integer and number, so it is carried as `any`.
type wireEnvValue struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// WireSchemas pairs the spec schemas this package sends with the types that
// encode them, for internal/apicontract's property check. The request bodies
// themselves are built as maps, which the check reads from source instead.
var WireSchemas = map[string]any{"EnvironmentValueType": wireEnvValue{}}

// toWire converts the always-string Terraform value to the type the API expects.
func (v valueModel) toWire(p path.Path, diags *diag.Diagnostics) wireEnvValue {
	typ := tfconv.GoString(v.Type)
	raw := tfconv.GoString(v.Value)

	switch typ {
	case "int":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			diags.AddAttributeError(p.AtName("value"), "Invalid integer value",
				fmt.Sprintf("`type` is \"int\" but %q is not an integer.", raw))
			return wireEnvValue{Type: typ, Value: raw}
		}
		return wireEnvValue{Type: typ, Value: n}
	case "bool":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			diags.AddAttributeError(p.AtName("value"), "Invalid boolean value",
				fmt.Sprintf("`type` is \"bool\" but %q is not a boolean. Use \"true\" or \"false\".", raw))
			return wireEnvValue{Type: typ, Value: raw}
		}
		return wireEnvValue{Type: typ, Value: b}
	default:
		return wireEnvValue{Type: typ, Value: raw}
	}
}

// fromWire stringifies whichever scalar the API returned.
func (w wireEnvValue) fromWire() valueModel {
	var s string
	switch v := w.Value.(type) {
	case nil:
		s = ""
	case string:
		s = v
	case bool:
		s = strconv.FormatBool(v)
	case float64:
		// JSON numbers decode as float64; render integers without a decimal point.
		if v == float64(int64(v)) {
			s = strconv.FormatInt(int64(v), 10)
		} else {
			s = strconv.FormatFloat(v, 'f', -1, 64)
		}
	default:
		s = fmt.Sprint(v)
	}
	return valueModel{Type: types.StringValue(w.Type), Value: types.StringValue(s)}
}

// ValidateConfig rejects int/bool values whose spelling will not survive the
// round trip. The API stores them as JSON scalars, so "007" comes back as "7"
// and "TRUE" as "true" — which Terraform reports as an inconsistent result
// after apply. Catching it at plan time gives the user an actionable message
// instead.
func (r *EnvironmentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model environmentModel
	if diags := req.Config.Get(ctx, &model); diags.HasError() {
		// Unknown values (wired to another resource's output) cannot be
		// decoded yet; Create/Update validates again once they are known.
		return
	}

	for name, v := range model.Values {
		raw := tfconv.GoString(v.Value)
		p := path.Root("values").AtMapKey(name).AtName("value")

		switch tfconv.GoString(v.Type) {
		case "int":
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				resp.Diagnostics.AddAttributeError(p, "Invalid integer value",
					fmt.Sprintf("`type` is \"int\" but %q is not an integer.", raw))
				continue
			}
			if canonical := strconv.FormatInt(n, 10); canonical != raw {
				resp.Diagnostics.AddAttributeError(p, "Non-canonical integer value",
					fmt.Sprintf("Orchestra stores this as the number %s and returns %q, which would not match the %q in your configuration. Write it as %q.",
						canonical, canonical, raw, canonical))
			}
		case "bool":
			b, err := strconv.ParseBool(raw)
			if err != nil {
				resp.Diagnostics.AddAttributeError(p, "Invalid boolean value",
					fmt.Sprintf("`type` is \"bool\" but %q is not a boolean. Use \"true\" or \"false\".", raw))
				continue
			}
			if canonical := strconv.FormatBool(b); canonical != raw {
				resp.Diagnostics.AddAttributeError(p, "Non-canonical boolean value",
					fmt.Sprintf("Orchestra returns this as %q, which would not match the %q in your configuration. Write it as %q.",
						canonical, raw, canonical))
			}
		}
	}
}

func (r *EnvironmentResource) valuesToWire(values map[string]valueModel, diags *diag.Diagnostics) map[string]wireEnvValue {
	out := make(map[string]wireEnvValue, len(values))
	for name, v := range values {
		out[name] = v.toWire(path.Root("values").AtMapKey(name), diags)
	}
	return out
}

// apply copies the API response into the model.
//
// refresh distinguishes a Read (where the API is the source of truth and drift
// must be visible) from a Create/Update (where Terraform already has a planned
// value it must not contradict). Overwriting a planned value with the API's
// echo aborts the apply with "Provider produced inconsistent result after
// apply" — `default_env` flips when the server promotes an environment on its
// own, and `values` round-trip through JSON scalars.
func (r *EnvironmentResource) apply(m *environmentModel, api environmentResponse, refresh bool) {
	m.Id = tfconv.String(api.EnvironmentId)
	m.AccountId = tfconv.String(api.AccountId)
	m.Name = tfconv.String(api.Name)
	m.AllowedBranches = tfconv.StringList(api.AllowedBranches)
	m.AllowUnpublishedPipelines = tfconv.Bool(api.AllowUnpublishedPipelines)
	m.CreatedAt = tfconv.String(api.CreatedAt)
	m.UpdatedAt = tfconv.String(api.UpdatedAt)

	if refresh {
		m.DefaultEnv = tfconv.Bool(api.DefaultEnv)
		// A nil map means the environment has no values; that is drift worth
		// showing, so it is not guarded the way the create/update path is.
		m.Values = make(map[string]valueModel, len(api.Values))
		for name, v := range api.Values {
			m.Values[name] = v.fromWire()
		}
		return
	}

	// Create/Update: the planned `default_env` and `values` stand. They are
	// refreshed on the next Read.
	if m.DefaultEnv.IsUnknown() {
		m.DefaultEnv = tfconv.Bool(api.DefaultEnv)
	}
}

func (r *EnvironmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var plan environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]any{
		"name":   tfconv.GoString(plan.Name),
		"values": r.valuesToWire(plan.Values, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var api environmentResponse
	if err := r.client.DoJSON(ctx, http.MethodPost, "/environments", nil, body, &api); err != nil {
		resp.Diagnostics.AddError("Cannot create environment", err.Error())
		return
	}

	// Create has no defaultEnv field; apply it with a follow-up patch when set.
	if plan.DefaultEnv.ValueBool() {
		if err := r.patch(ctx, derefString(api.EnvironmentId), plan, &api, &resp.Diagnostics); err != nil {
			resp.Diagnostics.AddError("Cannot set environment as default", err.Error())
			return
		}
	}

	r.apply(&plan, api, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *EnvironmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := tfconv.GoString(state.Id)
	if id == "" {
		resp.Diagnostics.AddError("Cannot identify environment", "State has no `id` to read the environment by.")
		return
	}

	var api environmentResponse
	if err := r.client.DoJSON(ctx, http.MethodGet, "/environments/"+id, nil, nil, &api); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Cannot read environment", err.Error())
		return
	}

	r.apply(&state, api, true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *EnvironmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var plan, state environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Id = state.Id

	var api environmentResponse
	if err := r.patch(ctx, tfconv.GoString(state.Id), plan, &api, &resp.Diagnostics); err != nil {
		resp.Diagnostics.AddError("Cannot update environment", err.Error())
		return
	}
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(&plan, api, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// patch issues PATCH /environments/{id}. The API requires name, values and
// defaultEnv together, so every update sends all three.
func (r *EnvironmentResource) patch(ctx context.Context, id string, plan environmentModel, out *environmentResponse, diags *diag.Diagnostics) error {
	if id == "" {
		return fmt.Errorf("no environment ID")
	}

	body := map[string]any{
		"name":       tfconv.GoString(plan.Name),
		"values":     r.valuesToWire(plan.Values, diags),
		"defaultEnv": plan.DefaultEnv.ValueBool(),
	}

	return r.client.DoJSON(ctx, http.MethodPatch, "/environments/"+id, nil, body, out)
}

func (r *EnvironmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := tfconv.GoString(state.Id)
	if id == "" {
		return
	}

	if err := r.client.DoJSON(ctx, http.MethodDelete, "/environments/"+id, nil, nil, nil); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return
		}
		resp.Diagnostics.AddError("Cannot delete environment", err.Error())
	}
}

func (r *EnvironmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Supply an environment ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
