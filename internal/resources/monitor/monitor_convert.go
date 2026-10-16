package monitor

import (
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/alerts"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

func toWire(m monitorModel, diags *diag.Diagnostics) wireDefinition {
	out := wireDefinition{
		Name:        m.Name.ValueString(),
		Description: tfconv.StringPtrOrNil(m.Description),
		Enabled:     m.Enabled.ValueBool(),
		GroupBy:     groupByToWire(m),
	}

	if m.Match != nil {
		out.Match = wireMatch{
			Pipelines:    tfconv.GoStrings(m.Match.Pipelines),
			Assets:       tfconv.GoStrings(m.Match.Assets),
			Connections:  tfconv.GoStrings(m.Match.Connections),
			Integrations: tfconv.GoStrings(m.Match.Integrations),

			OperationFailuresOnly: m.Match.OperationFailuresOnly.ValueBool(),
		}
		if len(m.Match.Labels) > 0 {
			out.Match.Labels = make(map[string][]wireLabel, len(m.Match.Labels))
			for key, lm := range m.Match.Labels {
				out.Match.Labels[key] = []wireLabel{{
					Path:   lm.Path.ValueString(),
					Values: tfconv.GoStrings(lm.Values),
				}}
			}
		}
	}

	if m.Trigger != nil {
		out.Trigger = &wireTrigger{
			Count:  m.Trigger.Count.ValueInt64(),
			Within: m.Trigger.Within.ValueString(),
		}
	}

	if m.Action != nil {
		a := &wireAction{
			Mute:     m.Action.Mute.ValueBool(),
			Severity: tfconv.StringPtrOrNil(m.Action.Severity),
			Assignee: tfconv.StringPtrOrNil(m.Action.Assignee),
			// Non-nil, so none is sent as [] rather than null.
			Subscribers: append([]string{}, tfconv.GoStrings(m.Action.Subscribers)...),
		}
		if m.Action.Alerts != nil {
			a.Alerts = &wireAlerts{
				Destinations: alerts.ToWire(m.Action.Alerts.Destinations,
					path.Root("action").AtName("alerts").AtName("destinations"), diags),
				AlsoSendToPipelineDestinations: m.Action.Alerts.AlsoSendToPipelineDestinations.ValueBool(),
			}
		}
		out.Action = a
	}

	return out
}

// apply copies the API response into the model.
//
// The API fills in declared defaults on the way out, so an Optional attribute
// left unset in config comes back populated. `enabled`, `action.mute` and
// `also_send_to_pipeline_destinations` all carry client-side defaults matching
// the server's, so the planned value and the applied value agree.
func apply(m *monitorModel, api wireMonitorResponse) {
	d := api.Definition

	m.Id = types.StringValue(api.MonitorID)
	m.CreatedAt = types.StringValue(api.CreatedAt)
	m.CreatedBy = types.StringValue(api.CreatedBy)
	m.UpdatedAt = types.StringValue(api.UpdatedAt)
	m.UpdatedBy = types.StringValue(api.UpdatedBy)

	m.Name = types.StringValue(d.Name)
	m.Description = tfconv.String(d.Description)
	m.Enabled = types.BoolValue(d.Enabled)
	m.Order = tfconv.Int64(d.Order)
	groupByFromWire(m, d.GroupBy)

	m.Match = &matchTF{
		Pipelines:    tfconv.Strings(d.Match.Pipelines),
		Assets:       tfconv.Strings(d.Match.Assets),
		Connections:  tfconv.Strings(d.Match.Connections),
		Integrations: tfconv.Strings(d.Match.Integrations),

		OperationFailuresOnly: types.BoolValue(d.Match.OperationFailuresOnly),
	}
	if len(d.Match.Labels) > 0 {
		m.Match.Labels = make(map[string]labelMatchTF, len(d.Match.Labels))
		for key, lms := range d.Match.Labels {
			// The spec allows several matches per key, but a Terraform map of
			// objects holds one. Sending one and reading the first back keeps
			// the round trip honest for anything this provider wrote; a monitor
			// authored elsewhere with several is reported by its first.
			if len(lms) == 0 {
				continue
			}
			m.Match.Labels[key] = labelMatchTF{
				Path:   types.StringValue(lms[0].Path),
				Values: tfconv.Strings(lms[0].Values),
			}
		}
	}

	m.Trigger = nil
	if d.Trigger != nil {
		m.Trigger = &triggerTF{
			Count:  types.Int64Value(d.Trigger.Count),
			Within: types.StringValue(d.Trigger.Within),
		}
	}

	m.Action = nil
	if d.Action != nil {
		a := &actionTF{
			Mute:     types.BoolValue(d.Action.Mute),
			Severity: tfconv.String(d.Action.Severity),
			Assignee: tfconv.String(d.Action.Assignee),
			// Never null: the schema defaults it to [].
			Subscribers: tfconv.Strings(append([]string{}, d.Action.Subscribers...)),
		}
		if d.Action.Alerts != nil {
			a.Alerts = &alertsTF{
				Destinations:                   alerts.FromWire(d.Action.Alerts.Destinations),
				AlsoSendToPipelineDestinations: types.BoolValue(d.Action.Alerts.AlsoSendToPipelineDestinations),
			}
		}
		m.Action = a
	}
}

// groupByToWire merges the two Terraform attributes back into the API's single
// list. Names go first, then labels; the API treats the list as a set of
// dimensions, so the interleaving the user originally wrote is not meaningful.
func groupByToWire(m monitorModel) []wireGroupBy {
	out := make([]wireGroupBy, 0, len(m.GroupBy)+len(m.GroupByLabels))
	for _, name := range m.GroupBy {
		if name.IsNull() || name.IsUnknown() {
			continue
		}
		out = append(out, wireGroupBy{Name: name.ValueString()})
	}
	for _, l := range m.GroupByLabels {
		out = append(out, wireGroupBy{Label: &wireGroupByLabel{
			Label: l.Label.ValueString(),
			Path:  l.Path.ValueString(),
		}})
	}
	return out
}

// groupByFromWire splits the API's single list back into the two attributes.
// Each stays nil when the API sent nothing of that form, so an unset attribute
// reads back as null rather than an empty list.
func groupByFromWire(m *monitorModel, entries []wireGroupBy) {
	m.GroupBy = nil
	m.GroupByLabels = nil
	for _, e := range entries {
		if e.Label != nil {
			m.GroupByLabels = append(m.GroupByLabels, groupByLabelTF{
				Label: types.StringValue(e.Label.Label),
				Path:  types.StringValue(e.Label.Path),
			})
			continue
		}
		m.GroupBy = append(m.GroupBy, types.StringValue(e.Name))
	}
}
