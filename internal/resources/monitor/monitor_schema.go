package monitor

import (
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/alerts"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/enums"
)

func MonitorResourceSchema() schema.Schema {
	return schema.Schema{
		Description: "A rule for which failures to group into one incident, and how that incident alerts.",
		MarkdownDescription: "A rule for which failures to group into one incident, and how that incident alerts.\n\n" +
			"`match` picks the events the monitor claims, `group_by` and `group_by_labels` decide what makes two " +
			"of them the same incident, and `action` says what happens when one opens. Where two monitors claim " +
			"the same event, the one Orchestra evaluates first wins — see `order`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The monitor ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Monitor name, shown on the incidents it opens. 1 to 100 characters.",
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 100)},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "What this monitor is for.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the monitor claims events. A disabled monitor is skipped entirely.",
			},
			"order": schema.Int64Attribute{
				Computed: true,
				// Evaluation order is a property of the whole workspace, set by
				// POST /monitors/reorder across every monitor at once. A
				// per-resource attribute cannot express that without each
				// monitor fighting its neighbours, so it is reported here and
				// set by orchestra_monitor_order. Updates never send it; the
				// API keeps it across a PUT, and deleting another monitor does
				// not renumber the rest, so an update plans the stored value.
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Description:   "Evaluation position within the workspace; lower wins when two monitors claim the same event. Set it with orchestra_monitor_order.",
				MarkdownDescription: "Evaluation position within the workspace: where two monitors claim the same event, the lower `order` wins.\n\n" +
					"Ordering is a workspace-wide operation, so it is not settable here. Set it with `orchestra_monitor_order`. A new monitor is placed last.",
			},
			"match":           matchAttribute(),
			"group_by":        groupByAttribute(),
			"group_by_labels": groupByLabelsAttribute(),
			"trigger":         triggerAttribute(),
			"action":          actionAttribute(),

			// Server-owned.
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "When the monitor was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_by": schema.StringAttribute{
				Computed:      true,
				Description:   "User who created the monitor.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the monitor was last updated.",
			},
			"updated_by": schema.StringAttribute{
				Computed:    true,
				Description: "User who last updated the monitor.",
			},
		},
	}
}

func matchAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		Required:    true,
		Description: "Which events the monitor claims: AND across dimensions, OR within a list.",
		MarkdownDescription: "Which events the monitor claims: AND across dimensions, OR within a list — " +
			"`pipelines` plus `integrations` means events from those pipelines *and* those integrations.\n\n" +
			"At least one dimension is required. A monitor that named none would claim every event on the account.",
		Attributes: map[string]schema.Attribute{
			"pipelines": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				// The API returns every match dimension as a list even when it
				// was not sent, so an Optional-only attribute would plan null
				// and apply [] — which aborts the apply.
				Default:     listdefault.StaticValue(emptyStrings()),
				Description: "Pipeline IDs to claim events from.",
			},
			"assets": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				// The API returns every match dimension as a list even when it
				// was not sent, so an Optional-only attribute would plan null
				// and apply [] — which aborts the apply.
				Default:     listdefault.StaticValue(emptyStrings()),
				Description: "Asset IDs to claim events from.",
			},
			"connections": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				// The API returns every match dimension as a list even when it
				// was not sent, so an Optional-only attribute would plan null
				// and apply [] — which aborts the apply.
				Default:     listdefault.StaticValue(emptyStrings()),
				Description: "Connection IDs to claim events from.",
			},
			"integrations": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				// See the note on `pipelines`.
				Default:     listdefault.StaticValue(emptyStrings()),
				Description: "Integrations to claim events from.",
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf(enums.Integrations...)),
				},
			},
			"labels": schema.MapNestedAttribute{
				Optional:     true,
				NestedObject: labelMatchSchema(),
				Description:  "Label matches keyed by label name. Max 32 labels.",
				MarkdownDescription: "Label matches keyed by label name (max 32). Each entry is a JSON path into the " +
					"event and the values that count as a match.",
				Validators: []validator.Map{
					mapvalidator.SizeAtMost(32),
					mapvalidator.KeysAre(stringvalidator.RegexMatches(labelKeyPattern,
						"must be 1-64 characters of letters, numbers, or _.:/-")),
				},
			},
			"operation_failures_only": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "Claim only failures with an operation behind them, such as a failing dbt model or test, " +
					"not task-level failures such as a dbt parsing error. Narrows the other dimensions; it does not count as one.",
			},
			"warnings_enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Description: "Whether the monitor also claims warnings, not only failures. The API's spec does not say what it does when unset, " +
					"so unset leaves it to Orchestra and the value it chose is kept in state.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func labelMatchSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"path": schema.StringAttribute{
				Required:    true,
				Description: "JSON path into the event.",
			},
			"values": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Values that count as a match. At least one.",
				Validators:  []validator.List{listvalidator.SizeAtLeast(1)},
			},
		},
	}
}

