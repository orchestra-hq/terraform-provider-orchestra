package integrationconnection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/client"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/enums"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

// IntegrationConnectionResource is one set of credentials for an integration,
// which pipeline tasks reference by ID.
type IntegrationConnectionResource struct {
	client *client.Client
}

var (
	_ resource.Resource                   = &IntegrationConnectionResource{}
	_ resource.ResourceWithImportState    = &IntegrationConnectionResource{}
	_ resource.ResourceWithValidateConfig = &IntegrationConnectionResource{}
)

type connectionModel struct {
	Id              types.String         `tfsdk:"id"`
	Integration     types.String         `tfsdk:"integration"`
	Name            types.String         `tfsdk:"name"`
	Parameters      jsontypes.Normalized `tfsdk:"parameters"`
	ParametersWO    jsontypes.Normalized `tfsdk:"parameters_wo"`
	ParametersWOVer types.Int64          `tfsdk:"parameters_wo_version"`
	CredentialStore types.String         `tfsdk:"credential_store"`
	IsDefault       types.Bool           `tfsdk:"is_default"`
	AuthStatus      types.String         `tfsdk:"auth_status"`
	Message         types.String         `tfsdk:"message"`
}

// wireConnection is components.schemas.SingleIntegrationAuthResponse, plus the
// redirectUrl of the IntegrationOAuthRedirectResponse that create and update
// return instead for an OAuth integration.
type wireConnection struct {
	ConnectionID    string          `json:"connectionId"`
	ConnectionName  string          `json:"connectionName"`
	IsDefault       bool            `json:"isDefault"`
	CredentialStore *string         `json:"credentialStore"`
	AuthStatus      string          `json:"authStatus"`
	Message         *string         `json:"message"`
	Parameters      json.RawMessage `json:"parameters"`
	RedirectURL     string          `json:"redirectUrl"`
}

func (r *IntegrationConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration_connection"
}

func (r *IntegrationConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A connection to an integration: the credentials pipeline tasks use to reach it.",
		MarkdownDescription: "A connection to an integration: the credentials pipeline tasks use to reach it, " +
			"referenced from a task's `connection`. Integrations that sign in through a browser (OAuth) cannot be " +
			"connected from Terraform; connect them in the Orchestra UI and look them up with " +
			"`orchestra_integration_connections`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The connection ID that tasks reference.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"integration": schema.StringAttribute{
				Required:      true,
				Description:   "The integration, e.g. SNOWFLAKE.",
				Validators:    []validator.String{stringvalidator.OneOf(enums.Integrations...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name.",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"parameters": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Optional:   true,
				Sensitive:  true,
				Description: "Auth parameters as a JSON object, keyed by the field IDs in the integration's documentation. " +
					"Stored in Terraform state; use parameters_wo to keep secrets out of it. Exactly one of the two is required. " +
					"Orchestra masks secrets when it returns them, so this is never read back: a change made in the UI is not detected.",
				MarkdownDescription: "Auth parameters as a JSON object, keyed by the field IDs listed under *Authentication parameters* " +
					"on the integration's [documentation page](https://docs.getorchestra.io/docs/integrations). Use `jsonencode({...})`. " +
					"Stored in Terraform state; use `parameters_wo` to keep secrets out of it. Exactly one of the two is required. " +
					"Orchestra masks secrets when it returns them, so this is never read back: a change made in the UI is not detected. " +
					"Changing it re-authenticates the connection.",
			},
			"parameters_wo": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Optional:   true,
				Sensitive:  true,
				WriteOnly:  true,
				Description: "parameters, write-only: sent to Orchestra but never stored in Terraform state or plan. " +
					"Requires Terraform 1.11 or later. Terraform cannot see a change to it, so bump parameters_wo_version to send new values.",
				MarkdownDescription: "`parameters`, write-only: sent to Orchestra but never stored in Terraform state or plan. " +
					"Requires Terraform 1.11 or later. Terraform cannot see a change to it, so bump `parameters_wo_version` to send new values.",
			},
			"parameters_wo_version": schema.Int64Attribute{
				Optional:            true,
				Description:         "Any number; changing it re-sends parameters_wo and re-authenticates the connection.",
				MarkdownDescription: "Any number; changing it re-sends `parameters_wo` and re-authenticates the connection.",
			},
			"credential_store": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Where the secrets live: ORCHESTRA stores them encrypted in Orchestra; an external store " +
					"takes that store's reference parameters in parameters instead.",
				Validators:    []validator.String{stringvalidator.OneOf(enums.CredentialStores...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"is_default": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Description: "Whether tasks that name no connection use this one. Can only be set to true: making another " +
					"connection the default is what clears it.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"auth_status": schema.StringAttribute{
				Computed:    true,
				Description: "Whether the credentials authenticated: " + strings.Join(enums.AuthStatuses, ", ") + ".",
			},
			"message": schema.StringAttribute{
				Computed:    true,
				Description: "Why authentication failed, when it did.",
			},
		},
	}
}

