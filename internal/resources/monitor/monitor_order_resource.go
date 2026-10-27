package monitor

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/client"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

// orderID is the only ID this resource has: a workspace has one monitor order.
const orderID = "monitor_order"

// MonitorOrderResource owns which monitors are evaluated first.
//
// POST /monitors/reorder must name every monitor on the workspace exactly once.
// Requiring the configuration to do the same would break the ordinary way a
// monitor is deleted: Terraform updates this resource to drop it before the
// monitor itself is destroyed, so the request would omit a monitor that still
// exists. Instead `monitor_ids` is the head of the order, and every monitor it
// does not name follows, in the order it already had.
type MonitorOrderResource struct {
	client *client.Client
}

var (
	_ resource.Resource                = &MonitorOrderResource{}
	_ resource.ResourceWithImportState = &MonitorOrderResource{}
)

type orderModel struct {
	Id         types.String   `tfsdk:"id"`
	MonitorIds []types.String `tfsdk:"monitor_ids"`
}

func (r *MonitorOrderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor_order"
}

func (r *MonitorOrderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The order monitors are evaluated in. Where two monitors claim the same event, the earlier one wins.",
		MarkdownDescription: "The order monitors are evaluated in. Where two monitors claim the same event, the earlier one wins.\n\n" +
			"The monitors in `monitor_ids` come first, in that order. Any other monitor follows in the order it " +
			"already had, so a monitor created outside Terraform does not need listing; a new monitor is placed " +
			"last. Declare this resource once per workspace. Destroying it leaves the order as it is.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Always monitor_order: a workspace has one.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"monitor_ids": schema.ListAttribute{
				ElementType:         types.StringType,
				Required:            true,
				Description:         "Monitor IDs to evaluate first, in order.",
				MarkdownDescription: "Monitor IDs to evaluate first, in order. Usually `orchestra_monitor.<name>.id`.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.UniqueValues(),
				},
			},
		},
	}
}

func (r *MonitorOrderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid ProviderData", "ProviderData is not an Orchestra API client")
		return
	}
	r.client = c
}

func (r *MonitorOrderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.reorder(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MonitorOrderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.reorder(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MonitorOrderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}
	var state orderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var current []wireMonitorResponse
	if err := r.client.DoJSON(ctx, http.MethodGet, "/monitors", nil, nil, &current); err != nil {
		resp.Diagnostics.AddError("Cannot list monitors", err.Error())
		return
	}

	state.Id = types.StringValue(orderID)
	state.MonitorIds = tfconv.Strings(head(evaluationOrder(current), tfconv.GoStrings(state.MonitorIds)))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete leaves the order as it is: there is no order to remove, only one to
// stop managing.
func (r *MonitorOrderResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *MonitorOrderResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), orderID)...)
}

// reorder puts plan.MonitorIds first and every other monitor after them, then
// records the order the API reports back.
func (r *MonitorOrderResource) reorder(ctx context.Context, plan *orderModel, diags *diag.Diagnostics) {
	if r.client == nil {
		diags.AddError("Missing client", "The Orchestra API client was not configured")
		return
	}

	first := tfconv.GoStrings(plan.MonitorIds)
	listed := map[string]bool{}
	for _, id := range first {
		listed[id] = true
	}

	// A monitor created or deleted between the read and the reorder makes the
	// API reject it with a 400. That happens within a single apply: Terraform
	// creates monitors the order does not depend on in parallel with it. So
	// read again and retry; a 400 that persists is a real error, such as a
	// listed monitor that does not exist.
	var reordered []wireMonitorResponse
	for attempt := 1; ; attempt++ {
		var current []wireMonitorResponse
		if err := r.client.DoJSON(ctx, http.MethodGet, "/monitors", nil, nil, &current); err != nil {
			diags.AddError("Cannot list monitors", err.Error())
			return
		}
		ids := append([]string{}, first...)
		for _, id := range evaluationOrder(current) {
			if !listed[id] {
				ids = append(ids, id)
			}
		}

		err := r.client.DoJSON(ctx, http.MethodPost, "/monitors/reorder", nil, map[string]any{"monitorIds": ids}, &reordered)
		if err == nil {
			break
		}
		var apiErr *client.APIError
		if attempt < 3 && errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest {
			continue
		}
		diags.AddError("Cannot reorder monitors", err.Error())
		return
	}

	plan.Id = types.StringValue(orderID)
	plan.MonitorIds = tfconv.Strings(head(evaluationOrder(reordered), first))
}

// evaluationOrder returns monitor IDs sorted by `order`.
func evaluationOrder(monitors []wireMonitorResponse) []string {
	sorted := append([]wireMonitorResponse(nil), monitors...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].Definition.Order, sorted[j].Definition.Order
		return a != nil && (b == nil || *a < *b)
	})
	ids := make([]string, len(sorted))
	for i, m := range sorted {
		ids[i] = m.MonitorID
	}
	return ids
}

// head returns the shortest prefix of order that holds every ID in managed,
// which is managed itself while those monitors are first and in that order. A
// monitor moved in front of them appears in the prefix, so the plan shows the
// drift; one deleted outside Terraform drops out of it. With nothing managed
// yet, as after an import, it is the whole order.
func head(order, managed []string) []string {
	if len(managed) == 0 {
		return order
	}
	want := map[string]bool{}
	for _, id := range managed {
		want[id] = true
	}
	end := 0
	for i, id := range order {
		if want[id] {
			end = i + 1
		}
	}
	return order[:end]
}
