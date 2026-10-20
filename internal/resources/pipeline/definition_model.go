package pipeline

import (
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/alerts"
)

// Terraform-side model of the pipeline definition. Mirrors definition_wire.go
// but in framework types, with the two unavoidable departures from the spec:
//
//   - `pipeline` is map[string](TaskGroupModel | TaskModel) in the API. Terraform
//     has no union type, so it is split into `task_groups` and `tasks` here and
//     merged back into one object on the wire.
//   - fields the API declares as free-form (additionalProperties: true, or an
//     untyped `any`) are normalized JSON strings.

type definitionTF struct {
	Version       types.String           `tfsdk:"version"`
	Name          types.String           `tfsdk:"name"`
	TaskGroups    map[string]taskGroupTF `tfsdk:"task_groups"`
	Tasks         map[string]taskTF      `tfsdk:"tasks"`
	Schedule      []scheduleTF           `tfsdk:"schedule"`
	Sensors       map[string]sensorTF    `tfsdk:"sensors"`
	TriggerEvents []triggerEventTF       `tfsdk:"trigger_events"`
	Webhook       *webhookTF             `tfsdk:"webhook"`
	Configuration *configurationTF       `tfsdk:"configuration"`
	Inputs        map[string]inputTF     `tfsdk:"inputs"`
	Meta          jsontypes.Normalized   `tfsdk:"meta"`
	Alerts        []alertTF              `tfsdk:"alerts"`
	Anomalies     []anomalyTF            `tfsdk:"anomalies"`
}

type taskTF struct {
	Integration           types.String                   `tfsdk:"integration"`
	IntegrationJob        types.String                   `tfsdk:"integration_job"`
	Parameters            jsontypes.Normalized           `tfsdk:"parameters"`
	DependsOn             []types.String                 `tfsdk:"depends_on"`
	Condition             types.String                   `tfsdk:"condition"`
	Name                  types.String                   `tfsdk:"name"`
	Tags                  []types.String                 `tfsdk:"tags"`
	Connection            types.String                   `tfsdk:"connection"`
	OperationMetadata     map[string]operationMetadataTF `tfsdk:"operation_metadata"`
	TreatFailureAsWarning types.Bool                     `tfsdk:"treat_failure_as_warning"`
	Configuration         *configurationTF               `tfsdk:"configuration"`
	Meta                  jsontypes.Normalized           `tfsdk:"meta"`
	Alerts                []alertTF                      `tfsdk:"alerts"`
	Anomalies             []taskAnomalyTF                `tfsdk:"anomalies"`
	Paused                types.Bool                     `tfsdk:"paused"`
}

type taskGroupTF struct {
	Tasks     map[string]taskTF    `tfsdk:"tasks"`
	DependsOn []types.String       `tfsdk:"depends_on"`
	Condition types.String         `tfsdk:"condition"`
	Name      types.String         `tfsdk:"name"`
	Matrix    *matrixTF            `tfsdk:"matrix"`
	Meta      jsontypes.Normalized `tfsdk:"meta"`
	Paused    types.Bool           `tfsdk:"paused"`
}

type matrixTF struct {
	Inputs          jsontypes.Normalized `tfsdk:"inputs"`
	MaxParallel     types.Int64          `tfsdk:"max_parallel"`
	Sequential      types.Bool           `tfsdk:"sequential"`
	ContinueOnError types.Bool           `tfsdk:"continue_on_error"`
}

type scheduleTF struct {
	Name        types.String         `tfsdk:"name"`
	Cron        types.String         `tfsdk:"cron"`
	Timezone    types.String         `tfsdk:"timezone"`
	Environment types.String         `tfsdk:"environment"`
	Branch      types.String         `tfsdk:"branch"`
	Exclude     []types.String       `tfsdk:"exclude"`
	RunInputs   jsontypes.Normalized `tfsdk:"run_inputs"`
}

type sensorTF struct {
	Name          types.String             `tfsdk:"name"`
	Cron          types.String             `tfsdk:"cron"`
	Timezone      types.String             `tfsdk:"timezone"`
	TimeoutMins   types.Int64              `tfsdk:"timeout_mins"`
	Environments  []types.String           `tfsdk:"environments"`
	Checks        map[string]sensorCheckTF `tfsdk:"checks"`
	Condition     types.String             `tfsdk:"condition"`
	Exclude       []types.String           `tfsdk:"exclude"`
	FrequencySecs types.Int64              `tfsdk:"frequency_secs"`
	RunInputs     jsontypes.Normalized     `tfsdk:"run_inputs"`
	Alerts        []alertTF                `tfsdk:"alerts"`
}

