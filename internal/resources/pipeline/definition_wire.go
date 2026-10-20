package pipeline

import (
	"encoding/json"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/alerts"
)

// Wire types mirror the OpenAPI `PipelineModel` tree exactly (camelCase JSON,
// pointers for nullable scalars). They are the payload of the `data` field on
// POST/PUT /pipelines and the body of GET /pipelines/data.
//
// Keep these in sync with components.schemas.PipelineModel in openapi.yaml.

// WireSchemas pairs the spec schemas this package sends with the types that
// encode them, for internal/apicontract's property check. The task groups and
// tasks are listed because `pipeline` is carried as raw JSON, which the check
// cannot follow.
var WireSchemas = map[string]any{
	"PipelineModel":  wireDefinition{},
	"TaskGroupModel": wireTaskGroup{},
	"TaskModel":      wireTask{},
}

type wireDefinition struct {
	Version       string                     `json:"version"`
	Name          string                     `json:"name"`
	Pipeline      map[string]json.RawMessage `json:"pipeline"`
	Schedule      []wireSchedule             `json:"schedule,omitempty"`
	Sensors       map[string]wireSensor      `json:"sensors,omitempty"`
	TriggerEvents []wireTriggerEvent         `json:"triggerEvents,omitempty"`
	Webhook       *wireWebhook               `json:"webhook,omitempty"`
	Configuration *wireConfiguration         `json:"configuration,omitempty"`
	Inputs        map[string]wireInput       `json:"inputs,omitempty"`
	Meta          json.RawMessage            `json:"meta,omitempty"`
	Alerts        []wireAlert                `json:"alerts,omitempty"`
	Anomalies     []wireAnomaly              `json:"anomalies,omitempty"`
}

type wireTask struct {
	Integration           string                           `json:"integration"`
	IntegrationJob        string                           `json:"integrationJob"`
	Parameters            json.RawMessage                  `json:"parameters"`
	DependsOn             []string                         `json:"dependsOn"`
	Condition             *string                          `json:"condition,omitempty"`
	Name                  string                           `json:"name,omitempty"`
	Tags                  []string                         `json:"tags"`
	Connection            *string                          `json:"connection,omitempty"`
	OperationMetadata     map[string]wireOperationMetadata `json:"operationMetadata,omitempty"`
	TreatFailureAsWarning *bool                            `json:"treatFailureAsWarning,omitempty"`
	Configuration         *wireConfiguration               `json:"configuration,omitempty"`
	Meta                  json.RawMessage                  `json:"meta,omitempty"`
	Alerts                []wireAlert                      `json:"alerts,omitempty"`
	Anomalies             []wireAnomaly                    `json:"anomalies,omitempty"`
	Paused                *bool                            `json:"paused,omitempty"`
}

type wireTaskGroup struct {
	Tasks     map[string]wireTask `json:"tasks"`
	DependsOn []string            `json:"dependsOn"`
	Condition *string             `json:"condition,omitempty"`
	Name      *string             `json:"name,omitempty"`
	Matrix    *wireMatrix         `json:"matrix,omitempty"`
	Meta      json.RawMessage     `json:"meta,omitempty"`
	Paused    *bool               `json:"paused,omitempty"`
}

type wireMatrix struct {
	Inputs          json.RawMessage `json:"inputs"`
	MaxParallel     *int64          `json:"maxParallel,omitempty"`
	Sequential      *bool           `json:"sequential,omitempty"`
	ContinueOnError *bool           `json:"continueOnError,omitempty"`
}

type wireSchedule struct {
	Name        *string         `json:"name,omitempty"`
	Cron        string          `json:"cron"`
	Timezone    *string         `json:"timezone,omitempty"`
	Environment *string         `json:"environment,omitempty"`
	Branch      *string         `json:"branch,omitempty"`
	Exclude     []string        `json:"exclude"`
	RunInputs   json.RawMessage `json:"runInputs,omitempty"`
}

