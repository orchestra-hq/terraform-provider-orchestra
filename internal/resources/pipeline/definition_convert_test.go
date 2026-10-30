package pipeline

import (
	"encoding/json"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/alerts"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func sampleDefinition() *definitionTF {
	return &definitionTF{
		Version: types.StringValue("v1"),
		Name:    types.StringValue("daily_sales"),
		TaskGroups: map[string]taskGroupTF{
			"transform": {
				DependsOn: []types.String{types.StringValue("ingest")},
				Name:      types.StringValue("Transform"),
				Condition: types.StringNull(),
				Meta:      jsontypes.NewNormalizedValue(`{"owner":"analytics"}`),
				Paused:    types.BoolValue(false),
				Matrix: &matrixTF{
					Inputs:          jsontypes.NewNormalizedValue(`{"table":["orders","customers"]}`),
					MaxParallel:     types.Int64Value(2),
					Sequential:      types.BoolNull(),
					ContinueOnError: types.BoolValue(true),
				},
				Tasks: map[string]taskTF{
					"dbt_build": {
						Integration:    types.StringValue("DBT_CORE"),
						IntegrationJob: types.StringValue("DBT_CORE_EXECUTE"),
						Parameters:     jsontypes.NewNormalizedValue(`{"commands":["dbt build"]}`),
						Name:           types.StringValue("dbt build"),
						Tags:           []types.String{types.StringValue("nightly")},
						Connection:     types.StringValue("name_ABCD"),
						DependsOn:      nil,
						Condition:      types.StringNull(),
						Meta:           jsontypes.NewNormalizedNull(),
						Configuration: &configurationTF{
							Retries:     types.StringValue("${{ inputs.retries }}"),
							RetryDelay:  types.Int64Value(60),
							Timeout:     types.Int64Value(3600),
							Concurrency: &concurrencyTF{MaxActive: types.StringValue("3")},
						},
						TreatFailureAsWarning: types.BoolValue(false),
						Paused:                types.BoolNull(),
						OperationMetadata: map[string]operationMetadataTF{
							"s3": {
								Integration:            types.StringValue("AWS_S3"),
								Connection:             types.StringNull(),
								S3BucketName:           types.StringValue("my-bucket"),
								S3KeyPrefix:            types.StringValue("dbt/"),
								DbtWarehouseIdentifier: types.StringNull(),
							},
						},
						Alerts: []alertTF{{
							Name:          types.StringValue("on_failure"),
							Statuses:      []types.String{types.StringValue("FAILED")},
							CustomMessage: types.StringValue("dbt build failed"),
							MaxLiveAlerts: types.StringValue("${{ ENV.MAX_ALERTS }}"),
							Destinations: []alerts.DestinationTF{{
								Integration:  types.StringValue("SLACK"),
								Destination:  types.StringValue("#data-alerts"),
								ConnectionID: types.StringNull(),
								Priority:     types.Int64Null(),
								Body:         jsontypes.NewNormalizedNull(),
							}},
						}},
						Anomalies: []taskAnomalyTF{{
							Type:                    types.StringValue("operation_duration_above_baseline"),
							MinBaselineSeconds:      types.Int64Value(30),
							PercentageAboveBaseline: types.Int64Value(50),
							PercentageBelowBaseline: types.Int64Null(),
							OperationTypes:          []types.String{types.StringValue("MATERIALISATION")},
							OperationNames:          []types.String{types.StringValue("fct_*")},
							MaxAnomalies:            types.Int64Value(5),
							Destinations: []alerts.DestinationTF{{
								Integration:  types.StringValue("DATADOG"),
								Destination:  types.StringNull(),
								ConnectionID: types.StringValue("conn-1"),
								Priority:     types.Int64Value(3),
								Body:         jsontypes.NewNormalizedNull(),
							}},
						}},
					},
				},
			},
		},
		Tasks: map[string]taskTF{
			"ingest": {
				Integration:           types.StringValue("FIVETRAN"),
				IntegrationJob:        types.StringValue("FIVETRAN_SYNC_ALL"),
				Parameters:            jsontypes.NewNormalizedValue(`{"connector_id":"abc"}`),
				Name:                  types.StringNull(),
				Condition:             types.StringValue("${{ inputs.run_ingest }}"),
				Connection:            types.StringNull(),
				Meta:                  jsontypes.NewNormalizedNull(),
				TreatFailureAsWarning: types.BoolValue(true),
				Paused:                types.BoolNull(),
			},
		},
		Schedule: []scheduleTF{{
			Name:        types.StringValue("nightly"),
			Cron:        types.StringValue("cron(0 2 * * ? *)"),
			Timezone:    types.StringValue("Europe/London"),
			Environment: types.StringValue("production"),
			Branch:      types.StringNull(),
			Exclude:     []types.String{types.StringValue("2026-12-25")},
			RunInputs:   jsontypes.NewNormalizedValue(`{"run_ingest":true}`),
		}},
		Sensors: map[string]sensorTF{
			"wait_for_file": {
				Name:          types.StringValue("Wait for file"),
				Cron:          types.StringValue("cron(0 1 * * ? *)"),
				Timezone:      types.StringValue("UTC"),
				TimeoutMins:   types.Int64Value(120),
				Environments:  []types.String{types.StringValue("production")},
				Condition:     types.StringNull(),
				Exclude:       nil,
				FrequencySecs: types.Int64Value(300),
				RunInputs:     jsontypes.NewNormalizedNull(),
				Checks: map[string]sensorCheckTF{
					"landing": {
						Integration: types.StringValue("AWS_S3"),
						SensorType:  types.StringValue("AWS_S3_FILE"),
						Parameters:  jsontypes.NewNormalizedValue(`{"bucket":"landing"}`),
						Connection:  types.StringNull(),
						MapOutputs:  map[string]types.String{"key": types.StringValue("file_key")},
					},
				},
			},
		},
		TriggerEvents: []triggerEventTF{{
			Type:       types.StringValue("pipeline"),
			PipelineID: types.StringValue("*"),
			Statuses:   []types.String{types.StringValue("SUCCEEDED")},
			RunInputs:  jsontypes.NewNormalizedNull(),
		}},
		Webhook: &webhookTF{
			Enabled:   types.BoolValue(true),
			RunInputs: jsontypes.NewNormalizedValue(`{"source":"api"}`),
		},
		Configuration: &configurationTF{
			Retries:     types.StringValue("1"),
			RetryDelay:  types.Int64Null(),
			Timeout:     types.Int64Value(7200),
			Concurrency: &concurrencyTF{MaxActive: types.StringValue("1")},
		},
		Inputs: map[string]inputTF{
			"run_ingest": {
				Type:     types.StringValue("boolean"),
				Default:  jsontypes.NewNormalizedValue(`true`),
				Optional: types.BoolValue(true),
			},
		},
		Meta: jsontypes.NewNormalizedValue(`{"team":"data"}`),
		Alerts: []alertTF{{
			Name:          types.StringValue("pipeline_failed"),
			Statuses:      []types.String{types.StringValue("FAILED"), types.StringValue("WARNING")},
			CustomMessage: types.StringNull(),
			MaxLiveAlerts: types.StringValue("5"),
			Destinations: []alerts.DestinationTF{{
				Integration:  types.StringValue("WEBHOOK"),
				Destination:  types.StringValue("https://example.invalid/hook"),
				ConnectionID: types.StringNull(),
				Priority:     types.Int64Null(),
				Body:         jsontypes.NewNormalizedValue(`{"text":"failed"}`),
			}},
		}},
		Anomalies: []anomalyTF{{
			Type:                    types.StringValue("pipeline_duration_above_baseline"),
			MinBaselineSeconds:      types.Int64Value(60),
			PercentageAboveBaseline: types.Int64Value(20),
			PercentageBelowBaseline: types.Int64Null(),
		}},
	}
}

