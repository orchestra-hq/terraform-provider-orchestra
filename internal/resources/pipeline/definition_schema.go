package pipeline

import (
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/enums"
)

// definitionAttribute builds the `data` attribute: the full PipelineModel tree
// from the OpenAPI spec, typed out rather than carried as an opaque blob.
func definitionAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		Optional:            true,
		Description:         "The pipeline definition: its tasks, schedule, sensors and alerts.",
		MarkdownDescription: "The pipeline definition: its tasks, schedule, sensors and alerts.",
		Attributes: map[string]schema.Attribute{
			"version": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("v1"),
				Description:         "Pipeline schema version. Currently only 'v1' is supported.",
				MarkdownDescription: "Pipeline schema version. Currently only `v1` is supported.",
				Validators:          []validator.String{stringvalidator.OneOf("v1")},
			},
			"name": schema.StringAttribute{
				Required:            true,
				Description:         "Unique identifier for this pipeline (max 100 chars). Used in UI and API.",
				MarkdownDescription: "Unique identifier for this pipeline (max 100 chars). Used in UI and API.",
				Validators:          []validator.String{stringvalidator.LengthAtMost(100)},
			},
			"task_groups": schema.MapNestedAttribute{
				Optional:            true,
				NestedObject:        taskGroupSchema(),
				Description:         "Task groups keyed by group ID. Groups are the primary way to build a DAG; dependencies between them define execution order.",
				MarkdownDescription: "Task groups keyed by group ID. Groups are the primary way to build a DAG; dependencies between them define execution order. Group IDs must not collide with `tasks` keys — both are merged into the API's single `pipeline` object.",
			},
			"tasks": schema.MapNestedAttribute{
				Optional:            true,
				NestedObject:        taskSchema(),
				Description:         "Standalone top-level tasks keyed by task ID, for pipelines that do not need grouping.",
				MarkdownDescription: "Standalone top-level tasks keyed by task ID, for pipelines that do not need grouping. Task IDs must not collide with `task_groups` keys.",
			},
			"schedule": schema.ListNestedAttribute{
				Optional:     true,
				NestedObject: scheduleSchema(),
				Description:  "Cron schedules that trigger this pipeline.",
			},
			"sensors": schema.MapNestedAttribute{
				Optional:     true,
				NestedObject: sensorSchema(),
				Description:  "Sensors keyed by sensor ID. A sensor triggers the pipeline once all of its checks pass.",
			},
			"trigger_events": schema.ListNestedAttribute{
				Optional:     true,
				NestedObject: triggerEventSchema(),
				Description:  "Pipeline completion events that trigger this pipeline.",
			},
			"webhook": schema.SingleNestedAttribute{
				Optional:   true,
				Computed:   true,
				Attributes: webhookAttributes(),
				// The API declares webhook non-nullable with a default of
				// {enabled: false} and echoes it back on every read, verified
				// against the live API. Defaulting to the same object keeps the
				// planned value equal to what apply returns; without it, a
				// config with no webhook block fails every apply.
				Default: objectdefault.StaticValue(types.ObjectValueMust(
					webhookObjectType().AttrTypes,
					map[string]attr.Value{
						"enabled":            types.BoolValue(false),
						"operation_metadata": types.MapNull(operationMetadataObjectType()),
						"run_inputs":         jsontypes.NewNormalizedNull(),
					},
				)),
				Description: "Webhook configuration for manual and API triggers.",
			},
			"configuration": schema.SingleNestedAttribute{
				Optional:            true,
				Attributes:          configurationAttributes(),
				Description:         "Default retry and timeout settings inherited by all tasks.",
				MarkdownDescription: "Default retry and timeout settings inherited by all tasks. Task-level `configuration` overrides these.",
			},
			"inputs": schema.MapNestedAttribute{
				Optional:     true,
				NestedObject: inputSchema(),
				Description:  "Declared runtime inputs keyed by input name.",
			},
			"meta": schema.StringAttribute{
				Optional:            true,
				CustomType:          jsontypes.NormalizedType{},
				Description:         "Arbitrary metadata for external tooling, as a JSON object.",
				MarkdownDescription: "Arbitrary metadata for external tooling, as a JSON object. The API accepts any shape here, so Terraform carries it as JSON — use `jsonencode({...})`.",
			},
			"alerts": schema.ListNestedAttribute{
				Optional:     true,
				NestedObject: alertSchema(),
				Description:  "Alerts to send when the pipeline run completes.",
			},
			"anomalies": schema.ListNestedAttribute{
				Optional:     true,
				NestedObject: anomalySchema(pipelineAnomalyTypes, false),
				Description:  "Pipeline duration anomaly monitors.",
			},
		},
	}
}

