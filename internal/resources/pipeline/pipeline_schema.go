// Hand-maintained. The OpenAPI codegen renders `PipelineModel.pipeline` — a
// map of (TaskGroupModel | TaskModel) — as an empty object, because Terraform
// has no union type. See definition_schema.go for how that union is modelled
// instead.

package pipeline

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// aliasPattern matches the API's constraint on pipeline aliases.
var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func PipelineResourceSchema(ctx context.Context) schema.Schema {
	_ = ctx
	return schema.Schema{
		Description:         "An Orchestra pipeline and its definition.",
		MarkdownDescription: "An Orchestra pipeline, with its definition in `data`.\n\nPipelines whose definition lives in a git repository are managed by Orchestra's git sync, not by Terraform.",
		Attributes: map[string]schema.Attribute{
			"alias": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "Unique identifier. Letters, numbers, underscores only. Max 255.",
				MarkdownDescription: "Unique identifier. Letters, numbers and underscores only, max 255 characters. Changing it replaces the pipeline.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(255),
					stringvalidator.RegexMatches(aliasPattern, "must contain only letters, numbers and underscores"),
				},
				// UseStateForUnknown is required alongside RequiresReplace on an
				// Optional+Computed attribute. Without it the value goes unknown
				// whenever anything else changes, compares unequal to the prior
				// value, and turns every in-place update into a destroy and
				// re-create — making Update unreachable.
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"data": definitionAttribute(),
			"published": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "Whether the pipeline is published and can be triggered.",
				MarkdownDescription: "Whether the pipeline is published and can be triggered. ",
			},
			"yaml_path": schema.StringAttribute{
				Computed:            true,
				Description:         "Path to the pipeline YAML file, assigned by Orchestra.",
				MarkdownDescription: "Path to the pipeline YAML file, assigned by Orchestra.",
				// No UseStateForUnknown: the path embeds the version number
				// (".../versions/2.yaml"), so it changes on every update.
				// Pinning the prior value fails the apply.
			},

			// Server-owned.
			"id": schema.StringAttribute{
				Computed:            true,
				Description:         "The pipeline ID.",
				MarkdownDescription: "The pipeline ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Computed:            true,
				Description:         "The pipeline name, taken from the definition.",
				MarkdownDescription: "The pipeline name, taken from `data.name` or from the YAML in the repository.",
			},
			"num_tasks": schema.Int64Attribute{
				Computed:    true,
				Description: "Number of tasks in the pipeline.",
			},
			"paused": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "Whether the pipeline is paused. A paused pipeline's schedules, sensors, trigger events and API starts do not start new runs. Leave unset to leave it as it is.",
				MarkdownDescription: "Whether the pipeline is paused. A paused pipeline's schedules, sensors, trigger events and API starts do not start new runs. Leave unset to leave it as it is, so a pause made in the Orchestra UI is not undone by the next apply.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"latest_version_number": schema.Int64Attribute{
				Computed:    true,
				Description: "Highest saved version number.",
			},
			"published_version_number": schema.Int64Attribute{
				Computed:    true,
				Description: "Version number currently published.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "When the pipeline was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the pipeline was last updated.",
			},
		},
	}
}

type PipelineModel struct {
	Alias     types.String  `tfsdk:"alias"`
	Data      *definitionTF `tfsdk:"data"`
	Published types.Bool    `tfsdk:"published"`
	YamlPath  types.String  `tfsdk:"yaml_path"`

	Id                     types.String `tfsdk:"id"`
	Name                   types.String `tfsdk:"name"`
	NumTasks               types.Int64  `tfsdk:"num_tasks"`
	Paused                 types.Bool   `tfsdk:"paused"`
	LatestVersionNumber    types.Int64  `tfsdk:"latest_version_number"`
	PublishedVersionNumber types.Int64  `tfsdk:"published_version_number"`
	CreatedAt              types.String `tfsdk:"created_at"`
	UpdatedAt              types.String `tfsdk:"updated_at"`
}
