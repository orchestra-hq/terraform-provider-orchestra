package pipeline

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

type PipelineDataSource struct {
	client *client.Client
}

var (
	_ datasource.DataSource              = &PipelineDataSource{}
	_ datasource.DataSourceWithConfigure = &PipelineDataSource{}
)

func (d *PipelineDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
	_ = ctx
}

func (d *PipelineDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	_ = ctx
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}
	d.client = c
}

func (d *PipelineDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	_ = ctx
	str := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, Description: desc}
	}
	num := func(desc string) schema.Attribute {
		return schema.Int64Attribute{Computed: true, Description: desc}
	}

	resp.Schema = schema.Schema{
		Description:         "A single pipeline, looked up by ID, alias, or repository and YAML path.",
		MarkdownDescription: "A single pipeline, looked up by ID, alias, or repository and YAML path. Supply exactly one selector.",
		Attributes: map[string]schema.Attribute{
			// Selectors.
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The pipeline ID. Supply it as a selector, or read it back from another selector.",
			},
			"alias": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The pipeline alias.",
			},
			"repository": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "Repository holding the definition. Must be supplied together with yaml_path.",
				MarkdownDescription: "Repository holding the definition. Must be supplied together with `yaml_path`.",
			},
			"yaml_path": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "Path to the pipeline YAML file. Must be supplied together with repository.",
				MarkdownDescription: "Path to the pipeline YAML file. Must be supplied together with `repository`.",
			},
			"version": schema.Int64Attribute{
				Optional:    true,
				Description: "Read a specific saved version rather than the current one.",
			},
			"branch": schema.StringAttribute{
				Optional:    true,
				Description: "Read from a specific branch, for git-backed pipelines.",
			},
			"commit": schema.StringAttribute{
				Optional:    true,
				Description: "Read from a specific commit, for git-backed pipelines.",
			},

			// Results.
			"name":                     str("The pipeline name."),
			"storage_provider":         str("Where the definition lives: ORCHESTRA or a git provider."),
			"default_branch":           str("Default branch of the repository."),
			"paused":                   schema.BoolAttribute{Computed: true, Description: "Whether the pipeline is paused."},
			"num_tasks":                num("Number of tasks in the pipeline."),
			"latest_version_number":    num("Highest saved version number."),
			"published_version_number": num("Version number currently published."),
			"product_id":               str("Product the pipeline belongs to."),
			"product_name":             str("Name of that product."),
			"created_at":               str("When the pipeline was created."),
			"updated_at":               str("When the pipeline was last updated."),
			"latest_run_id":            str("ID of the most recent run."),
			"latest_run_status":        str("Status of the most recent run."),
			"latest_run_message":       str("Message from the most recent run."),
			"latest_run_created_at":    str("When the most recent run was created."),
			"latest_run_started_at":    str("When the most recent run started."),
			"latest_run_completed_at":  str("When the most recent run completed."),
			"schedule":                 str("Summary of the pipeline's schedules."),
			"sensors":                  str("Summary of the pipeline's sensors."),
			"trigger_events":           str("Summary of the pipeline's trigger events."),
			"webhook":                  str("Summary of the pipeline's webhook trigger."),
			"inputs": schema.MapNestedAttribute{
				Computed:    true,
				Description: "Declared runtime inputs, keyed by input name.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type":     schema.StringAttribute{Computed: true, Description: "Data type of the input."},
						"optional": schema.BoolAttribute{Computed: true, Description: "Whether the input may be omitted."},
						"default": schema.StringAttribute{
							Computed:    true,
							CustomType:  jsontypes.NormalizedType{},
							Description: "Default value, JSON-encoded.",
						},
					},
				},
			},
		},
	}
}

type pipelineModel struct {
	Id         types.String `tfsdk:"id"`
	Alias      types.String `tfsdk:"alias"`
	Repository types.String `tfsdk:"repository"`
	YamlPath   types.String `tfsdk:"yaml_path"`
	Version    types.Int64  `tfsdk:"version"`
	Branch     types.String `tfsdk:"branch"`
	Commit     types.String `tfsdk:"commit"`

	Name                   types.String          `tfsdk:"name"`
	StorageProvider        types.String          `tfsdk:"storage_provider"`
	DefaultBranch          types.String          `tfsdk:"default_branch"`
	Paused                 types.Bool            `tfsdk:"paused"`
	NumTasks               types.Int64           `tfsdk:"num_tasks"`
	LatestVersionNumber    types.Int64           `tfsdk:"latest_version_number"`
	PublishedVersionNumber types.Int64           `tfsdk:"published_version_number"`
	ProductId              types.String          `tfsdk:"product_id"`
	ProductName            types.String          `tfsdk:"product_name"`
	CreatedAt              types.String          `tfsdk:"created_at"`
	UpdatedAt              types.String          `tfsdk:"updated_at"`
	LatestRunId            types.String          `tfsdk:"latest_run_id"`
	LatestRunStatus        types.String          `tfsdk:"latest_run_status"`
	LatestRunMessage       types.String          `tfsdk:"latest_run_message"`
	LatestRunCreatedAt     types.String          `tfsdk:"latest_run_created_at"`
	LatestRunStartedAt     types.String          `tfsdk:"latest_run_started_at"`
	LatestRunCompletedAt   types.String          `tfsdk:"latest_run_completed_at"`
	Schedule               types.String          `tfsdk:"schedule"`
	Sensors                types.String          `tfsdk:"sensors"`
	TriggerEvents          types.String          `tfsdk:"trigger_events"`
	Webhook                types.String          `tfsdk:"webhook"`
	Inputs                 map[string]inputModel `tfsdk:"inputs"`
}

