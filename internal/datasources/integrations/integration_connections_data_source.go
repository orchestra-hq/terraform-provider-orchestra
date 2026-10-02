// Package integrations exposes Orchestra's integration catalogue: which
// integrations exist, which jobs each supports, which sensor checks exist, and
// which connections the workspace has configured. These resolve the values that
// pipeline tasks reference.
package integrations

import (
	"context"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/enums"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

func configure(providerData any, target **client.Client) (bool, string) {
	if providerData == nil {
		return false, ""
	}
	c, ok := providerData.(*client.Client)
	if !ok {
		return false, "ProviderData is not an Orchestra API client"
	}
	*target = c
	return true, ""
}

func computedString(desc string) schema.Attribute {
	return schema.StringAttribute{Computed: true, Description: desc}
}

// ---------- orchestra_integrations ----------

type IntegrationConnectionsDataSource struct{ client *client.Client }

var (
	_ datasource.DataSource              = &IntegrationConnectionsDataSource{}
	_ datasource.DataSourceWithConfigure = &IntegrationConnectionsDataSource{}
)

func (d *IntegrationConnectionsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration_connections"
	_ = ctx
}

func (d *IntegrationConnectionsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	_ = ctx
	if ok, msg := configure(req.ProviderData, &d.client); !ok && msg != "" {
		resp.Diagnostics.AddError("Invalid ProviderData", msg)
	}
}

func (d *IntegrationConnectionsDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	_ = ctx
	resp.Schema = schema.Schema{
		Description:         "Integration connections configured in the workspace.",
		MarkdownDescription: "Integration connections configured in the workspace. Use this to resolve the connection IDs that pipeline tasks reference through `connection`.",
		Attributes: map[string]schema.Attribute{
			"integration": schema.StringAttribute{
				Optional:    true,
				Description: "Only return connections for this integration.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.Integrations...)},
			},
			"auth_status": schema.StringAttribute{
				Optional:    true,
				Description: "Only return connections in this authentication state.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.AuthStatuses...)},
			},
			"connections": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Matching connections.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"connection_id":   computedString("Connection ID, as referenced by a task's connection field."),
						"connection_name": computedString("Human-readable connection name."),
						"integration":     computedString("Integration the connection is for."),
						"auth_status":     computedString("Authentication state."),
						"is_default": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether this is the default connection for the integration.",
						},
						"created_at": computedString("When the connection was created."),
						"updated_at": computedString("When the connection was last updated."),
					},
				},
			},
		},
	}
}

type integrationConnectionsModel struct {
	Integration types.String   `tfsdk:"integration"`
	AuthStatus  types.String   `tfsdk:"auth_status"`
	Connections []connectionTF `tfsdk:"connections"`
}

type connectionTF struct {
	ConnectionId   types.String `tfsdk:"connection_id"`
	ConnectionName types.String `tfsdk:"connection_name"`
	Integration    types.String `tfsdk:"integration"`
	AuthStatus     types.String `tfsdk:"auth_status"`
	IsDefault      types.Bool   `tfsdk:"is_default"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

type connectionResponse struct {
	ConnectionId   *string `json:"connectionId"`
	ConnectionName *string `json:"connectionName"`
	Integration    *string `json:"integration"`
	AuthStatus     *string `json:"authStatus"`
	IsDefault      *bool   `json:"isDefault"`
	CreatedAt      *string `json:"createdAt"`
	UpdatedAt      *string `json:"updatedAt"`
}

func (d *IntegrationConnectionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var model integrationConnectionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	query := url.Values{}
	if v := tfconv.GoString(model.Integration); v != "" {
		query.Set("integration", v)
	}
	if v := tfconv.GoString(model.AuthStatus); v != "" {
		query.Set("authStatus", v)
	}

	var api []connectionResponse
	if err := d.client.DoJSON(ctx, http.MethodGet, "/integrations/connections", query, nil, &api); err != nil {
		resp.Diagnostics.AddError("Cannot list integration connections", err.Error())
		return
	}

	model.Connections = make([]connectionTF, 0, len(api))
	for _, c := range api {
		model.Connections = append(model.Connections, connectionTF{
			ConnectionId:   tfconv.String(c.ConnectionId),
			ConnectionName: tfconv.String(c.ConnectionName),
			Integration:    tfconv.String(c.Integration),
			AuthStatus:     tfconv.String(c.AuthStatus),
			IsDefault:      tfconv.Bool(c.IsDefault),
			CreatedAt:      tfconv.String(c.CreatedAt),
			UpdatedAt:      tfconv.String(c.UpdatedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// ---------- orchestra_sensor_checks ----------
