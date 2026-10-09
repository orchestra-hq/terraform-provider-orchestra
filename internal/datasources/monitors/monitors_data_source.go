package monitors

import (
	"context"
	"net/http"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	client "github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

// MonitorsDataSource lists the workspace's monitors in evaluation order. The
// full definition of one is the orchestra_monitor resource's business; this
// is for finding IDs, to import a monitor or to order them.
type MonitorsDataSource struct {
	client *client.Client
}

var (
	_ datasource.DataSource              = &MonitorsDataSource{}
	_ datasource.DataSourceWithConfigure = &MonitorsDataSource{}
)

func (d *MonitorsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitors"
}

func (d *MonitorsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *MonitorsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The workspace's monitors, in evaluation order.",
		MarkdownDescription: "The workspace's monitors, in evaluation order. Where two monitors claim the same event, " +
			"the earlier one wins. Use it to find a monitor's ID to import it, or to build an `orchestra_monitor_order`.",
		Attributes: map[string]schema.Attribute{
			"monitors": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Every monitor, earliest first.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true, Description: "The monitor ID."},
						"name":        schema.StringAttribute{Computed: true, Description: "The monitor name."},
						"description": schema.StringAttribute{Computed: true, Description: "The monitor description."},
						"enabled":     schema.BoolAttribute{Computed: true, Description: "Whether the monitor claims events."},
						"order":       schema.Int64Attribute{Computed: true, Description: "Position in the evaluation order."},
					},
				},
			},
		},
	}
}

type monitorsModel struct {
	Monitors []monitorSummary `tfsdk:"monitors"`
}

type monitorSummary struct {
	Id          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	Order       types.Int64  `tfsdk:"order"`
}

// wireMonitor is the part of components.schemas.MonitorResponse read here.
type wireMonitor struct {
	MonitorID  string `json:"monitorId"`
	Definition struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Enabled     bool    `json:"enabled"`
		Order       *int64  `json:"order"`
	} `json:"definition"`
}

func (d *MonitorsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var api []wireMonitor
	if err := d.client.DoJSON(ctx, http.MethodGet, "/monitors", nil, nil, &api); err != nil {
		resp.Diagnostics.AddError("Cannot list monitors", err.Error())
		return
	}
	// The API lists them in evaluation order; sort anyway, so the list's
	// position always matches `order`.
	sort.SliceStable(api, func(i, j int) bool {
		a, b := api[i].Definition.Order, api[j].Definition.Order
		return a != nil && (b == nil || *a < *b)
	})

	model := monitorsModel{Monitors: make([]monitorSummary, 0, len(api))}
	for _, m := range api {
		model.Monitors = append(model.Monitors, monitorSummary{
			Id:          types.StringValue(m.MonitorID),
			Name:        types.StringValue(m.Definition.Name),
			Description: tfconv.String(m.Definition.Description),
			Enabled:     types.BoolValue(m.Definition.Enabled),
			Order:       tfconv.Int64(m.Definition.Order),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
