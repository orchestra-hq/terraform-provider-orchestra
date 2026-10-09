package agents

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	client "github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

// SkillResource is an agent skill: a markdown document, either held by
// Orchestra or synced from a file in a connected repository.
type SkillResource struct {
	client *client.Client
}

var (
	_ resource.Resource                     = &SkillResource{}
	_ resource.ResourceWithImportState      = &SkillResource{}
	_ resource.ResourceWithConfigValidators = &SkillResource{}
)

type skillModel struct {
	Id          types.String `tfsdk:"id"`
	Markdown    types.String `tfsdk:"markdown"`
	Git         *skillGitTF  `tfsdk:"git"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

type skillGitTF struct {
	Repository    types.String `tfsdk:"repository"`
	DefaultBranch types.String `tfsdk:"default_branch"`
	Path          types.String `tfsdk:"path"`
}

// wireSkill is the part of components.schemas.SkillResponse the resource reads.
type wireSkill struct {
	SkillID         string  `json:"skillId"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Markdown        *string `json:"markdown"`
	StorageProvider string  `json:"storageProvider"`
	Repository      *string `json:"repository"`
	DefaultBranch   *string `json:"defaultBranch"`
	GitPath         *string `json:"gitPath"`
}

func (r *SkillResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_skill"
}

func (r *SkillResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	required := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Required: true, Description: desc, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}}
	}
	resp.Schema = schema.Schema{
		Description: "An agent skill: instructions an Orchestra agent follows, as a markdown document.",
		MarkdownDescription: "An agent skill: instructions an Orchestra agent follows, as a markdown document whose " +
			"frontmatter gives its `name` and `description`. Set `markdown` to keep the document in Terraform, or `git` to " +
			"sync it from a file in a connected repository, which Orchestra then keeps up to date. " +
			"See [Skills](https://docs.getorchestra.io/docs/ai-agents/skills).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The skill ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"markdown": schema.StringAttribute{
				Optional:            true,
				Description:         "The skill document: frontmatter with name and description, then the instructions.",
				MarkdownDescription: "The skill document: frontmatter with `name` and `description`, then the instructions. Usually `file(\"skills/<name>.md\")`.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"git": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Sync the skill from a file in a connected repository. Changing it replaces the skill, which detaches it from agents until they are applied again.",
				// The API cannot move a git-backed skill or turn one into an
				// Orchestra-held one, so any change here is a new skill.
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"repository":     required("The repository, as connected in Orchestra."),
					"default_branch": required("The branch to sync from."),
					"path":           required("The file's path in the repository."),
				},
			},
			"name":        schema.StringAttribute{Computed: true, Description: "The name from the document's frontmatter."},
			"description": schema.StringAttribute{Computed: true, Description: "The description from the document's frontmatter."},
		},
	}
}

func (r *SkillResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("markdown"), path.MatchRoot("git")),
	}
}

func (r *SkillResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SkillResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan skillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var api wireSkill
	var err error
	if plan.Git != nil {
		err = r.client.DoJSON(ctx, http.MethodPost, "/skills/import", nil, map[string]string{
			"repository":    plan.Git.Repository.ValueString(),
			"defaultBranch": plan.Git.DefaultBranch.ValueString(),
			"path":          plan.Git.Path.ValueString(),
		}, &api)
	} else {
		err = r.client.DoJSON(ctx, http.MethodPost, "/skills", nil, map[string]string{"markdown": plan.Markdown.ValueString()}, &api)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot create skill", err.Error())
		return
	}
	applySkill(&plan, api)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SkillResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state skillModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var api wireSkill
	if err := r.client.DoJSON(ctx, http.MethodGet, "/skills/"+url.PathEscape(state.Id.ValueString()), nil, nil, &api); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Cannot read skill", err.Error())
		return
	}
	// An Orchestra-held skill's document is Terraform's to manage, so a
	// change made in the UI is drift. A git-backed one belongs to the
	// repository and changes on every sync.
	if api.StorageProvider == "ORCHESTRA" {
		state.Markdown = types.StringPointerValue(api.Markdown)
	} else if state.Git == nil {
		// Only after an import. Overwriting a configured `git` with the API's
		// spelling of the same repository would plan a replacement.
		state.Git = &skillGitTF{
			Repository:    types.StringPointerValue(api.Repository),
			DefaultBranch: types.StringPointerValue(api.DefaultBranch),
			Path:          types.StringPointerValue(api.GitPath),
		}
	}
	applySkill(&state, api)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SkillResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state skillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Id = state.Id

	// Only an Orchestra-held skill gets here: any change to `git` replaces.
	var api wireSkill
	if err := r.client.DoJSON(ctx, http.MethodPatch, "/skills/"+url.PathEscape(state.Id.ValueString()), nil,
		map[string]string{"markdown": plan.Markdown.ValueString()}, &api); err != nil {
		resp.Diagnostics.AddError("Cannot update skill", err.Error())
		return
	}
	applySkill(&plan, api)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SkillResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state skillModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DoJSON(ctx, http.MethodDelete, "/skills/"+url.PathEscape(state.Id.ValueString()), nil, nil, nil)
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot delete skill", err.Error())
	}
}

// ImportState takes the skill ID.
func (r *SkillResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applySkill copies the server-owned fields. `markdown` keeps the planned
// value: it is what was sent, and Read refreshes it.
func applySkill(m *skillModel, api wireSkill) {
	m.Id = types.StringValue(api.SkillID)
	m.Name = types.StringValue(api.Name)
	m.Description = types.StringValue(api.Description)
}
