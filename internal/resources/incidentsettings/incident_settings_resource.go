// Package incidentsettings manages the workspace's incident settings: a
// singleton with GET and PATCH but no create or delete.
package incidentsettings

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/enums"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

// singletonID is the only ID this resource has: a workspace has exactly one
// set of incident settings.
const singletonID = "incident_settings"

// toggle is one boolean setting. They are listed rather than written out as
// struct fields because each is the same Optional+Computed boolean.
type toggle struct{ attr, field, desc string }

var toggles = []toggle{
	{"incidents_enabled", "incidentsEnabled", "Raise incidents. Off, failures still alert but are not grouped into incidents."},
	{"require_alert_configured", "requireAlertConfigured", "Only failures from pipelines with alerting configured can raise an incident."},
	{"require_default_branch_or_published_version", "requireDefaultBranchOrPublishedVersion", "Only runs on a Git-backed pipeline's default branch, or of an Orchestra-backed pipeline's published version, can raise an incident."},
	{"warnings_enabled", "warningsEnabled", "Let warnings open and join incidents."},
	{"group_warning_with_failed", "groupWarningWithFailed", "Group warnings into the same incidents as failures."},
	{"auto_resolve_on_success", "autoResolveOnSuccess", "Resolve an open incident once everything it covers has succeeded again."},
	{"auto_diagnose_incidents", "autoDiagnoseIncidents", "Have Orchestra's diagnoser investigate incidents as they open and grow."},
}

type IncidentSettingsResource struct {
	client *client.Client
}

var (
	_ resource.Resource                = &IncidentSettingsResource{}
	_ resource.ResourceWithImportState = &IncidentSettingsResource{}
)

func (r *IncidentSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_incident_settings"
}

func (r *IncidentSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// Every setting is Optional+Computed with no default. The workspace already
	// has a value for each, and the spec's declared defaults have been wrong
	// before, so an unset attribute is left as it is rather than reset.
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:      true,
			Description:   "Always incident_settings: a workspace has one set.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"environment_ids": schema.SetAttribute{
			ElementType:   types.StringType,
			Optional:      true,
			Computed:      true,
			Description:   "Only failures from these environments open or join an incident. Empty means every environment.",
			PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
		},
		"max_alerts_per_incident": schema.Int64Attribute{
			Optional:      true,
			Computed:      true,
			Description:   "The alert budget: live alerts sent to each destination per incident, from 1 to 50, before further updates are rolled into one notice.",
			Validators:    []validator.Int64{int64validator.Between(1, 50)},
			PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
		},
		"alert_budget_refill_cadence": schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			Description:         "How often the alert budget resets: daily, weekly or never. Never means once per incident.",
			MarkdownDescription: "How often the alert budget resets: `daily`, `weekly` or `never`. With `never` the budget is spent once per incident.",
			Validators:          []validator.String{stringvalidator.OneOf(enums.AlertBudgetRefillCadences...)},
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
	}
	for _, t := range toggles {
		attrs[t.attr] = schema.BoolAttribute{
			Optional:      true,
			Computed:      true,
			Description:   t.desc,
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
		}
	}

	resp.Schema = schema.Schema{
		Description: "The workspace's incident settings. There is one set per workspace, so declare this resource once.",
		MarkdownDescription: "The workspace's incident settings: whether incidents are raised, which failures can raise one, " +
			"and the alert budget.\n\n" +
			"There is one set per workspace, so declare this resource once. An attribute left unset keeps its current " +
			"value. Destroying the resource only removes it from state; the settings stay as they are.",
		Attributes: attrs,
	}
}

func (r *IncidentSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *IncidentSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.patch(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *IncidentSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.patch(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *IncidentSettingsResource) Read(ctx context.Context, _ resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}
	var api map[string]json.RawMessage
	if err := r.client.DoJSON(ctx, http.MethodGet, "/incident_settings", nil, nil, &api); err != nil {
		resp.Diagnostics.AddError("Cannot read incident settings", err.Error())
		return
	}
	setState(ctx, &resp.State, api, &resp.Diagnostics)
}

// Delete leaves the settings alone. There is nothing to delete: the API has no
// such endpoint, and resetting every setting to the spec's defaults would undo
// choices made before Terraform managed them.
func (r *IncidentSettingsResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *IncidentSettingsResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), singletonID)...)
}

// patch sends every setting the plan knows and stores the response. Unknowns
// are attributes unset on create; PATCH leaves omitted settings as they are.
func (r *IncidentSettingsResource) patch(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, diags *diag.Diagnostics) {
	if r.client == nil {
		diags.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	body := map[string]any{}
	for _, t := range toggles {
		var v types.Bool
		diags.Append(plan.GetAttribute(ctx, path.Root(t.attr), &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			body[t.field] = v.ValueBool()
		}
	}
	var ids types.Set
	diags.Append(plan.GetAttribute(ctx, path.Root("environment_ids"), &ids)...)
	if !ids.IsNull() && !ids.IsUnknown() {
		list := []string{} // an empty set is sent as [], meaning every environment
		diags.Append(ids.ElementsAs(ctx, &list, false)...)
		body["environmentIds"] = list
	}
	var budget types.Int64
	diags.Append(plan.GetAttribute(ctx, path.Root("max_alerts_per_incident"), &budget)...)
	if !budget.IsNull() && !budget.IsUnknown() {
		body["maxAlertsPerIncident"] = budget.ValueInt64()
	}
	var cadence types.String
	diags.Append(plan.GetAttribute(ctx, path.Root("alert_budget_refill_cadence"), &cadence)...)
	if !cadence.IsNull() && !cadence.IsUnknown() {
		body["alertBudgetRefillCadence"] = cadence.ValueString()
	}
	if diags.HasError() {
		return
	}

	var api map[string]json.RawMessage
	if err := r.client.DoJSON(ctx, http.MethodPatch, "/incident_settings", nil, body, &api); err != nil {
		diags.AddError("Cannot update incident settings", err.Error())
		return
	}
	setState(ctx, state, api, diags)
}

// setState writes an IncidentSettingsResponse into state. Every attribute is
// set, so no unknown from the plan survives the apply.
func setState(ctx context.Context, state *tfsdk.State, api map[string]json.RawMessage, diags *diag.Diagnostics) {
	diags.Append(state.SetAttribute(ctx, path.Root("id"), singletonID)...)

	for _, t := range toggles {
		v := types.BoolNull()
		var b bool
		if json.Unmarshal(api[t.field], &b) == nil {
			v = types.BoolValue(b)
		}
		diags.Append(state.SetAttribute(ctx, path.Root(t.attr), v)...)
	}

	var ids []string
	_ = json.Unmarshal(api["environmentIds"], &ids)
	elems := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		elems = append(elems, types.StringValue(id))
	}
	set, d := types.SetValue(types.StringType, elems)
	diags.Append(d...)
	diags.Append(state.SetAttribute(ctx, path.Root("environment_ids"), set)...)

	budget := types.Int64Null()
	var n int64
	if json.Unmarshal(api["maxAlertsPerIncident"], &n) == nil {
		budget = types.Int64Value(n)
	}
	diags.Append(state.SetAttribute(ctx, path.Root("max_alerts_per_incident"), budget)...)

	cadence := types.StringNull()
	var s string
	if json.Unmarshal(api["alertBudgetRefillCadence"], &s) == nil {
		cadence = types.StringValue(s)
	}
	diags.Append(state.SetAttribute(ctx, path.Root("alert_budget_refill_cadence"), cadence)...)
}
