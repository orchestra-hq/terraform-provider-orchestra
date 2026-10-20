package monitor

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	client "github.com/orchestra-hq/terraform-provider-orchestra/internal/orchestra"
)

type MonitorResource struct {
	client *client.Client
}

var (
	_ resource.Resource                   = &MonitorResource{}
	_ resource.ResourceWithImportState    = &MonitorResource{}
	_ resource.ResourceWithValidateConfig = &MonitorResource{}
)

func (r *MonitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (r *MonitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = MonitorResourceSchema()
}

func (r *MonitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		// Configure runs before the provider is configured during
		// `terraform validate`, and again after.
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}
	r.client = c
}

// ValidateConfig enforces the one rule the schema cannot: `match` must name at
// least one dimension. A monitor matching nothing would claim every event on
// the account, which the API rejects and which is never what an author means.
func (r *MonitorResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	// Decode only what each check reads: an unknown value anywhere else fails a whole-model decode.
	var groupBy []types.String
	var groupByLabels []groupByLabelTF
	diags := req.Config.GetAttribute(ctx, path.Root("group_by"), &groupBy)
	diags.Append(req.Config.GetAttribute(ctx, path.Root("group_by_labels"), &groupByLabels)...)
	if !diags.HasError() {
		// The spec puts minItems 1 / maxItems 32 on the single groupBy list; here
		// it is two attributes, so the bound is on their total.
		total := len(groupBy) + len(groupByLabels)
		if total == 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root("group_by"),
				"Nothing to group by",
				"Set `group_by`, `group_by_labels`, or both. A monitor has to say what makes two claimed events the same incident.",
			)
		}
		if total > 32 {
			resp.Diagnostics.AddAttributeError(
				path.Root("group_by"),
				"Too many group-by entries",
				fmt.Sprintf("`group_by` and `group_by_labels` hold %d entries between them; the API allows at most 32.", total),
			)
		}
	}

	var m *matchTF
	if req.Config.GetAttribute(ctx, path.Root("match"), &m).HasError() || m == nil {
		return
	}
	if len(m.Pipelines) == 0 && len(m.Assets) == 0 && len(m.Connections) == 0 &&
		len(m.Integrations) == 0 && len(m.Labels) == 0 {
		resp.Diagnostics.AddAttributeError(
			path.Root("match"),
			"Monitor matches everything",
			"Set at least one of `pipelines`, `assets`, `connections`, `integrations` or `labels`. "+
				"A monitor that names no dimension claims every event on the account.",
		)
	}
}

func (r *MonitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var plan monitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := toWire(plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var api wireMonitorResponse
	if err := r.client.DoJSON(ctx, http.MethodPost, "/monitors", nil, body, &api); err != nil {
		resp.Diagnostics.AddError("Cannot create monitor", err.Error())
		return
	}

	apply(&plan, api)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var api wireMonitorResponse
	err := r.client.DoJSON(ctx, http.MethodGet, "/monitors/"+state.Id.ValueString(), nil, nil, &api)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Cannot read monitor", err.Error())
		return
	}

	apply(&state, api)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *MonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var plan, state monitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Id = state.Id

	// PUT replaces the whole definition: a field left out takes its default
	// rather than keeping its stored value. Terraform always sends the full
	// desired state, so that is exactly the semantics we want — except `order`,
	// which the API preserves on its own.
	body := toWire(plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var api wireMonitorResponse
	if err := r.client.DoJSON(ctx, http.MethodPut, "/monitors/"+state.Id.ValueString(), nil, body, &api); err != nil {
		resp.Diagnostics.AddError("Cannot update monitor", err.Error())
		return
	}

	apply(&plan, api)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MonitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	var state monitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DoJSON(ctx, http.MethodDelete, "/monitors/"+state.Id.ValueString(), nil, nil, nil)
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot delete monitor", err.Error())
	}
}

func (r *MonitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Missing monitor ID",
			fmt.Sprintf("Import a monitor by its ID: terraform import %s <monitor-id>", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