// TestDefinitionRoundTrip pushes a definition through the wire format and back,
// which is exactly what Create does (send `data`, read the response back into
// state). Anything lost here shows up as a permanent diff in a real plan.
func TestDefinitionRoundTrip(t *testing.T) {
	var diags diag.Diagnostics
	in := sampleDefinition()

	wire := definitionToWire(in, path.Root("data"), &diags)
	if diags.HasError() {
		t.Fatalf("definitionToWire: %v", diags.Errors())
	}

	// Go through real JSON, as the API would.
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded wireDefinition
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	out, err := definitionFromWire(&decoded)
	if err != nil {
		t.Fatalf("definitionFromWire: %v", err)
	}

	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip lost or changed data.\n got: %#v\nwant: %#v", out, in)
	}
}

// TestPipelineUnionSplitsBackCorrectly checks that the two Terraform maps merge
// into the API's single `pipeline` object and split back the same way. The API
// has no discriminator, so groups are told apart by their `tasks` key.
func TestPipelineUnionSplitsBackCorrectly(t *testing.T) {
	var diags diag.Diagnostics
	wire := definitionToWire(sampleDefinition(), path.Root("data"), &diags)
	if diags.HasError() {
		t.Fatalf("definitionToWire: %v", diags.Errors())
	}

	if len(wire.Pipeline) != 2 {
		t.Fatalf("pipeline has %d entries, want 2", len(wire.Pipeline))
	}
	for _, id := range []string{"transform", "ingest"} {
		if _, ok := wire.Pipeline[id]; !ok {
			t.Errorf("pipeline is missing %q", id)
		}
	}

	back, err := definitionFromWire(wire)
	if err != nil {
		t.Fatalf("definitionFromWire: %v", err)
	}
	if _, ok := back.TaskGroups["transform"]; !ok {
		t.Error("transform came back as a standalone task rather than a group")
	}
	if _, ok := back.Tasks["ingest"]; !ok {
		t.Error("ingest came back as a group rather than a standalone task")
	}
}