type wireSensor struct {
	Name          string                     `json:"name"`
	Cron          string                     `json:"cron"`
	Timezone      string                     `json:"timezone"`
	TimeoutMins   int64                      `json:"timeoutMins"`
	Environments  []string                   `json:"environments"`
	Checks        map[string]wireSensorCheck `json:"checks"`
	Condition     *string                    `json:"condition,omitempty"`
	Exclude       []string                   `json:"exclude"`
	FrequencySecs *int64                     `json:"frequencySecs,omitempty"`
	RunInputs     json.RawMessage            `json:"runInputs,omitempty"`
	Alerts        []wireAlert                `json:"alerts,omitempty"`
}

type wireSensorCheck struct {
	Integration string            `json:"integration"`
	Connection  *string           `json:"connection,omitempty"`
	SensorType  string            `json:"sensorType"`
	Parameters  json.RawMessage   `json:"parameters"`
	MapOutputs  map[string]string `json:"mapOutputs,omitempty"`
}

type wireTriggerEvent struct {
	Type       string          `json:"type"`
	PipelineID string          `json:"pipelineId"`
	Statuses   []string        `json:"statuses"`
	RunInputs  json.RawMessage `json:"runInputs,omitempty"`
}

type wireWebhook struct {
	Enabled           bool                             `json:"enabled"`
	OperationMetadata map[string]wireOperationMetadata `json:"operationMetadata,omitempty"`
	RunInputs         json.RawMessage                  `json:"runInputs,omitempty"`
}

type wireOperationMetadata struct {
	Integration            string  `json:"integration"`
	Connection             *string `json:"connection,omitempty"`
	S3BucketName           *string `json:"s3_bucket_name,omitempty"`
	S3KeyPrefix            *string `json:"s3_key_prefix,omitempty"`
	DbtWarehouseIdentifier *string `json:"dbt_warehouse_identifier,omitempty"`
}

type wireConfiguration struct {
	Retries     *int64           `json:"retries,omitempty"`
	RetryDelay  *int64           `json:"retryDelay,omitempty"`
	Timeout     *int64           `json:"timeout,omitempty"`
	Concurrency *wireConcurrency `json:"concurrency,omitempty"`
}

type wireConcurrency struct {
	// MaxActive is ParameterizedIntStr: an integer or a ${{ }} template.
	MaxActive *json.RawMessage `json:"maxActive,omitempty"`
	OnLimit   *string          `json:"onLimit,omitempty"`
}

type wireInput struct {
	Type     string          `json:"type"`
	Default  json.RawMessage `json:"default,omitempty"`
	Optional *bool           `json:"optional,omitempty"`
}

type wireAlert struct {
	Name          string                   `json:"name"`
	Statuses      []string                 `json:"statuses"`
	Destinations  []alerts.WireDestination `json:"destinations"`
	CustomMessage *string                  `json:"customMessage,omitempty"`
	// MaxLiveAlerts is ParameterizedIntStr: an integer or a ${{ }} template.
	MaxLiveAlerts *json.RawMessage `json:"maxLiveAlerts,omitempty"`
}

// wireAnomaly flattens the discriminated oneOf of the four duration-anomaly
// models. They share every field except the `type` discriminator.
type wireAnomaly struct {
	Type                    string                   `json:"type"`
	Destinations            []alerts.WireDestination `json:"destinations,omitempty"`
	MinBaselineSeconds      *int64                   `json:"minBaselineSeconds,omitempty"`
	PercentageAboveBaseline *int64                   `json:"percentageAboveBaseline,omitempty"`
	PercentageBelowBaseline *int64                   `json:"percentageBelowBaseline,omitempty"`
	OperationTypes          []string                 `json:"operationTypes"`
	OperationNames          []string                 `json:"operationNames"`
	MaxAnomalies            *int64                   `json:"maxAnomalies,omitempty"`
}