var (
	pipelineAnomalyTypes = []string{
		"pipeline_duration_above_baseline",
		"pipeline_duration_below_baseline",
	}
	taskAnomalyTypes = []string{
		"task_duration_above_baseline",
		"task_duration_below_baseline",
		"operation_duration_above_baseline",
		"operation_duration_below_baseline",
	}
)

func taskSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"integration": schema.StringAttribute{
				Required:    true,
				Description: "The integration platform (SNOWFLAKE, DBT, AWS_ECS, etc.).",
				Validators:  []validator.String{stringvalidator.OneOf(enums.Integrations...)},
			},
			"integration_job": schema.StringAttribute{
				Required:    true,
				Description: "The operation to perform. Must be compatible with the integration.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.IntegrationJobs...)},
			},
			"parameters": schema.StringAttribute{
				Required:            true,
				CustomType:          jsontypes.NormalizedType{},
				Description:         "Integration-specific parameters, as a JSON object. Structure depends on integration_job.",
				MarkdownDescription: "Integration-specific parameters, as a JSON object. The shape depends on `integration_job`, so the API declares it free-form and Terraform carries it as JSON — use `jsonencode({...})`.",
			},
			"depends_on": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				// The API declares dependsOn non-nullable with a default of [],
				// so it always echoes a list back. Defaulting to [] here keeps
				// the planned value equal to what apply will return.
				Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
				Description: "IDs of tasks or groups that must complete before this task runs.",
			},
			"condition": schema.StringAttribute{
				Optional:            true,
				Description:         "Expression that determines whether this task runs.",
				MarkdownDescription: "Expression using `${{ expression }}` syntax that determines whether this task runs.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Human-readable name for display in the UI (max 100 chars).",
				Validators:  []validator.String{stringvalidator.LengthAtMost(100)},
			},
			"tags": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Labels for filtering and organization (max 10 tags).",
			},
			"connection": schema.StringAttribute{
				Optional:            true,
				Description:         "Named connection to use for this task's integration.",
				MarkdownDescription: "Named connection to use for this task's integration. Format: `name_XXXX`, or a `${{ }}` template.",
			},
			"operation_metadata": schema.MapNestedAttribute{
				Optional:     true,
				NestedObject: operationMetadataSchema(),
				Description:  "Per-integration operation metadata, keyed by metadata name.",
			},
			"treat_failure_as_warning": schema.BoolAttribute{
				Optional:    true,
				Description: "If true, task failure results in WARNING status instead of FAILED.",
			},
			"configuration": schema.SingleNestedAttribute{
				Optional:            true,
				Attributes:          configurationAttributes(),
				Description:         "Task-specific retry and timeout settings.",
				MarkdownDescription: "Task-specific retry and timeout settings. Overrides the pipeline-level `configuration`.",
			},
			"meta": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Arbitrary metadata for external tooling, as a JSON object.",
			},
			"alerts": schema.ListNestedAttribute{
				Optional:     true,
				NestedObject: alertSchema(),
				Description:  "Alerts to send when the task completes.",
			},
			"anomalies": schema.ListNestedAttribute{
				Optional:     true,
				NestedObject: anomalySchema(taskAnomalyTypes, true),
				Description:  "Task and operation duration anomaly monitors.",
			},
			"paused": schema.BoolAttribute{
				Optional:    true,
				Description: "When true, this task does not run. Downstream tasks execute as if it were not in the DAG.",
			},
		},
	}
}

func taskGroupSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"tasks": schema.MapNestedAttribute{
				Required:     true,
				NestedObject: taskSchema(),
				Description:  "Task definitions within this group, keyed by task ID.",
			},
			"depends_on": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				// The API declares dependsOn non-nullable with a default of [],
				// so it always echoes a list back. Defaulting to [] here keeps
				// the planned value equal to what apply will return.
				Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
				Description: "IDs of other top-level entries that must complete before this group runs.",
			},
			"condition": schema.StringAttribute{
				Optional:            true,
				Description:         "Expression applied to every task in the group.",
				MarkdownDescription: "Expression using `${{ expression }}` syntax applied to every task in the group.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Human-readable name for the group (max 100 chars).",
				Validators:  []validator.String{stringvalidator.LengthAtMost(100)},
			},
			"matrix": schema.SingleNestedAttribute{
				Optional:    true,
				Attributes:  matrixAttributes(),
				Description: "Run the group multiple times with varying inputs.",
			},
			"meta": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Arbitrary metadata for external tooling, as a JSON object.",
			},
			"paused": schema.BoolAttribute{
				Optional:    true,
				Description: "When true, no task in this group runs.",
			},
		},
	}
}

func matrixAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"inputs": schema.StringAttribute{
			Required:            true,
			CustomType:          jsontypes.NormalizedType{},
			Description:         "JSON object mapping each input name to a list of values, or to a template expression.",
			MarkdownDescription: "JSON object mapping each input name to a list of values, or to a `${{ }}` template expression. Values may be of any type, so Terraform carries this as JSON — use `jsonencode({...})`.",
		},
		"max_parallel": schema.Int64Attribute{
			Optional:    true,
			Description: "Maximum concurrent matrix iterations.",
		},
		"sequential": schema.BoolAttribute{
			Optional:    true,
			Description: "If true, run iterations sequentially instead of in parallel.",
		},
		"continue_on_error": schema.BoolAttribute{
			Optional:    true,
			Description: "If true, continue with remaining iterations even if one fails.",
		},
	}
}

func scheduleSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"cron": schema.StringAttribute{
				Required:    true,
				Description: "AWS EventBridge cron expression.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Schedule name for display and reference.",
			},
			"timezone": schema.StringAttribute{
				Optional:    true,
				Description: "IANA timezone for schedule evaluation.",
			},
			"environment": schema.StringAttribute{
				Optional:    true,
				Description: "Environment name or ID to run in.",
			},
			"branch": schema.StringAttribute{
				Optional:    true,
				Description: "Branch to run from, for git-backed pipelines.",
			},
			"exclude": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Dates (YYYY-MM-DD) to skip.",
			},
			"run_inputs": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Input values to pass on each scheduled run, as a JSON object.",
			},
		},
	}
}

func sensorSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Sensor name for display and reference (max 100 chars).",
				Validators:  []validator.String{stringvalidator.LengthAtMost(100)},
			},
			"cron": schema.StringAttribute{
				Required:    true,
				Description: "Cron expression defining when the sensor evaluation window opens.",
			},
			"timezone": schema.StringAttribute{
				Required:    true,
				Description: "IANA timezone for schedule evaluation.",
			},
			"timeout_mins": schema.Int64Attribute{
				Required:    true,
				Description: "Maximum minutes to wait for all checks to pass (max 7200).",
			},
			"checks": schema.MapNestedAttribute{
				Required:     true,
				NestedObject: sensorCheckSchema(),
				Description:  "Checks that must all pass before the pipeline is triggered, keyed by check ID.",
			},
			"environments": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Environments to run in when the sensor fires.",
			},
			"condition": schema.StringAttribute{
				Optional:            true,
				Description:         "Expression evaluated after all checks pass, before triggering the pipeline.",
				MarkdownDescription: "Expression using `${{ }}` syntax, evaluated after all checks pass and before triggering the pipeline.",
			},
			"exclude": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Dates (YYYY-MM-DD) to skip sensor evaluation.",
			},
			"frequency_secs": schema.Int64Attribute{
				Optional:    true,
				Description: "How often checks are evaluated within the open window, in seconds (60-600).",
			},
			"run_inputs": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Input values to pass when the sensor triggers the pipeline, as a JSON object.",
			},
			"alerts": schema.ListNestedAttribute{
				Optional:     true,
				NestedObject: alertSchema(),
				Description:  "Alerts to send for this sensor.",
			},
		},
	}
}

func sensorCheckSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"integration": schema.StringAttribute{
				Required:    true,
				Description: "Integration platform for this check.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.Integrations...)},
			},
			"sensor_type": schema.StringAttribute{
				Required:    true,
				Description: "Type of sensor check to perform.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.SensorChecks...)},
			},
			"parameters": schema.StringAttribute{
				Required:            true,
				CustomType:          jsontypes.NormalizedType{},
				Description:         "Check parameters, as a JSON object. Structure depends on sensor_type.",
				MarkdownDescription: "Check parameters, as a JSON object. The shape depends on `sensor_type`, so the API declares it free-form — use `jsonencode({...})`.",
			},
			"connection": schema.StringAttribute{
				Optional:    true,
				Description: "Named connection to use for this check.",
			},
			"map_outputs": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Maps check result fields to pipeline outputs.",
			},
		},
	}
}

func triggerEventSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"pipeline_id": schema.StringAttribute{
				Required:            true,
				Description:         "UUID of the pipeline whose completion triggers this one, or '*' for any pipeline.",
				MarkdownDescription: "UUID of the pipeline whose completion triggers this one, or `*` for any pipeline.",
			},
			"type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("pipeline"),
				Description:         "Event type. Currently only 'pipeline' is supported.",
				MarkdownDescription: "Event type. Currently only `pipeline` is supported.",
				Validators:          []validator.String{stringvalidator.OneOf("pipeline")},
			},
			"statuses": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Pipeline run statuses that fire the trigger.",
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf(enums.PipelineRunStatuses...)),
				},
			},
			"run_inputs": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Input values to pass to the triggered pipeline run, as a JSON object.",
			},
		},
	}
}

func webhookAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"enabled": schema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Default:     booldefault.StaticBool(false),
			Description: "Whether the webhook trigger is enabled.",
		},
		"operation_metadata": schema.MapNestedAttribute{
			Optional:     true,
			NestedObject: operationMetadataSchema(),
			Description:  "Per-integration operation metadata, keyed by metadata name.",
		},
		"run_inputs": schema.StringAttribute{
			Optional:    true,
			CustomType:  jsontypes.NormalizedType{},
			Description: "Default input values for webhook-triggered runs, as a JSON object.",
		},
	}
}

func operationMetadataSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"integration": schema.StringAttribute{
				Required:    true,
				Description: "Integration this metadata applies to.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.Integrations...)},
			},
			"connection": schema.StringAttribute{
				Optional:    true,
				Description: "Named connection to use.",
			},
			"s3_bucket_name": schema.StringAttribute{
				Optional:    true,
				Description: "S3 bucket to write operation metadata to.",
			},
			"s3_key_prefix": schema.StringAttribute{
				Optional:    true,
				Description: "Key prefix within the S3 bucket.",
			},
			"dbt_warehouse_identifier": schema.StringAttribute{
				Optional:    true,
				Description: "Warehouse identifier for dbt operation metadata.",
			},
		},
	}
}

func configurationAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"retries": schema.Int64Attribute{
			Optional:    true,
			Description: "Number of times to retry on failure.",
		},
		"retry_delay": schema.Int64Attribute{
			Optional:    true,
			Description: "Seconds to wait between retries.",
		},
		"timeout": schema.Int64Attribute{
			Optional:    true,
			Description: "Timeout in seconds.",
		},
		"concurrency": schema.SingleNestedAttribute{
			Optional: true,
			Attributes: map[string]schema.Attribute{
				"max_active": schema.StringAttribute{
					Optional:            true,
					Description:         "Maximum concurrent runs, as a number or a template expression.",
					MarkdownDescription: "Maximum concurrent runs. The API accepts an integer or a `${{ }}` template, so Terraform carries both as a string (for example `\"3\"`).",
				},
			},
			Description: "Concurrency limits.",
		},
	}
}

func inputSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"type": schema.StringAttribute{
				Required:    true,
				Description: "Data type of the input.",
				Validators:  []validator.String{stringvalidator.OneOf("string", "number", "boolean", "dict", "list")},
			},
			"default": schema.StringAttribute{
				Optional:            true,
				CustomType:          jsontypes.NormalizedType{},
				Description:         "Default value, JSON-encoded, used when the input is not supplied at runtime.",
				MarkdownDescription: "Default value used when the input is not supplied at runtime. The API accepts any type here, so Terraform carries it JSON-encoded — `jsonencode(\"a string\")`, `jsonencode(3)`, `jsonencode([1,2])`.",
			},
			"optional": schema.BoolAttribute{
				Optional:            true,
				Description:         "If true, the input can be omitted even without a default value.",
				MarkdownDescription: "If true, the input can be omitted even without a default value. Defaults to `false`.",
			},
		},
	}
}

func alertSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Alert name.",
			},
			"statuses": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Run statuses that fire this alert.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(stringvalidator.OneOf(enums.AlertLevels...)),
				},
			},
			"destinations": schema.ListNestedAttribute{
				Required:     true,
				NestedObject: alertDestinationSchema(),
				Description:  "Where to send the alert.",
				Validators:   []validator.List{listvalidator.SizeAtLeast(1)},
			},
			"custom_message": schema.StringAttribute{
				Optional:    true,
				Description: "Custom alert message (max 200 chars).",
				Validators:  []validator.String{stringvalidator.LengthAtMost(200)},
			},
			"max_live_alerts": schema.StringAttribute{
				Optional:            true,
				Description:         "Maximum simultaneous live alerts, as a number or a template expression.",
				MarkdownDescription: "Maximum simultaneous live alerts. The API accepts an integer or a `${{ }}` template, so Terraform carries both as a string.",
			},
		},
	}
}

