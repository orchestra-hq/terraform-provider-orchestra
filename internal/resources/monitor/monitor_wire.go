package monitor

import "github.com/orchestra-hq/terraform-provider-orchestra/internal/alerts"

// Wire types mirror components.schemas.MonitorDefinition and MonitorResponse.
// Keep them in sync with openapi.yaml.

type wireMonitorResponse struct {
	MonitorID  string         `json:"monitorId"`
	Definition wireDefinition `json:"definition"`
	CreatedAt  string         `json:"createdAt"`
	CreatedBy  string         `json:"createdBy"`
	UpdatedAt  string         `json:"updatedAt"`
	UpdatedBy  string         `json:"updatedBy"`
}

type wireDefinition struct {
	Name        string       `json:"name"`
	Description *string      `json:"description,omitempty"`
	Enabled     bool         `json:"enabled"`
	Order       *int64       `json:"order,omitempty"`
	Match       wireMatch    `json:"match"`
	GroupBy     []string     `json:"groupBy"`
	Trigger     *wireTrigger `json:"trigger,omitempty"`
	Action      *wireAction  `json:"action,omitempty"`
}

type wireMatch struct {
	Pipelines    []string               `json:"pipelines,omitempty"`
	Assets       []string               `json:"assets,omitempty"`
	Connections  []string               `json:"connections,omitempty"`
	Integrations []string               `json:"integrations,omitempty"`
	Labels       map[string][]wireLabel `json:"labels,omitempty"`
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
	Mute     bool        `json:"mute"`
	Severity *string     `json:"severity,omitempty"`
	Assignee *string     `json:"assignee,omitempty"`
	Alerts   *wireAlerts `json:"alerts,omitempty"`
}

type wireAlerts struct {
	Destinations                   []alerts.WireDestination `json:"destinations,omitempty"`
	AlsoSendToPipelineDestinations bool                     `json:"alsoSendToPipelineDestinations"`
}
