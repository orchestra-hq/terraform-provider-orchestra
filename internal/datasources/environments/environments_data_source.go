package environments

import (
	"context"
	"net/http"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/client"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

// EnvironmentsDataSource lists environments. GET /environments returns
// ProtectedEnvironmentsResponse, which deliberately omits `values` — read a
// single environment to get those.
type EnvironmentsDataSource struct {
	client *client.Client
}

var (
	_ datasource.DataSource              = &EnvironmentsDataSource{}
	_ datasource.DataSourceWithConfigure = &EnvironmentsDataSource{}
)

func (d *EnvironmentsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environments"
	_ = ctx
}

func (d *EnvironmentsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *EnvironmentsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	_ = ctx
	resp.Schema = schema.Schema{
		Description: "Environments in the workspace.",
		MarkdownDescription: "Environments in the workspace. The list endpoint omits environment values; " +
			"use the `orchestra_environment` data source to read those for one environment.",
		Attributes: map[string]schema.Attribute{
			"environments": schema.ListNestedAttribute{
				Computed:    true,
				Description: "All environments in the workspace.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true, Description: "The environment ID."},
						"account_id":  schema.StringAttribute{Computed: true, Description: "Account the environment belongs to."},
						"name":        schema.StringAttribute{Computed: true, Description: "Environment name."},
						"default_env": schema.BoolAttribute{Computed: true, Description: "Whether this is the default environment."},
						"allowed_branches": schema.ListAttribute{
							Computed:    true,
							ElementType: types.StringType,
							Description: "Branches allowed to run against this environment.",
						},
						"allow_unpublished_pipelines": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether unpublished pipelines may run against this environment.",
						},
					},
				},
			},
		},
	}
}

type environmentsModel struct {
	Environments []environmentSummary `tfsdk:"environments"`
}

type environmentSummary struct {
	Id                        types.String   `tfsdk:"id"`
	AccountId                 types.String   `tfsdk:"account_id"`
	Name                      types.String   `tfsdk:"name"`
	DefaultEnv                types.Bool     `tfsdk:"default_env"`
	AllowedBranches           []types.String `tfsdk:"allowed_branches"`
	AllowUnpublishedPipelines types.Bool     `tfsdk:"allow_unpublished_pipelines"`
}

// protectedEnvironmentResponse is components.schemas.ProtectedEnvironmentsResponse.
type protectedEnvironmentResponse struct {
	AccountId                 *string  `json:"accountId"`
	EnvironmentId             *string  `json:"environmentId"`
	Name                      *string  `json:"name"`
	DefaultEnv                *bool    `json:"defaultEnv"`
	AllowedBranches           []string `json:"allowedBranches"`
	AllowUnpublishedPipelines *bool    `json:"allowUnpublishedPipelines"`
}

func (d *EnvironmentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model environmentsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var api []protectedEnvironmentResponse
	if err := d.client.DoJSON(ctx, http.MethodGet, "/environments", nil, nil, &api); err != nil {
		resp.Diagnostics.AddError("Cannot list environments", err.Error())
		return
	}

	model.Environments = make([]environmentSummary, 0, len(api))
	for _, e := range api {
		model.Environments = append(model.Environments, environmentSummary{
			Id:                        tfconv.String(e.EnvironmentId),
			AccountId:                 tfconv.String(e.AccountId),
			Name:                      tfconv.String(e.Name),
			DefaultEnv:                tfconv.Bool(e.DefaultEnv),
			AllowedBranches:           tfconv.Strings(e.AllowedBranches),
			AllowUnpublishedPipelines: tfconv.Bool(e.AllowUnpublishedPipelines),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// EnvironmentDataSource reads one environment, including its values.
type EnvironmentDataSource struct {
	client *client.Client
}

var (
	_ datasource.DataSource              = &EnvironmentDataSource{}
	_ datasource.DataSourceWithConfigure = &EnvironmentDataSource{}
)

func (d *EnvironmentDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
	_ = ctx
}

func (d *EnvironmentDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *EnvironmentDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	_ = ctx
	resp.Schema = schema.Schema{
		Description: "A single environment, including its values.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "The environment ID.",
			},
			"account_id":  schema.StringAttribute{Computed: true, Description: "Account the environment belongs to."},
			"name":        schema.StringAttribute{Computed: true, Description: "Environment name."},
			"default_env": schema.BoolAttribute{Computed: true, Description: "Whether this is the default environment."},
			"allowed_branches": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Branches allowed to run against this environment.",
			},
			"allow_unpublished_pipelines": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether unpublished pipelines may run against this environment.",
			},
			"created_at": schema.StringAttribute{Computed: true, Description: "When the environment was created."},
			"updated_at": schema.StringAttribute{Computed: true, Description: "When the environment was last updated."},
			"values": schema.MapNestedAttribute{
				Computed:    true,
				Description: "Environment values keyed by variable name.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type":  schema.StringAttribute{Computed: true, Description: "Value type."},
						"value": schema.StringAttribute{Computed: true, Description: "The value, rendered as a string."},
					},
				},
			},
		},
	}
}

