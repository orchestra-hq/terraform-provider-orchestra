package monitor

import (
	"encoding/json"
	"fmt"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/alerts"
)

// Wire types mirror components.schemas.MonitorDefinition and MonitorResponse.
// Keep them in sync with openapi.yaml.

// WireSchemas pairs the spec schemas this package sends with the types that
// encode them, for internal/apicontract: a schema property that none of their
// JSON tags names fails the build. Nested schemas are paired by following the
// fields, so only roots and types the fields cannot lead to are listed.
// GroupByLabel is the second kind: wireGroupBy marshals it by hand.
var WireSchemas = map[string]any{
	"MonitorDefinition": wireDefinition{},
	"GroupByLabel":      wireGroupByLabel{},
}

type wireMonitorResponse struct {
	MonitorID  string         `json:"monitorId"`
	Definition wireDefinition `json:"definition"`
	CreatedAt  string         `json:"createdAt"`
	CreatedBy  string         `json:"createdBy"`
	UpdatedAt  string         `json:"updatedAt"`
	UpdatedBy  string         `json:"updatedBy"`
}

type wireDefinition struct {
	Name        string        `json:"name"`
	Description *string       `json:"description,omitempty"`
	Enabled     bool          `json:"enabled"`
	Order       *int64        `json:"order,omitempty"`
	Match       wireMatch     `json:"match"`
	GroupBy     []wireGroupBy `json:"groupBy"`
	Trigger     *wireTrigger  `json:"trigger,omitempty"`
	Action      *wireAction   `json:"action,omitempty"`
}

type wireMatch struct {
	Pipelines    []string               `json:"pipelines,omitempty"`
	Assets       []string               `json:"assets,omitempty"`
	Connections  []string               `json:"connections,omitempty"`
	Integrations []string               `json:"integrations,omitempty"`
	Labels       map[string][]wireLabel `json:"labels,omitempty"`

	OperationFailuresOnly bool `json:"operationFailuresOnly"`
}

type wireLabel struct {
	Path   string   `json:"path"`
	Values []string `json:"values"`
}

type wireTrigger struct {
	Count  int64  `json:"count"`
	Within string `json:"within"`
}

type wireAction struct {
	Mute     bool    `json:"mute"`
	Severity *string `json:"severity,omitempty"`
	Assignee *string `json:"assignee,omitempty"`
	// Always sent: a PUT resets an omitted field to its default.
	Subscribers []string    `json:"subscribers"`
	Alerts      *wireAlerts `json:"alerts,omitempty"`
}

type wireAlerts struct {
	Destinations                   []alerts.WireDestination `json:"destinations,omitempty"`
	AlsoSendToPipelineDestinations bool                     `json:"alsoSendToPipelineDestinations"`
}

// wireGroupBy is one entry of MonitorDefinition.groupBy, which the spec
// declares as anyOf[MonitorGroupByEnum, GroupByLabel]: either a bare dimension
// name or a {label, path} object naming a place in the event body. Go has no
// union either, so the two forms are carried side by side and discriminated on
// the wire.
type wireGroupBy struct {
	Name  string
	Label *wireGroupByLabel
}

type wireGroupByLabel struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

func (g *wireGroupBy) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err == nil {
		g.Name, g.Label = name, nil
		return nil
	}
	var label wireGroupByLabel
	if err := json.Unmarshal(b, &label); err != nil {
		return fmt.Errorf("groupBy entry is neither a dimension name nor a {label, path} object: %w", err)
	}
	g.Name, g.Label = "", &label
	return nil
}

func (g wireGroupBy) MarshalJSON() ([]byte, error) {
	if g.Label != nil {
		return json.Marshal(g.Label)
	}
	return json.Marshal(g.Name)
}
