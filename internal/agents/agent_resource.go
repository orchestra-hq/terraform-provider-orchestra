// Package agents holds everything that calls the Agents API, which has its own
// spec (internal/apicontract/agents.yaml) and base URL. The contract tests
// check calls from this directory against that spec.
package agents

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/client"
)

// llmKeyTypes are the discriminator values of the spec's llmKeyType union
// (AccountDefaultLLMKey, OrchestraLLMKey, ConnectionLLMKey). They are `const`
// properties rather than an enum schema, so internal/enums has no list.
var llmKeyTypes = []string{"ACCOUNT_DEFAULT", "ORCHESTRA", "CONNECTION"}

var llmKeyAttrTypes = map[string]attr.Type{
	"type":          types.StringType,
	"integration":   types.StringType,
	"connection_id": types.StringType,
}

type AgentResource struct {
	client *client.Client
}

var (
	_ resource.Resource                = &AgentResource{}
	_ resource.ResourceWithImportState = &AgentResource{}
)

type agentModel struct {
	Id             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	SystemPrompt   types.String `tfsdk:"system_prompt"`
	ResponsePrompt types.String `tfsdk:"response_prompt"`
	AvatarPath     types.String `tfsdk:"avatar_path"`
	SamplePrompts  types.List   `tfsdk:"sample_prompts"`
	DefaultModelId types.String `tfsdk:"default_model_id"`
	DefaultEffort  types.String `tfsdk:"default_effort"`
	GitEnabled     types.Bool   `tfsdk:"git_enabled"`
	Secrets        types.Map    `tfsdk:"secrets"`
	SecretsWO      types.Map    `tfsdk:"secrets_wo"`
	SecretsWOVer   types.Int64  `tfsdk:"secrets_wo_version"`
	LLMKey         types.Object `tfsdk:"llm_key"`
	SkillIds       types.Set    `tfsdk:"skill_ids"`
	Integrations   types.Set    `tfsdk:"integrations"`
}

// wireAgent is the part of components.schemas.PublicAgentResponse the resource reads.
// `secrets` is left out on purpose: see apply.
type wireAgent struct {
	AgentID        string      `json:"agentId"`
	Name           string      `json:"name"`
	Description    *string     `json:"description"`
	SystemPrompt   *string     `json:"systemPrompt"`
	ResponsePrompt *string     `json:"responsePrompt"`
	AvatarPath     *string     `json:"avatarPath"`
	SamplePrompts  []string    `json:"samplePrompts"`
	DefaultModelID *string     `json:"defaultModelId"`
	DefaultEffort  *string     `json:"defaultEffort"`
	GitEnabled     bool        `json:"gitEnabled"`
	LLMKeyType     *wireLLMKey `json:"llmKeyType"`
}

// wireLLMKey flattens the oneOf of the three LLM key models, discriminated by
// `type`.
type wireLLMKey struct {
	Type         string  `json:"type"`
	Integration  *string `json:"integration,omitempty"`
	ConnectionID *string `json:"connectionId,omitempty"`
}

// WireSchemas pairs the spec schemas this package sends with the types that
// encode them, for internal/apicontract's property check. The request bodies
// themselves are built as maps, which the check reads from source instead.
var WireSchemas = map[string]any{
	"AccountDefaultLLMKey": wireLLMKey{},
	"OrchestraLLMKey":      wireLLMKey{},
	"ConnectionLLMKey":     wireLLMKey{},
}

func (r *AgentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent"
}