// groupByAttribute and groupByLabelsAttribute are the two halves of the spec's
// anyOf[MonitorGroupByEnum, GroupByLabel] list. Terraform has no union type, so
// the forms are separate attributes, merged onto the wire and split on read.
// ValidateConfig enforces the combined bounds the spec puts on the single list.
func groupByAttribute() schema.Attribute {
	return schema.ListAttribute{
		Optional:    true,
		ElementType: types.StringType,
		Description: "Built-in dimensions that make two claimed events the same incident.",
		MarkdownDescription: "Built-in dimensions that make two claimed events the same incident — grouping by " +
			"`pipeline` opens one incident per pipeline, by `monitor` one for the monitor as a whole.\n\n" +
			"Set this, `group_by_labels`, or both; together they must name at least one thing and at most 32.",
		Validators: []validator.List{
			listvalidator.ValueStringsAre(stringvalidator.OneOf(enums.MonitorGroupBys...)),
		},
	}
}

func groupByLabelsAttribute() schema.Attribute {
	return schema.ListNestedAttribute{
		Optional: true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"label": schema.StringAttribute{
					Required:    true,
					Description: "Name the incident and its alerts use for the value found at `path`.",
					Validators: []validator.String{
						stringvalidator.RegexMatches(labelKeyPattern,
							"must be 1-64 characters of letters, numbers, or _.:/-"),
					},
				},
				"path": schema.StringAttribute{
					Required:    true,
					Description: "JSON path to the value in the event body to group on.",
				},
			},
		},
		Description: "Places in the event body to group on, each named by a label.",
		MarkdownDescription: "Places in the event body to group on. Two events whose value at `path` differs " +
			"become different incidents, and `label` is what the incident name and its alerts call that value.",
	}
}

func triggerAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		Optional:    true,
		Description: "Open an incident only after this many events in a window.",
		MarkdownDescription: "Open an incident only after this many events in a window. Supplying `trigger` is " +
			"what makes a monitor a threshold monitor; without it the first claimed event opens an incident.",
		Attributes: map[string]schema.Attribute{
			"count": schema.Int64Attribute{
				Required:    true,
				Description: "How many events before the incident opens. Must be more than 1.",
				Validators:  []validator.Int64{int64validator.AtLeast(2)},
			},
			"within": schema.StringAttribute{
				Required:            true,
				Description:         "The window, as an ISO 8601 duration such as PT30M or PT2H.",
				MarkdownDescription: "The window, as an ISO 8601 duration — `PT30M`, `PT2H`, `P1D`. Must be longer than zero.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(durationPattern, "must be an ISO 8601 duration such as PT30M, PT2H or P1D"),
				},
			},
		},
	}
}

// alertsDefault is the `action.alerts` the API returns when none was sent.
func alertsDefault() types.Object {
	return types.ObjectValueMust(alertsObjectType().AttrTypes, map[string]attr.Value{
		"destinations":                       types.ListNull(alerts.DestinationSchema().Type()),
		"also_send_to_pipeline_destinations": types.BoolValue(false),
	})
}

