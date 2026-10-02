// Hand-maintained. The codegen renders `inputs` (a map of PipelineInputModel)
// as an empty object that can never carry a value, so it is typed out here.

package pipelines

import (
	"context"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

type PipelinesDataSource struct {
	client *client.Client
}

var (
	_ datasource.DataSource              = &PipelinesDataSource{}
	_ datasource.DataSourceWithConfigure = &PipelinesDataSource{}
)

func (d *PipelinesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipelines"
	_ = ctx
}

func (d *PipelinesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *PipelinesDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	_ = ctx
	resp.Schema = schema.Schema{
		Description:         "Pipelines in the workspace, optionally filtered by name. All matching pages are fetched.",
		MarkdownDescription: "Pipelines in the workspace, optionally filtered by name. All matching pages are fetched.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by a case-insensitive substring match on the pipeline name.",
			},
			"page_size": schema.Int64Attribute{
				Optional:            true,
				Description:         "Items fetched per request (1-100). Every page is fetched regardless; this only tunes request size.",
				MarkdownDescription: "Items fetched per request (1-100, default 100). Every matching page is fetched regardless; this only tunes the request size.",
			},
			"pipelines": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Matching pipelines, sorted by name.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: pipelineAttributes(),
				},
			},
		},
	}
}

func pipelineAttributes() map[string]schema.Attribute {
	str := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, Description: desc}
	}
	num := func(desc string) schema.Attribute {
		return schema.Int64Attribute{Computed: true, Description: desc}
	}

	return map[string]schema.Attribute{
		"id":                       str("The pipeline ID."),
		"alias":                    str("The pipeline alias."),
		"name":                     str("The pipeline name."),
		"yaml_path":                str("Path to the pipeline YAML file."),
		"storage_provider":         str("Where the definition lives: ORCHESTRA or a git provider."),
		"repository":               str("Repository holding the definition, for git-backed pipelines."),
		"default_branch":           str("Default branch of that repository."),
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
						Description: "Default value, JSON-encoded. The API allows any type here.",
					},
				},
			},
		},
	}
}

type pipelinesModel struct {
	Name      types.String `tfsdk:"name"`
	PageSize  types.Int64  `tfsdk:"page_size"`
	Pipelines []pipelineTF `tfsdk:"pipelines"`
}

type pipelineTF struct {
	Id                     types.String             `tfsdk:"id"`
	Alias                  types.String             `tfsdk:"alias"`
	Name                   types.String             `tfsdk:"name"`
	YamlPath               types.String             `tfsdk:"yaml_path"`
	StorageProvider        types.String             `tfsdk:"storage_provider"`
	Repository             types.String             `tfsdk:"repository"`
	DefaultBranch          types.String             `tfsdk:"default_branch"`
	Paused                 types.Bool               `tfsdk:"paused"`
	NumTasks               types.Int64              `tfsdk:"num_tasks"`
	LatestVersionNumber    types.Int64              `tfsdk:"latest_version_number"`
	PublishedVersionNumber types.Int64              `tfsdk:"published_version_number"`
	ProductId              types.String             `tfsdk:"product_id"`
	ProductName            types.String             `tfsdk:"product_name"`
	CreatedAt              types.String             `tfsdk:"created_at"`
	UpdatedAt              types.String             `tfsdk:"updated_at"`
	LatestRunId            types.String             `tfsdk:"latest_run_id"`
	LatestRunStatus        types.String             `tfsdk:"latest_run_status"`
	LatestRunMessage       types.String             `tfsdk:"latest_run_message"`
	LatestRunCreatedAt     types.String             `tfsdk:"latest_run_created_at"`
	LatestRunStartedAt     types.String             `tfsdk:"latest_run_started_at"`
	LatestRunCompletedAt   types.String             `tfsdk:"latest_run_completed_at"`
	Schedule               types.String             `tfsdk:"schedule"`
	Sensors                types.String             `tfsdk:"sensors"`
	TriggerEvents          types.String             `tfsdk:"trigger_events"`
	Webhook                types.String             `tfsdk:"webhook"`
	Inputs                 map[string]pipelineInput `tfsdk:"inputs"`
}

type pipelineInput struct {
	Type     types.String         `tfsdk:"type"`
	Default  jsontypes.Normalized `tfsdk:"default"`
	Optional types.Bool           `tfsdk:"optional"`
}

// pipelineResponse is components.schemas.PipelineResponse.
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

func (d *PipelinesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model pipelinesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	query := url.Values{}
	if name := tfconv.GoString(model.Name); name != "" {
		query.Set("name", name)
	}
	if p := tfconv.Int64Ptr(model.PageSize); p != nil {
		query.Set("page_size", strconv.FormatInt(*p, 10))
	}

	var api []pipelineResponse
	if err := d.client.ListAll(ctx, "/pipelines", query, &api); err != nil {
		resp.Diagnostics.AddError("Cannot list pipelines", err.Error())
		return
	}

	model.Pipelines = make([]pipelineTF, 0, len(api))
	for _, p := range api {
		entry := pipelineTF{
			Id:                     tfconv.String(p.Id),
			Alias:                  tfconv.String(p.Alias),
			Name:                   tfconv.String(p.Name),
			YamlPath:               tfconv.String(p.YamlPath),
			StorageProvider:        tfconv.String(p.StorageProvider),
			Repository:             tfconv.String(p.Repository),
			DefaultBranch:          tfconv.String(p.DefaultBranch),
			Paused:                 tfconv.Bool(p.Paused),
			NumTasks:               tfconv.Int64(p.NumTasks),
			LatestVersionNumber:    tfconv.Int64(p.LatestVersionNumber),
			PublishedVersionNumber: tfconv.Int64(p.PublishedVersionNumber),
			ProductId:              tfconv.String(p.ProductId),
			ProductName:            tfconv.String(p.ProductName),
			CreatedAt:              tfconv.String(p.CreatedAt),
			UpdatedAt:              tfconv.String(p.UpdatedAt),
			LatestRunId:            tfconv.String(p.LatestRunId),
			LatestRunStatus:        tfconv.String(p.LatestRunStatus),
			LatestRunMessage:       tfconv.String(p.LatestRunMessage),
			LatestRunCreatedAt:     tfconv.String(p.LatestRunCreatedAt),
			LatestRunStartedAt:     tfconv.String(p.LatestRunStartedAt),
			LatestRunCompletedAt:   tfconv.String(p.LatestRunCompletedAt),
			Schedule:               tfconv.String(p.Schedule),
			Sensors:                tfconv.String(p.Sensors),
			TriggerEvents:          tfconv.String(p.TriggerEvents),
			Webhook:                tfconv.String(p.Webhook),
		}

		if p.Inputs != nil {
			entry.Inputs = make(map[string]pipelineInput, len(p.Inputs))
			for name, in := range p.Inputs {
				entry.Inputs[name] = pipelineInput{
					Type:     types.StringValue(in.Type),
					Default:  tfconv.JSON(in.Default),
					Optional: tfconv.Bool(in.Optional),
				}
			}
		}

		model.Pipelines = append(model.Pipelines, entry)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