func (r *AgentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	setOfStrings := func(desc, md string) schema.SetAttribute {
		return schema.SetAttribute{
			ElementType:         types.StringType,
			Optional:            true,
			Computed:            true,
			Description:         desc,
			MarkdownDescription: md,
			PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
		}
	}
	serverDefaulted := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{
			Optional:      true,
			Computed:      true,
			Description:   desc,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}

	resp.Schema = schema.Schema{
		Description:         "An Orchestra AI agent: its prompts, skills, integrations and model.",
		MarkdownDescription: "An Orchestra AI agent: its prompts, skills, integrations and model. Agents run in Orchestra's sandbox; see [AI agents](https://docs.getorchestra.io/docs/ai-agents/agents).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The agent ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name":            schema.StringAttribute{Required: true, Description: "The agent's name."},
			"description":     schema.StringAttribute{Optional: true, Description: "What the agent is for."},
			"system_prompt":   schema.StringAttribute{Optional: true, Description: "Who the agent is and how it should approach its work."},
			"response_prompt": schema.StringAttribute{Optional: true, Description: "How the agent should format its answers."},
			"avatar_path":     serverDefaulted("The agent's image, from Orchestra's avatar catalogue."),
			"sample_prompts": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, nil)),
				Description: "Up to five example questions, each up to 500 characters, shown above the agent's chat box.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(5),
					listvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 500)),
				},
			},
			"default_model_id": serverDefaulted("The model the agent runs on, such as claude-opus-5. Unset uses Orchestra's default. Orchestra moves an agent off a retired model."),
			"default_effort":   serverDefaulted("How much reasoning the agent spends: low, medium, high, xhigh or max. Unset uses the model's default. Not every model supports every level."),
			"git_enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Let the agent use the workspace's git connection.",
			},
			"secrets": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Sensitive:   true,
				Description: "Secret environment variables available to the agent at runtime. Written but never read back, so a change made outside Terraform is not detected. " +
					"Stored in Terraform state; use secrets_wo to keep them out of it.",
				MarkdownDescription: "Secret environment variables available to the agent at runtime. Written but never read back, so a change made outside Terraform is not detected. " +
					"Stored in Terraform state; use `secrets_wo` to keep them out of it.",
			},
			"secrets_wo": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Sensitive:   true,
				WriteOnly:   true,
				Description: "secrets, write-only: sent to Orchestra but never stored in Terraform state or plan. " +
					"Requires Terraform 1.11 or later. Terraform cannot see a change to it, so bump secrets_wo_version to send new values.",
				MarkdownDescription: "`secrets`, write-only: sent to Orchestra but never stored in Terraform state or plan. " +
					"Requires Terraform 1.11 or later. Terraform cannot see a change to it, so bump `secrets_wo_version` to send new values.",
				Validators: []validator.Map{mapvalidator.ConflictsWith(path.MatchRoot("secrets"))},
			},
			"secrets_wo_version": schema.Int64Attribute{
				Optional:            true,
				Description:         "Any number; changing it re-sends secrets_wo.",
				MarkdownDescription: "Any number; changing it re-sends `secrets_wo`.",
			},
			"llm_key": schema.SingleNestedAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Which API key pays for the agent's runs. Defaults to the workspace's choice.",
				Default: objectdefault.StaticValue(types.ObjectValueMust(llmKeyAttrTypes, map[string]attr.Value{
					"type": types.StringValue("ACCOUNT_DEFAULT"), "integration": types.StringNull(), "connection_id": types.StringNull(),
				})),
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:            true,
						Description:         "ACCOUNT_DEFAULT follows the workspace's choice, ORCHESTRA uses Orchestra's key, CONNECTION one of the workspace's own connections.",
						MarkdownDescription: "`ACCOUNT_DEFAULT` follows the workspace's choice, `ORCHESTRA` uses Orchestra's key, and `CONNECTION` one of the workspace's own connections, named by `integration` and `connection_id`.",
						Validators:          []validator.String{stringvalidator.OneOf(llmKeyTypes...)},
					},
					"integration":   schema.StringAttribute{Optional: true, Description: "For CONNECTION: the connection's integration, such as ANTHROPIC."},
					"connection_id": schema.StringAttribute{Optional: true, Description: "For CONNECTION: the connection's ID."},
				},
			},
			"skill_ids": setOfStrings("Skills the agent can use. Unset leaves them as they are.",
				"Skills the agent can use, by ID: `orchestra_skill.<name>.id`, or a built-in one from the `orchestra_skills` data source. Unset leaves them as they are."),
			"integrations": setOfStrings("Integrations whose credentials the agent may use, such as SNOWFLAKE. Unset leaves them as they are.",
				"Integrations whose credentials the agent may use, such as `SNOWFLAKE`. Unset leaves them as they are."),
		},
	}
}

func (r *AgentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}
	r.client = c.Agents()
}

