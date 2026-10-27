package agents

import (
	"context"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/client"
)

// SkillsDataSource lists skills, chiefly to find the IDs of the ones Orchestra
// publishes, which an agent can use but Terraform cannot create.
type SkillsDataSource struct {
	client *client.Client
}

var _ datasource.DataSourceWithConfigure = &SkillsDataSource{}

type skillsModel struct {
	Search           types.String   `tfsdk:"search"`
	OrchestraManaged types.Bool     `tfsdk:"orchestra_managed"`
	Skills           []skillSummary `tfsdk:"skills"`
}

type skillSummary struct {
	Id               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	OrchestraManaged types.Bool   `tfsdk:"orchestra_managed"`
}

func (d *SkillsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_skills"
}

func (d *SkillsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}
	d.client = c.Agents()
}

func (d *SkillsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "Agent skills in the workspace, including the ones Orchestra publishes.",
		MarkdownDescription: "Agent skills in the workspace, including the ones Orchestra publishes, which an `orchestra_agent` can use by ID.",
		Attributes: map[string]schema.Attribute{
			"search": schema.StringAttribute{
				Optional:    true,
				Description: "Only skills whose name or description contains this.",
			},
			"orchestra_managed": schema.BoolAttribute{
				Optional:    true,
				Description: "true for only the skills Orchestra publishes, false for only the workspace's own. Unset returns both.",
			},
			"skills": schema.ListNestedAttribute{
				Computed:    true,
				Description: "The matching skills.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                schema.StringAttribute{Computed: true, Description: "The skill ID."},
						"name":              schema.StringAttribute{Computed: true, Description: "The skill name."},
						"description":       schema.StringAttribute{Computed: true, Description: "When an agent should use it."},
						"orchestra_managed": schema.BoolAttribute{Computed: true, Description: "Whether Orchestra publishes it."},
					},
				},
			},
		},
	}
}

func (d *SkillsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}
	var model skillsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	q := url.Values{}
	if s := model.Search.ValueString(); s != "" {
		q.Set("search", s)
	}
	if !model.OrchestraManaged.IsNull() {
		q.Set("orchestra_managed", strconv.FormatBool(model.OrchestraManaged.ValueBool()))
	}

	var api []struct {
		SkillID          string `json:"skillId"`
		Name             string `json:"name"`
		Description      string `json:"description"`
		OrchestraManaged bool   `json:"orchestraManaged"`
	}
	if err := d.client.ListAll(ctx, "/skills", q, &api); err != nil {
		resp.Diagnostics.AddError("Cannot list skills", err.Error())
		return
	}

	model.Skills = make([]skillSummary, 0, len(api))
	for _, s := range api {
		model.Skills = append(model.Skills, skillSummary{
			Id:               types.StringValue(s.SkillID),
			Name:             types.StringValue(s.Name),
			Description:      types.StringValue(s.Description),
			OrchestraManaged: types.BoolValue(s.OrchestraManaged),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