func alertDestinationSchema() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"integration": schema.StringAttribute{
				Required:    true,
				Description: "Notification integration to send through.",
				Validators:  []validator.String{stringvalidator.OneOf(enums.NotificationTypes...)},
			},
			"destination": schema.StringAttribute{
				Optional:            true,
				Description:         "Where to deliver: an email address, Slack channel, or webhook URL.",
				MarkdownDescription: "Where to deliver: an email address, Slack channel, or webhook URL, depending on `integration`.",
			},
			"connection_id": schema.StringAttribute{
				Optional:    true,
				Description: "Connection to authenticate the delivery with.",
			},
			"priority": schema.Int64Attribute{
				Optional:            true,
				Description:         "Datadog notification priority (1-5). Only valid when integration is DATADOG.",
				MarkdownDescription: "Datadog notification priority (1-5). Only valid when `integration = \"DATADOG\"`.",
			},
			"body": schema.StringAttribute{
				Optional:            true,
				CustomType:          jsontypes.NormalizedType{},
				Description:         "Webhook request body, JSON-encoded. Only valid when integration is WEBHOOK.",
				MarkdownDescription: "Webhook request body, JSON-encoded. Only valid when `integration = \"WEBHOOK\"`. The API accepts an object or a string, so encode either — `jsonencode({...})` or `jsonencode(\"text\")`.",
			},
		},
	}
}

func anomalySchema(allowedTypes []string, operationScoped bool) schema.NestedAttributeObject {
	attrs := map[string]schema.Attribute{
		"type": schema.StringAttribute{
			Required:    true,
			Description: "Which anomaly to monitor for.",
			Validators:  []validator.String{stringvalidator.OneOf(allowedTypes...)},
		},
		"destinations": schema.ListNestedAttribute{
			Optional:     true,
			NestedObject: alertDestinationSchema(),
			Description:  "Where to send the anomaly alert.",
		},
		"min_baseline_seconds": schema.Int64Attribute{
			Optional: true,
			// Server-defaulted and echoed back, so Computed; the spec's declared
			// default is not reliable (it says 0, the API returns 10).
			Computed:    true,
			Description: "Skip monitoring when the baseline is shorter than this (max 86400). Defaulted by Orchestra when not set.",
		},
		"percentage_above_baseline": schema.Int64Attribute{
			Optional:            true,
			Computed:            true,
			Description:         "Trigger when duration exceeds the baseline by this percentage (1-1000). Only for *_above_baseline types. Defaulted by Orchestra when not set.",
			MarkdownDescription: "Trigger when duration exceeds the baseline by this percentage (1-1000). Only valid for `*_above_baseline` types. Defaulted by Orchestra when not set.",
		},
		"percentage_below_baseline": schema.Int64Attribute{
			Optional:            true,
			Computed:            true,
			Description:         "Trigger when duration falls below the baseline by this percentage (1-99). Only for *_below_baseline types. Defaulted by Orchestra when not set.",
			MarkdownDescription: "Trigger when duration falls below the baseline by this percentage (1-99). Only valid for `*_below_baseline` types. Defaulted by Orchestra when not set.",
		},
	}

	if operationScoped {
		attrs["operation_types"] = schema.ListAttribute{
			Optional:            true,
			ElementType:         types.StringType,
			Description:         "Only monitor operations of these types. Only for operation_duration_* types.",
			MarkdownDescription: "Only monitor operations of these types. Only valid for `operation_duration_*` types.",
			Validators:          []validator.List{listvalidator.ValueStringsAre(stringvalidator.OneOf(enums.OperationTypes...))},
		}
		attrs["operation_names"] = schema.ListAttribute{
			Optional:            true,
			ElementType:         types.StringType,
			Description:         "Glob patterns matched against operation names, e.g. 'fct_*'. Only for operation_duration_* types.",
			MarkdownDescription: "Glob patterns matched against operation names, for example `fct_*`. Only valid for `operation_duration_*` types.",
		}
		attrs["max_anomalies"] = schema.Int64Attribute{
			Optional:            true,
			Computed:            true,
			Description:         "How many of this task's operation anomalies to report (1-100). Only for operation_duration_* types.",
			MarkdownDescription: "How many of this task's operation anomalies to report (1-100). Only valid for `operation_duration_*` types.",
		}
	}

	return schema.NestedAttributeObject{Attributes: attrs}
}

// webhookObjectType and operationMetadataObjectType spell out the attribute
// types the webhook default needs. They are derived from the schemas above so
// the two cannot drift apart.
func operationMetadataObjectType() attr.Type {
	return operationMetadataSchema().Type()
}

func webhookObjectType() types.ObjectType {
	attrs := map[string]attr.Type{}
	for name, a := range webhookAttributes() {
		attrs[name] = a.GetType()
	}
	return types.ObjectType{AttrTypes: attrs}
}