type inputModel struct {
	Type     types.String         `tfsdk:"type"`
	Default  jsontypes.Normalized `tfsdk:"default"`
	Optional types.Bool           `tfsdk:"optional"`
}

type pipelineResponse struct {
	Id                     *string `json:"id"`
	Alias                  *string `json:"alias"`
	Name                   *string `json:"name"`
	YamlPath               *string `json:"yamlPath"`
	StorageProvider        *string `json:"storageProvider"`
	Repository             *string `json:"repository"`
	DefaultBranch          *string `json:"defaultBranch"`
	Paused                 *bool   `json:"paused"`
	NumTasks               *int64  `json:"numTasks"`
	LatestVersionNumber    *int64  `json:"latestVersionNumber"`
	PublishedVersionNumber *int64  `json:"publishedVersionNumber"`
	ProductId              *string `json:"productId"`
	ProductName            *string `json:"productName"`
	CreatedAt              *string `json:"createdAt"`
	UpdatedAt              *string `json:"updatedAt"`
	LatestRunId            *string `json:"latestRunId"`
	LatestRunStatus        *string `json:"latestRunStatus"`
	LatestRunMessage       *string `json:"latestRunMessage"`
	LatestRunCreatedAt     *string `json:"latestRunCreatedAt"`
	LatestRunStartedAt     *string `json:"latestRunStartedAt"`
	LatestRunCompletedAt   *string `json:"latestRunCompletedAt"`
	Schedule               *string `json:"schedule"`
	Sensors                *string `json:"sensors"`
	TriggerEvents          *string `json:"triggerEvents"`
	Webhook                *string `json:"webhook"`
	Inputs                 map[string]struct {
		Type     string `json:"type"`
		Default  any    `json:"default"`
		Optional *bool  `json:"optional"`
	} `json:"inputs"`
}

func (d *PipelineDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model pipelineModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	query := url.Values{}
	switch {
	case tfconv.GoString(model.Id) != "":
		query.Set("pipeline_id", tfconv.GoString(model.Id))
	case tfconv.GoString(model.Alias) != "":
		query.Set("alias", tfconv.GoString(model.Alias))
	case tfconv.GoString(model.Repository) != "" && tfconv.GoString(model.YamlPath) != "":
		query.Set("repository", tfconv.GoString(model.Repository))
		query.Set("yaml_path", tfconv.GoString(model.YamlPath))
	default:
		resp.Diagnostics.AddError(
			"Missing pipeline selector",
			"Set one of `id`, `alias`, or both `repository` and `yaml_path`.",
		)
		return
	}

	if v := tfconv.Int64Ptr(model.Version); v != nil {
		query.Set("version", strconv.FormatInt(*v, 10))
	}
	if v := tfconv.GoString(model.Branch); v != "" {
		query.Set("branch", v)
	}
	if v := tfconv.GoString(model.Commit); v != "" {
		query.Set("commit", v)
	}

	var api pipelineResponse
	if err := d.client.DoJSON(ctx, http.MethodGet, "/pipeline", query, nil, &api); err != nil {
		resp.Diagnostics.AddError("Cannot read pipeline", err.Error())
		return
	}

	model.Id = tfconv.String(api.Id)
	model.Alias = tfconv.String(api.Alias)
	model.Name = tfconv.String(api.Name)
	model.YamlPath = tfconv.String(api.YamlPath)
	model.StorageProvider = tfconv.String(api.StorageProvider)
	model.Repository = tfconv.String(api.Repository)
	model.DefaultBranch = tfconv.String(api.DefaultBranch)
	model.Paused = tfconv.Bool(api.Paused)
	model.NumTasks = tfconv.Int64(api.NumTasks)
	model.LatestVersionNumber = tfconv.Int64(api.LatestVersionNumber)
	model.PublishedVersionNumber = tfconv.Int64(api.PublishedVersionNumber)
	model.ProductId = tfconv.String(api.ProductId)
	model.ProductName = tfconv.String(api.ProductName)
	model.CreatedAt = tfconv.String(api.CreatedAt)
	model.UpdatedAt = tfconv.String(api.UpdatedAt)
	model.LatestRunId = tfconv.String(api.LatestRunId)
	model.LatestRunStatus = tfconv.String(api.LatestRunStatus)
	model.LatestRunMessage = tfconv.String(api.LatestRunMessage)
	model.LatestRunCreatedAt = tfconv.String(api.LatestRunCreatedAt)
	model.LatestRunStartedAt = tfconv.String(api.LatestRunStartedAt)
	model.LatestRunCompletedAt = tfconv.String(api.LatestRunCompletedAt)
	model.Schedule = tfconv.String(api.Schedule)
	model.Sensors = tfconv.String(api.Sensors)
	model.TriggerEvents = tfconv.String(api.TriggerEvents)
	model.Webhook = tfconv.String(api.Webhook)

	if api.Inputs != nil {
		model.Inputs = make(map[string]inputModel, len(api.Inputs))
		for name, in := range api.Inputs {
			model.Inputs[name] = inputModel{
				Type:     types.StringValue(in.Type),
				Default:  tfconv.JSON(in.Default),
				Optional: tfconv.Bool(in.Optional),
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