// ValidateConfig rejects is_default = false. The API can only promote a
// connection, so an apply asking for false could never be met.
func (r *IntegrationConnectionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var params, paramsWO jsontypes.Normalized
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("parameters"), &params)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("parameters_wo"), &paramsWO)...)
	if params.IsNull() == paramsWO.IsNull() && !params.IsUnknown() && !paramsWO.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("parameters"), "Set exactly one of parameters and parameters_wo",
			"Both carry the connection's auth parameters. Use parameters_wo to keep secrets out of Terraform state, or parameters on Terraform older than 1.11.")
	}

	var isDefault types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("is_default"), &isDefault)...)
	if !isDefault.IsNull() && !isDefault.IsUnknown() && !isDefault.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("is_default"), "is_default cannot be false",
			"Orchestra can only make a connection the default. Leave is_default unset, and set it to true on the connection that should be the default instead.")
	}
}

func (r *IntegrationConnectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *IntegrationConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan connectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]any{
		"connectionName": plan.Name.ValueString(),
		"parameters":     parameters(ctx, req.Config, plan, &resp.Diagnostics),
	}
	if !plan.CredentialStore.IsUnknown() {
		body["credentialStore"] = plan.CredentialStore.ValueString()
	}
	if resp.Diagnostics.HasError() {
		return
	}

	integration := url.PathEscape(plan.Integration.ValueString())
	var api wireConnection
	if err := r.client.DoJSON(ctx, http.MethodPost, "/integrations/"+integration+"/auth", nil, body, &api); err != nil {
		resp.Diagnostics.AddError("Cannot create integration connection", err.Error())
		return
	}
	if api.RedirectURL != "" {
		// The connection exists but waits for a browser sign-in Terraform
		// cannot do, so remove it rather than leave it half made.
		_ = r.client.DoJSON(ctx, http.MethodDelete, "/integrations/"+integration+"/auth/"+url.PathEscape(api.ConnectionID), nil, nil, nil)
		resp.Diagnostics.AddAttributeError(path.Root("integration"), "Integration signs in through a browser",
			fmt.Sprintf("%s authenticates with OAuth, which Terraform cannot complete. Connect it in the Orchestra UI and look it up with the orchestra_integration_connections data source.", plan.Integration.ValueString()))
		return
	}

	// Create takes no isDefault, so promote it separately.
	if plan.IsDefault.ValueBool() && !api.IsDefault {
		r.patch(ctx, plan.Integration.ValueString(), api.ConnectionID, map[string]any{"isDefault": true}, &api, &resp.Diagnostics)
	}
	applyConnection(&plan, api, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *IntegrationConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state connectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var api wireConnection
	err := r.client.DoJSON(ctx, http.MethodGet,
		"/integrations/"+url.PathEscape(state.Integration.ValueString())+"/auth/"+url.PathEscape(state.Id.ValueString()), nil, nil, &api)
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read integration connection", err.Error())
		return
	}

	state.Name = types.StringValue(api.ConnectionName)
	state.IsDefault = types.BoolValue(api.IsDefault)
	state.CredentialStore = tfconv.String(api.CredentialStore)
	if state.Name.IsNull() {
		// Only after an import. The masked secrets will not match the
		// configuration, so the next apply sends the real ones.
		state.Parameters = tfconv.RawJSON(api.Parameters)
	}
	applyConnection(&state, api, nil)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update sends one PATCH per changed group: the API accepts only one of the