type sensorCheckTF struct {
	Integration types.String            `tfsdk:"integration"`
	Connection  types.String            `tfsdk:"connection"`
	SensorType  types.String            `tfsdk:"sensor_type"`
	Parameters  jsontypes.Normalized    `tfsdk:"parameters"`
	MapOutputs  map[string]types.String `tfsdk:"map_outputs"`
}

type triggerEventTF struct {
	Type       types.String         `tfsdk:"type"`
	PipelineID types.String         `tfsdk:"pipeline_id"`
	Statuses   []types.String       `tfsdk:"statuses"`
	RunInputs  jsontypes.Normalized `tfsdk:"run_inputs"`
}

type webhookTF struct {
	Enabled           types.Bool                     `tfsdk:"enabled"`
	OperationMetadata map[string]operationMetadataTF `tfsdk:"operation_metadata"`
	RunInputs         jsontypes.Normalized           `tfsdk:"run_inputs"`
}

type operationMetadataTF struct {
	Integration            types.String `tfsdk:"integration"`
	Connection             types.String `tfsdk:"connection"`
	S3BucketName           types.String `tfsdk:"s3_bucket_name"`
	S3KeyPrefix            types.String `tfsdk:"s3_key_prefix"`
	DbtWarehouseIdentifier types.String `tfsdk:"dbt_warehouse_identifier"`
}

type configurationTF struct {
	Retries     types.Int64    `tfsdk:"retries"`
	RetryDelay  types.Int64    `tfsdk:"retry_delay"`
	Timeout     types.Int64    `tfsdk:"timeout"`
	Concurrency *concurrencyTF `tfsdk:"concurrency"`
}

type concurrencyTF struct {
	// MaxActive is ParameterizedIntStr in the spec: an integer or a ${{ }}
	// template. Terraform carries both as a string.
	MaxActive types.String `tfsdk:"max_active"`
	OnLimit   types.String `tfsdk:"on_limit"`
}

type inputTF struct {
	Type     types.String         `tfsdk:"type"`
	Default  jsontypes.Normalized `tfsdk:"default"`
	Optional types.Bool           `tfsdk:"optional"`
	Options  []types.String       `tfsdk:"options"`
}

type alertTF struct {
	Name          types.String           `tfsdk:"name"`
	Statuses      []types.String         `tfsdk:"statuses"`
	Destinations  []alerts.DestinationTF `tfsdk:"destinations"`
	CustomMessage types.String           `tfsdk:"custom_message"`
	// MaxLiveAlerts is ParameterizedIntStr; see concurrencyTF.MaxActive.
	MaxLiveAlerts types.String `tfsdk:"max_live_alerts"`
}

// anomalyTF is the pipeline-level anomaly. The API's pipeline_duration_*
// models have no operation scoping, so those attributes are absent from the
// schema and must be absent from the struct too.
type anomalyTF struct {
	Type                    types.String           `tfsdk:"type"`
	Destinations            []alerts.DestinationTF `tfsdk:"destinations"`
	MinBaselineSeconds      types.Int64            `tfsdk:"min_baseline_seconds"`
	PercentageAboveBaseline types.Int64            `tfsdk:"percentage_above_baseline"`
	PercentageBelowBaseline types.Int64            `tfsdk:"percentage_below_baseline"`
}

// taskAnomalyTF is the task-level anomaly, which additionally covers the
// operation_duration_* models and their operation scoping.
type taskAnomalyTF struct {
	Type                    types.String           `tfsdk:"type"`
	Destinations            []alerts.DestinationTF `tfsdk:"destinations"`
	MinBaselineSeconds      types.Int64            `tfsdk:"min_baseline_seconds"`
	PercentageAboveBaseline types.Int64            `tfsdk:"percentage_above_baseline"`
	PercentageBelowBaseline types.Int64            `tfsdk:"percentage_below_baseline"`
	OperationTypes          []types.String         `tfsdk:"operation_types"`
	OperationNames          []types.String         `tfsdk:"operation_names"`
	MaxAnomalies            types.Int64            `tfsdk:"max_anomalies"`
}