func (r *AgentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan agentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	body := agentBody(ctx, plan, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var api wireAgent
	if err := r.client.DoJSON(ctx, http.MethodPost, "/agents", nil, body, &api); err != nil {
		resp.Diagnostics.AddError("Cannot create agent", err.Error())
		return
	}
	plan.Id = types.StringValue(api.AgentID)

	// The agent exists from here on, so state is saved even if attaching its
	// skills or integrations fails; the error taints it.
	r.attach(ctx, &plan, &resp.Diagnostics)
	apply(&plan, api, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AgentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state agentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var api wireAgent
	if err := r.client.DoJSON(ctx, http.MethodGet, "/agents/"+url.PathEscape(state.Id.ValueString()), nil, nil, &api); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Cannot read agent", err.Error())
		return
	}
	state.SkillIds = types.SetUnknown(types.StringType)
	state.Integrations = types.SetUnknown(types.StringType)
	r.readAttached(ctx, &state, &resp.Diagnostics)
	apply(&state, api, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AgentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state agentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	body := agentBody(ctx, plan, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Id = state.Id

	var api wireAgent
	if err := r.client.DoJSON(ctx, http.MethodPatch, "/agents/"+url.PathEscape(state.Id.ValueString()), nil, body, &api); err != nil {
		resp.Diagnostics.AddError("Cannot update agent", err.Error())
		return
	}
	r.attach(ctx, &plan, &resp.Diagnostics)
	apply(&plan, api, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AgentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state agentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DoJSON(ctx, http.MethodDelete, "/agents/"+url.PathEscape(state.Id.ValueString()), nil, nil, nil)
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot delete agent", err.Error())
	}
}

func (r *AgentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// agentBody builds a CreateAgentRequest or PatchAgentRequest. An optional
// string left unset is sent as null, which clears it; a server-defaulted one
// still unknown at create is left out, so the server picks.
func agentBody(ctx context.Context, m agentModel, config tfsdk.Config, diags *diag.Diagnostics) map[string]any {
	body := map[string]any{"name": m.Name.ValueString(), "gitEnabled": m.GitEnabled.ValueBool()}
	for field, v := range map[string]types.String{
		"description": m.Description, "systemPrompt": m.SystemPrompt, "responsePrompt": m.ResponsePrompt,
		"avatarPath": m.AvatarPath, "defaultModelId": m.DefaultModelId, "defaultEffort": m.DefaultEffort,
	} {
		if !v.IsUnknown() {
			body[field] = v.ValueStringPointer()
		}
	}

	prompts := []string{}
	diags.Append(m.SamplePrompts.ElementsAs(ctx, &prompts, false)...)
	body["samplePrompts"] = prompts

	// secrets_wo is null in the plan, yet every PATCH must carry it or the secrets clear.
	secrets := m.Secrets
	if secrets.IsNull() {
		diags.Append(config.GetAttribute(ctx, path.Root("secrets_wo"), &secrets)...)
	}
	var values map[string]string
	diags.Append(secrets.ElementsAs(ctx, &values, false)...)
	body["secrets"] = values

	if !m.LLMKey.IsNull() && !m.LLMKey.IsUnknown() {
		a := m.LLMKey.Attributes()
		key := wireLLMKey{Type: a["type"].(types.String).ValueString()}
		key.Integration = a["integration"].(types.String).ValueStringPointer()
		key.ConnectionID = a["connection_id"].(types.String).ValueStringPointer()
		body["llmKeyType"] = key
	}
	return body
}

// attach sets the agent's skills and integrations when the plan knows them. A
// set left unset, so still unknown, is read back instead.
func (r *AgentResource) attach(ctx context.Context, m *agentModel, diags *diag.Diagnostics) {
	id := url.PathEscape(m.Id.ValueString())

	if !m.SkillIds.IsUnknown() && !m.SkillIds.IsNull() {
		var ids []string
		diags.Append(m.SkillIds.ElementsAs(ctx, &ids, false)...)
		body := make([]map[string]string, 0, len(ids))
		for _, s := range ids {
			body = append(body, map[string]string{"skillId": s})
		}
		if err := r.client.DoJSON(ctx, http.MethodPut, "/agents/"+id+"/skills", nil, body, nil); err != nil {
			diags.AddAttributeError(path.Root("skill_ids"), "Cannot set the agent's skills", err.Error())
			m.SkillIds = types.SetUnknown(types.StringType) // record what is actually attached
		}
	}

	if !m.Integrations.IsUnknown() && !m.Integrations.IsNull() {
		var names []string
		diags.Append(m.Integrations.ElementsAs(ctx, &names, false)...)
		body := make([]map[string]string, 0, len(names))
		for _, n := range names {
			body = append(body, map[string]string{"integration": n})
		}
		if err := r.client.DoJSON(ctx, http.MethodPut, "/agents/"+id+"/integrations", nil, body, nil); err != nil {
			diags.AddAttributeError(path.Root("integrations"), "Cannot set the agent's integrations", err.Error())
			m.Integrations = types.SetUnknown(types.StringType)
		}
	}

	r.readAttached(ctx, m, diags)
}

// readAttached fills whichever of skill_ids and integrations is still unknown
// from the API.
func (r *AgentResource) readAttached(ctx context.Context, m *agentModel, diags *diag.Diagnostics) {
	id := url.PathEscape(m.Id.ValueString())

	if m.SkillIds.IsUnknown() || m.SkillIds.IsNull() {
		var skills []struct {
			SkillID string `json:"skillId"`
		}
		if err := r.client.ListAll(ctx, "/agents/"+id+"/skills", nil, &skills); err != nil {
			diags.AddError("Cannot read the agent's skills", err.Error())
			return
		}
		ids := make([]string, len(skills))
		for i, s := range skills {
			ids[i] = s.SkillID
		}
		m.SkillIds = stringSet(ids, diags)
	}

	if m.Integrations.IsUnknown() || m.Integrations.IsNull() {
		var integrations []struct {
			Integration string `json:"integration"`
		}
		if err := r.client.ListAll(ctx, "/agents/"+id+"/integrations", nil, &integrations); err != nil {
			diags.AddError("Cannot read the agent's integrations", err.Error())
			return
		}
		names := make([]string, len(integrations))
		for i, s := range integrations {
			names[i] = s.Integration
		}
		m.Integrations = stringSet(names, diags)
	}
}

// apply copies a PublicAgentResponse onto the model.
//
// `secrets` is not read back. Whether the API returns them in full or masked is
// not something the spec says, and a masked value would fail every apply, so
// state keeps the configured value.
func apply(m *agentModel, api wireAgent, diags *diag.Diagnostics) {
	m.Id = types.StringValue(api.AgentID)
	m.Name = types.StringValue(api.Name)
	m.Description = types.StringPointerValue(api.Description)
	m.SystemPrompt = types.StringPointerValue(api.SystemPrompt)
	m.ResponsePrompt = types.StringPointerValue(api.ResponsePrompt)
	m.AvatarPath = types.StringPointerValue(api.AvatarPath)
	m.DefaultModelId = types.StringPointerValue(api.DefaultModelID)
	m.DefaultEffort = types.StringPointerValue(api.DefaultEffort)
	m.GitEnabled = types.BoolValue(api.GitEnabled)

	prompts := make([]attr.Value, len(api.SamplePrompts))
	for i, p := range api.SamplePrompts {
		prompts[i] = types.StringValue(p)
	}
	list, d := types.ListValue(types.StringType, prompts)
	diags.Append(d...)
	m.SamplePrompts = list

	key := wireLLMKey{Type: "ACCOUNT_DEFAULT"}
	if api.LLMKeyType != nil {
		key = *api.LLMKeyType
	}
	obj, d := types.ObjectValue(llmKeyAttrTypes, map[string]attr.Value{
		"type":          types.StringValue(key.Type),
		"integration":   types.StringPointerValue(key.Integration),
		"connection_id": types.StringPointerValue(key.ConnectionID),
	})
	diags.Append(d...)
	m.LLMKey = obj
}

func stringSet(values []string, diags *diag.Diagnostics) types.Set {
	elems := make([]attr.Value, len(values))
	for i, v := range values {
		elems[i] = types.StringValue(v)
	}
	set, d := types.SetValue(types.StringType, elems)
	diags.Append(d...)
	return set
}