func TestDuplicateEntryIDIsRejected(t *testing.T) {
	def := &definitionTF{
		Version: types.StringValue("v1"),
		Name:    types.StringValue("clash"),
		TaskGroups: map[string]taskGroupTF{
			"shared": {Tasks: map[string]taskTF{}},
		},
		Tasks: map[string]taskTF{
			"shared": {
				Integration:    types.StringValue("SNOWFLAKE"),
				IntegrationJob: types.StringValue("SNOWFLAKE_RUN_QUERY"),
				Parameters:     jsontypes.NewNormalizedValue(`{}`),
			},
		},
	}

	var diags diag.Diagnostics
	definitionToWire(def, path.Root("data"), &diags)

	if !diags.HasError() {
		t.Fatal("expected an error for an ID used by both task_groups and tasks")
	}
}

func TestParameterizedIntStrEncoding(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"3", `3`},
		{"0", `0`},
		{"${{ ENV.MAX }}", `"${{ ENV.MAX }}"`},
		{"not-a-number", `"not-a-number"`},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			raw := parameterizedIntStr(types.StringValue(tc.in))
			if raw == nil {
				t.Fatal("got nil")
			}
			if string(*raw) != tc.want {
				t.Errorf("encoded %q as %s, want %s", tc.in, string(*raw), tc.want)
			}
			if back := tfParameterizedIntStr(raw); back.ValueString() != tc.in {
				t.Errorf("decoded back to %q, want %q", back.ValueString(), tc.in)
			}
		})
	}

	if parameterizedIntStr(types.StringNull()) != nil {
		t.Error("a null value should encode to nil, not a JSON null")
	}
}

func TestInvalidJSONIsReportedAgainstItsAttribute(t *testing.T) {
	def := &definitionTF{
		Version: types.StringValue("v1"),
		Name:    types.StringValue("bad_json"),
		Tasks: map[string]taskTF{
			"t": {
				Integration:    types.StringValue("SNOWFLAKE"),
				IntegrationJob: types.StringValue("SNOWFLAKE_RUN_QUERY"),
				Parameters:     jsontypes.NewNormalizedValue(`["not","an","object"]`),
			},
		},
	}

	var diags diag.Diagnostics
	definitionToWire(def, path.Root("data"), &diags)

	if !diags.HasError() {
		t.Fatal("expected an error for a non-object `parameters` value")
	}
	want := path.Root("data").AtName("tasks").AtMapKey("t").AtName("parameters")
	if got := diags.Errors()[0].(diag.DiagnosticWithPath).Path(); !got.Equal(want) {
		t.Errorf("error reported at %s, want %s", got, want)
	}
}

// TestEmptyParametersStillSent guards the API's requirement that `parameters`
// is present even when a task takes none.
func TestEmptyParametersStillSent(t *testing.T) {
	def := &definitionTF{
		Version: types.StringValue("v1"),
		Name:    types.StringValue("no_params"),
		Tasks: map[string]taskTF{
			"t": {
				Integration:    types.StringValue("SNOWFLAKE"),
				IntegrationJob: types.StringValue("SNOWFLAKE_RUN_QUERY"),
				Parameters:     jsontypes.NewNormalizedNull(),
			},
		},
	}

	var diags diag.Diagnostics
	wire := definitionToWire(def, path.Root("data"), &diags)
	if diags.HasError() {
		t.Fatalf("definitionToWire: %v", diags.Errors())
	}

	encoded, err := json.Marshal(wire.Pipeline["t"])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var task map[string]any
	if err := json.Unmarshal(encoded, &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	params, ok := task["parameters"]
	if !ok {
		t.Fatal("`parameters` was omitted; the API requires it")
	}
	if m, ok := params.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("parameters = %#v, want an empty object", params)
	}
}

// TestFolderPatternMatchesWhatTheAPIStores pins the folders the API keeps as
// written. One it would rewrite fails the apply, so it is rejected at plan.
func TestFolderPatternMatchesWhatTheAPIStores(t *testing.T) {
	for folder, want := range map[string]bool{
		"":                 true,
		"finance":          true,
		"finance/daily":    true,
		"finance ops/2026": true,
		"/finance":         false,
		"finance/":         false,
		"finance//daily":   false,
		" finance":         false,
		"finance /daily":   false,
		"finance/ daily":   false,
	} {
		if got := folderPattern.MatchString(folder); got != want {
			t.Errorf("%q: matched %v, want %v", folder, got, want)
		}
	}
}
