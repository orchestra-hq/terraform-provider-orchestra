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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/enums"
)

// aliasPattern matches the API's constraint on pipeline aliases.
var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// folderPattern accepts only folders the API stores as written. It strips
// leading, trailing and doubled slashes and the spaces around each segment, so
// "finance/daily/" would come back as "finance/daily" and fail the apply.
var folderPattern = regexp.MustCompile(`^([^/\s]([^/]*[^/\s])?(/[^/\s]([^/]*[^/\s])?)*)?$`)

func PipelineResourceSchema(ctx context.Context) schema.Schema {
	_ = ctx
	return schema.Schema{
		Description:         "An Orchestra pipeline and its definition.",
		MarkdownDescription: "An Orchestra pipeline. Set `data` to keep its definition in Terraform, or `git` to sync it from a YAML file in a connected repository, which Orchestra then keeps up to date.",
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
			"git": schema.SingleNestedAttribute{
				Optional:            true,
				Description:         "Sync the definition from a YAML file in a connected repository. Terraform manages the link, not the file: changes merged to the file are picked up by Orchestra and are not drift. Removing it replaces the pipeline.",
				MarkdownDescription: "Sync the definition from a YAML file in a connected repository. Terraform manages the link, not the file: changes merged to the file are picked up by Orchestra and are not drift. Changing it updates the link in place. Removing it replaces the pipeline, since the API cannot move a pipeline back into Orchestra storage.",
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.ObjectRequest, resp *objectplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.StateValue.IsNull() && req.PlanValue.IsNull()
					}, "Removing git replaces the pipeline.", "Removing `git` replaces the pipeline."),
				},
				Attributes: map[string]schema.Attribute{
					"provider": schema.StringAttribute{
						Required:    true,
						Description: "Where the repository is hosted.",
						Validators: []validator.String{
							stringvalidator.OneOf(enums.StorageProviders...),
							stringvalidator.NoneOf("ORCHESTRA"),
						},
					},
					"repository":     gitString("The repository, as connected in Orchestra."),
					"default_branch": gitString("The branch to sync from."),
					"path":           gitString("The YAML file's path in the repository."),
				},
			},
			"folder": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "The folder the pipeline is filed in, such as finance/daily. Set it to an empty string for no folder. Leave unset to leave it as it is.",
				MarkdownDescription: "The folder the pipeline is filed in, such as `finance/daily`. Set it to `\"\"` for no folder. Leave unset to leave it as it is, so a pipeline filed in the Orchestra UI is not moved by the next apply. Changing it moves the pipeline without saving a new version of the definition.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(folderPattern, "must be a path such as finance/daily, with no leading, trailing or doubled slashes and no spaces around a segment"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"published": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "Whether to publish the definition. Every change to data saves a new version of the pipeline: true publishes it, false saves it as a draft and leaves the published version running. Only the published version runs on a schedule.",
				MarkdownDescription: "Whether to publish the definition. Every change to `data` saves a new version of the pipeline: `true` publishes it, `false` saves it as a draft and leaves the published version running. Only the published version runs on a schedule.",
			},
			"yaml_path": schema.StringAttribute{
				Computed:            true,
				Description:         "Path to the pipeline YAML file, assigned by Orchestra.",
				MarkdownDescription: "Path to the pipeline YAML file: assigned by Orchestra, or `git.path` for a git-backed pipeline.",
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
	Alias     types.String   `tfsdk:"alias"`
	Data      *definitionTF  `tfsdk:"data"`
	Git       *pipelineGitTF `tfsdk:"git"`
	Folder    types.String   `tfsdk:"folder"`
	Published types.Bool     `tfsdk:"published"`
	YamlPath  types.String   `tfsdk:"yaml_path"`

	Id                     types.String `tfsdk:"id"`
	Name                   types.String `tfsdk:"name"`
	NumTasks               types.Int64  `tfsdk:"num_tasks"`
	Paused                 types.Bool   `tfsdk:"paused"`
	LatestVersionNumber    types.Int64  `tfsdk:"latest_version_number"`
	PublishedVersionNumber types.Int64  `tfsdk:"published_version_number"`
	CreatedAt              types.String `tfsdk:"created_at"`
	UpdatedAt              types.String `tfsdk:"updated_at"`
}

type pipelineGitTF struct {
	Provider      types.String `tfsdk:"provider"`
	Repository    types.String `tfsdk:"repository"`
	DefaultBranch types.String `tfsdk:"default_branch"`
	Path          types.String `tfsdk:"path"`
}

func gitString(desc string) schema.StringAttribute {
	return schema.StringAttribute{Required: true, Description: desc, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}}
}