// name, the default flag, or the parameters with their credential store at a
// time.
func (r *IntegrationConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state connectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Id = state.Id
	integration, id := plan.Integration.ValueString(), state.Id.ValueString()

	var api wireConnection
	var patches []map[string]any
	if !plan.Name.Equal(state.Name) {
		patches = append(patches, map[string]any{"connectionName": plan.Name.ValueString()})
	}
	if !plan.Parameters.Equal(state.Parameters) || !plan.ParametersWOVer.Equal(state.ParametersWOVer) ||
		!plan.CredentialStore.Equal(state.CredentialStore) {
		body := map[string]any{"parameters": parameters(ctx, req.Config, plan, &resp.Diagnostics)}
		if !plan.CredentialStore.IsUnknown() {
			body["credentialStore"] = plan.CredentialStore.ValueString()
		}
		patches = append(patches, body)
	}
	if plan.IsDefault.ValueBool() && !state.IsDefault.ValueBool() {
		patches = append(patches, map[string]any{"isDefault": true})
	}
	for _, body := range patches {
		if resp.Diagnostics.HasError() {
			return
		}
		r.patch(ctx, integration, id, body, &api, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	applyConnection(&plan, api, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// parameters is the auth parameters to send, from whichever attribute holds
// them. A write-only value is null in the plan by design, so it can only be
// read from the config.
func parameters(ctx context.Context, config tfsdk.Config, plan connectionModel, diags *diag.Diagnostics) json.RawMessage {
	if !plan.Parameters.IsNull() {
		return tfconv.JSONObject(plan.Parameters, path.Root("parameters"), diags)
	}
	var wo jsontypes.Normalized
	diags.Append(config.GetAttribute(ctx, path.Root("parameters_wo"), &wo)...)
	return tfconv.JSONObject(wo, path.Root("parameters_wo"), diags)
}

func (r *IntegrationConnectionResource) patch(ctx context.Context, integration, id string, body map[string]any, api *wireConnection, diags *diag.Diagnostics) {
	if err := r.client.DoJSON(ctx, http.MethodPatch, "/integrations/"+url.PathEscape(integration)+"/auth/"+url.PathEscape(id), nil, body, api); err != nil {
		diags.AddError("Cannot update integration connection", err.Error())
		return
	}
	if api.RedirectURL != "" {
		diags.AddAttributeError(path.Root("parameters"), "Integration signs in through a browser",
			fmt.Sprintf("%s authenticates with OAuth, which Terraform cannot complete. Reconnect it in the Orchestra UI.", integration))
	}
}

func (r *IntegrationConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state connectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DoJSON(ctx, http.MethodDelete,
		"/integrations/"+url.PathEscape(state.Integration.ValueString())+"/auth/"+url.PathEscape(state.Id.ValueString()), nil, nil, nil)
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot delete integration connection",
			err.Error()+"\n\nOrchestra will not delete an integration's default connection while it has others. Make another one the default first.")
	}
}

// ImportState takes INTEGRATION/connection_id: reading a connection needs both.
func (r *IntegrationConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	integration, id, ok := strings.Cut(req.ID, "/")
	if !ok || integration == "" || id == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use INTEGRATION/connection_id, e.g. SNOWFLAKE/snowflake_production_48213.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("integration"), integration)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// applyConnection copies the server-owned fields. A planned is_default stands:
// promoting another connection in the same apply can clear it, and the next
// refresh shows that. With diags, from a create or update, a connection whose
// credentials were rejected is a warning: it exists, but tasks using it fail.
func applyConnection(m *connectionModel, api wireConnection, diags *diag.Diagnostics) {
	m.Id = types.StringValue(api.ConnectionID)
	if m.IsDefault.IsUnknown() {
		m.IsDefault = types.BoolValue(api.IsDefault)
	}
	if m.CredentialStore.IsUnknown() {
		m.CredentialStore = tfconv.String(api.CredentialStore)
	}
	m.AuthStatus = types.StringValue(api.AuthStatus)
	m.Message = tfconv.String(api.Message)
	if diags != nil && api.AuthStatus == "FAILED" {
		diags.AddWarning("Integration connection did not authenticate",
			fmt.Sprintf("Orchestra saved %q but could not authenticate with it: %s", m.Name.ValueString(), m.Message.ValueString()))
	}
}