type environmentModel struct {
	Id                        types.String          `tfsdk:"id"`
	AccountId                 types.String          `tfsdk:"account_id"`
	Name                      types.String          `tfsdk:"name"`
	DefaultEnv                types.Bool            `tfsdk:"default_env"`
	AllowedBranches           []types.String        `tfsdk:"allowed_branches"`
	AllowUnpublishedPipelines types.Bool            `tfsdk:"allow_unpublished_pipelines"`
	CreatedAt                 types.String          `tfsdk:"created_at"`
	UpdatedAt                 types.String          `tfsdk:"updated_at"`
	Values                    map[string]valueModel `tfsdk:"values"`
}

type valueModel struct {
	Type  types.String `tfsdk:"type"`
	Value types.String `tfsdk:"value"`
}

type environmentResponse struct {
	AccountId                 *string  `json:"accountId"`
	EnvironmentId             *string  `json:"environmentId"`
	Name                      *string  `json:"name"`
	DefaultEnv                *bool    `json:"defaultEnv"`
	AllowedBranches           []string `json:"allowedBranches"`
	AllowUnpublishedPipelines *bool    `json:"allowUnpublishedPipelines"`
	CreatedAt                 *string  `json:"createdAt"`
	UpdatedAt                 *string  `json:"updatedAt"`
	Values                    map[string]struct {
		Type  string `json:"type"`
		Value any    `json:"value"`
	} `json:"values"`
}

func (d *EnvironmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model environmentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := tfconv.GoString(model.Id)
	if id == "" {
		resp.Diagnostics.AddError("Missing environment ID", "Set `id` to the environment you want to read.")
		return
	}

	var api environmentResponse
	if err := d.client.DoJSON(ctx, http.MethodGet, "/environments/"+id, nil, nil, &api); err != nil {
		resp.Diagnostics.AddError("Cannot read environment", err.Error())
		return
	}

	model.Id = tfconv.String(api.EnvironmentId)
	model.AccountId = tfconv.String(api.AccountId)
	model.Name = tfconv.String(api.Name)
	model.DefaultEnv = tfconv.Bool(api.DefaultEnv)
	model.AllowedBranches = tfconv.Strings(api.AllowedBranches)
	model.AllowUnpublishedPipelines = tfconv.Bool(api.AllowUnpublishedPipelines)
	model.CreatedAt = tfconv.String(api.CreatedAt)
	model.UpdatedAt = tfconv.String(api.UpdatedAt)

	if api.Values != nil {
		model.Values = make(map[string]valueModel, len(api.Values))
		for name, v := range api.Values {
			model.Values[name] = valueModel{
				Type:  types.StringValue(v.Type),
				Value: types.StringValue(stringifyScalar(v.Value)),
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// stringifyScalar renders the EnvironmentValueType union (string, bool,
// integer or number) as a string.
func stringifyScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}