func actionAttribute() schema.Attribute {
	a := schema.SingleNestedAttribute{
		Optional:    true,
		Computed:    true,
		Description: "What happens when an incident opens.",
		Attributes: map[string]schema.Attribute{
			"mute": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Open the incident without alerting.",
			},
			"severity": schema.StringAttribute{
				Optional:    true,
				Description: "Severity to open the incident at.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.IncidentSeverities...)},
			},
			"assignee": schema.StringAttribute{
				Optional:    true,
				Description: "User ID to assign the incident to. Can be a Slack-only user, who is notified in Slack but cannot open the incident in Orchestra.",
			},
			"subscribers": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				// toWire sends [] for none, and the API echoes it.
				Default:     listdefault.StaticValue(emptyStrings()),
				Description: "User IDs mentioned in the incident's Slack alerts, at most 20. Like assignee, they can be Slack-only users.",
				Validators:  []validator.List{listvalidator.SizeAtMost(20)},
			},
			"alerts": schema.SingleNestedAttribute{
				Optional: true,
				Computed: true,
				// The API materialises `alerts` on every response, so without a
				// matching default an `action` block that omits it plans null
				// and applies an object.
				Default:     objectdefault.StaticValue(alertsDefault()),
				Description: "Where to send the alert.",
				Attributes: map[string]schema.Attribute{
					"destinations": schema.ListNestedAttribute{
						Optional:     true,
						NestedObject: alerts.DestinationSchema(),
						Description:  "Destinations to alert.",
					},
					"also_send_to_pipeline_destinations": schema.BoolAttribute{
						Optional:    true,
						Computed:    true,
						Default:     booldefault.StaticBool(false),
						Description: "Also send to the destinations configured on the pipeline that produced the event.",
					},
				},
			},
		},
	}
	// The API materialises `action` on every response, even for a monitor
	// created without one, so a monitor that omits it would plan null and apply
	// an object.
	a.Default = objectdefault.StaticValue(types.ObjectValueMust(
		a.GetType().(types.ObjectType).AttrTypes,
		map[string]attr.Value{
			"mute": types.BoolValue(false), "severity": types.StringNull(), "assignee": types.StringNull(),
			"subscribers": emptyStrings(), "alerts": alertsDefault(),
		},
	))
	return a
}

// emptyStrings is the [] the API returns for a list attribute it was not sent.
func emptyStrings() types.List {
	return types.ListValueMust(types.StringType, []attr.Value{})
}

type monitorModel struct {
	Id            types.String     `tfsdk:"id"`
	Name          types.String     `tfsdk:"name"`
	Description   types.String     `tfsdk:"description"`
	Enabled       types.Bool       `tfsdk:"enabled"`
	Order         types.Int64      `tfsdk:"order"`
	Match         *matchTF         `tfsdk:"match"`
	GroupBy       []types.String   `tfsdk:"group_by"`
	GroupByLabels []groupByLabelTF `tfsdk:"group_by_labels"`
	Trigger       *triggerTF       `tfsdk:"trigger"`
	Action        *actionTF        `tfsdk:"action"`

	CreatedAt types.String `tfsdk:"created_at"`
	CreatedBy types.String `tfsdk:"created_by"`
	UpdatedAt types.String `tfsdk:"updated_at"`
	UpdatedBy types.String `tfsdk:"updated_by"`
}

type matchTF struct {
	Pipelines    []types.String          `tfsdk:"pipelines"`
	Assets       []types.String          `tfsdk:"assets"`
	Connections  []types.String          `tfsdk:"connections"`
	Integrations []types.String          `tfsdk:"integrations"`
	Labels       map[string]labelMatchTF `tfsdk:"labels"`

	OperationFailuresOnly types.Bool `tfsdk:"operation_failures_only"`
	WarningsEnabled       types.Bool `tfsdk:"warnings_enabled"`
}

type groupByLabelTF struct {
	Label types.String `tfsdk:"label"`
	Path  types.String `tfsdk:"path"`
}

type labelMatchTF struct {
	Path   types.String   `tfsdk:"path"`
	Values []types.String `tfsdk:"values"`
}

type triggerTF struct {
	Count  types.Int64  `tfsdk:"count"`
	Within types.String `tfsdk:"within"`
}

type actionTF struct {
	Mute        types.Bool     `tfsdk:"mute"`
	Severity    types.String   `tfsdk:"severity"`
	Assignee    types.String   `tfsdk:"assignee"`
	Subscribers []types.String `tfsdk:"subscribers"`
	Alerts      *alertsTF      `tfsdk:"alerts"`
}

type alertsTF struct {
	Destinations                   []alerts.DestinationTF `tfsdk:"destinations"`
	AlsoSendToPipelineDestinations types.Bool             `tfsdk:"also_send_to_pipeline_destinations"`
}

// Patterns from the spec: LabelKey's own pattern, and an ISO 8601 duration for
// Duration (format: duration, gt: PT0S).
var (
	labelKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_.:/-]{1,64}$`)
	durationPattern = regexp.MustCompile(`^P(?:\d+Y)?(?:\d+M)?(?:\d+W)?(?:\d+D)?(?:T(?:\d+H)?(?:\d+M)?(?:\d+(?:\.\d+)?S)?)?$`)
)

// alertsObjectType spells out the attribute types the alerts default needs,
// derived from the schema above so the two cannot drift apart.
func alertsObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"destinations":                       types.ListType{ElemType: alerts.DestinationSchema().Type()},
		"also_send_to_pipeline_destinations": types.BoolType,
	}}
}
