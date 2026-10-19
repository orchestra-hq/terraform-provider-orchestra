package pipeline

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/alerts"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/tfconv"
)

// ---------- scalar helpers ----------

func strPtrOrNil(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func boolPtrOrNil(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func int64PtrOrNil(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

func tfString(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}

func tfBool(v *bool) types.Bool {
	if v == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*v)
}

func tfInt64(v *int64) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*v)
}

// ---------- free-form JSON helpers ----------

// jsonObject validates that a normalized JSON string is an object and returns
// it verbatim. The bytes go straight to the API: decoding into map[string]any
// would turn every number into a float64 and silently round integers above
// 2^53, which is the range warehouse job IDs and Snowflake IDs live in.
func jsonObject(v jsontypes.Normalized, p path.Path, diags *diag.Diagnostics) json.RawMessage {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(v.ValueString()), &probe); err != nil {
		diags.AddAttributeError(p, "Invalid JSON object", fmt.Sprintf("Expected a JSON object: %s", err))
		return nil
	}
	return json.RawMessage(v.ValueString())
}

// jsonAny is jsonObject without the object requirement.
func jsonAny(v jsontypes.Normalized, p path.Path, diags *diag.Diagnostics) json.RawMessage {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	if !json.Valid([]byte(v.ValueString())) {
		diags.AddAttributeError(p, "Invalid JSON", "Expected a JSON value.")
		return nil
	}
	return json.RawMessage(v.ValueString())
}

// tfRawJSON is the inverse of jsonObject/jsonAny.
func tfRawJSON(raw json.RawMessage) jsontypes.Normalized {
	if len(raw) == 0 || string(raw) == "null" {
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(raw))
}

// parameterizedIntStr encodes ParameterizedIntStr: a bare integer when the
// string parses as one, otherwise a JSON string (a ${{ }} template).
func parameterizedIntStr(v types.String) *json.RawMessage {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	var raw json.RawMessage
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		raw = json.RawMessage(strconv.FormatInt(n, 10))
	} else {
		b, err := json.Marshal(s)
		if err != nil {
			return nil
		}
		raw = json.RawMessage(b)
	}
	return &raw
}

// tfParameterizedIntStr is the inverse: both encodings come back as a string.
func tfParameterizedIntStr(raw *json.RawMessage) types.String {
	if raw == nil || len(*raw) == 0 || string(*raw) == "null" {
		return types.StringNull()
	}
	var s string
	if err := json.Unmarshal(*raw, &s); err == nil {
		return types.StringValue(s)
	}
	var n int64
	if err := json.Unmarshal(*raw, &n); err == nil {
		return types.StringValue(strconv.FormatInt(n, 10))
	}
	return types.StringValue(string(*raw))
}

// ---------- TF -> wire ----------

func definitionToWire(def *definitionTF, root path.Path, diags *diag.Diagnostics) *wireDefinition {
	if def == nil {
		return nil
	}

	out := &wireDefinition{
		Version:  def.Version.ValueString(),
		Name:     def.Name.ValueString(),
		Pipeline: map[string]json.RawMessage{},
		Meta:     jsonObject(def.Meta, root.AtName("meta"), diags),
	}
	if out.Version == "" {
		out.Version = "v1"
	}

	// The API's single `pipeline` object is the union of our two maps. Duplicate
	// keys are rejected in ValidateConfig; guard here too so a collision can
	// never silently drop an entry.
	for _, id := range sortedKeys(def.TaskGroups) {
		p := root.AtName("task_groups").AtMapKey(id)
		group := taskGroupToWire(def.TaskGroups[id], p, diags)
		b, err := json.Marshal(group)
		if err != nil {
			diags.AddAttributeError(p, "Cannot encode task group", err.Error())
			continue
		}
		out.Pipeline[id] = b
	}
	for _, id := range sortedKeys(def.Tasks) {
		p := root.AtName("tasks").AtMapKey(id)
		if _, clash := out.Pipeline[id]; clash {
			diags.AddAttributeError(p, "Duplicate pipeline entry ID",
				fmt.Sprintf("%q is used by both `task_groups` and `tasks`. The API merges them into one object, so IDs must be unique across both.", id))
			continue
		}
		task := taskToWire(def.Tasks[id], p, diags)
		b, err := json.Marshal(task)
		if err != nil {
			diags.AddAttributeError(p, "Cannot encode task", err.Error())
			continue
		}
		out.Pipeline[id] = b
	}

	for i, s := range def.Schedule {
		p := root.AtName("schedule").AtListIndex(i)
		out.Schedule = append(out.Schedule, wireSchedule{
			Name:        strPtrOrNil(s.Name),
			Cron:        s.Cron.ValueString(),
			Timezone:    strPtrOrNil(s.Timezone),
			Environment: strPtrOrNil(s.Environment),
			Branch:      strPtrOrNil(s.Branch),
			Exclude:     tfconv.GoStrings(s.Exclude),
			RunInputs:   jsonObject(s.RunInputs, p.AtName("run_inputs"), diags),
		})
	}

	if len(def.Sensors) > 0 {
		out.Sensors = map[string]wireSensor{}
		for _, id := range sortedKeys(def.Sensors) {
			out.Sensors[id] = sensorToWire(def.Sensors[id], root.AtName("sensors").AtMapKey(id), diags)
		}
	}

	for i, t := range def.TriggerEvents {
		p := root.AtName("trigger_events").AtListIndex(i)
		typ := t.Type.ValueString()
		if typ == "" {
			typ = "pipeline"
		}
		out.TriggerEvents = append(out.TriggerEvents, wireTriggerEvent{
			Type:       typ,
			PipelineID: t.PipelineID.ValueString(),
			Statuses:   tfconv.GoStrings(t.Statuses),
			RunInputs:  jsonObject(t.RunInputs, p.AtName("run_inputs"), diags),
		})
	}

	if def.Webhook != nil {
		p := root.AtName("webhook")
		out.Webhook = &wireWebhook{
			Enabled:           def.Webhook.Enabled.ValueBool(),
			OperationMetadata: operationMetadataToWire(def.Webhook.OperationMetadata),
			RunInputs:         jsonObject(def.Webhook.RunInputs, p.AtName("run_inputs"), diags),
		}
	}

	out.Configuration = configurationToWire(def.Configuration)

	if len(def.Inputs) > 0 {
		out.Inputs = map[string]wireInput{}
		for _, name := range sortedKeys(def.Inputs) {
			in := def.Inputs[name]
			out.Inputs[name] = wireInput{
				Type:     in.Type.ValueString(),
				Default:  jsonAny(in.Default, root.AtName("inputs").AtMapKey(name).AtName("default"), diags),
				Optional: boolPtrOrNil(in.Optional),
			}
		}
	}

	out.Alerts = alertsToWire(def.Alerts, root.AtName("alerts"), diags)
	for _, a := range def.Anomalies {
		out.Anomalies = append(out.Anomalies, wireAnomaly{
			Type:                    a.Type.ValueString(),
			Destinations:            alerts.ToWire(a.Destinations, root.AtName("anomalies"), diags),
			MinBaselineSeconds:      int64PtrOrNil(a.MinBaselineSeconds),
			PercentageAboveBaseline: int64PtrOrNil(a.PercentageAboveBaseline),
			PercentageBelowBaseline: int64PtrOrNil(a.PercentageBelowBaseline),
		})
	}

	return out
}

func taskToWire(t taskTF, p path.Path, diags *diag.Diagnostics) wireTask {
	out := wireTask{
		Integration:           t.Integration.ValueString(),
		IntegrationJob:        t.IntegrationJob.ValueString(),
		Parameters:            jsonObject(t.Parameters, p.AtName("parameters"), diags),
		DependsOn:             tfconv.GoStrings(t.DependsOn),
		Condition:             strPtrOrNil(t.Condition),
		Name:                  t.Name.ValueString(),
		Tags:                  tfconv.GoStrings(t.Tags),
		Connection:            strPtrOrNil(t.Connection),
		OperationMetadata:     operationMetadataToWire(t.OperationMetadata),
		TreatFailureAsWarning: boolPtrOrNil(t.TreatFailureAsWarning),
		Configuration:         configurationToWire(t.Configuration),
		Meta:                  jsonObject(t.Meta, p.AtName("meta"), diags),
		Alerts:                alertsToWire(t.Alerts, p.AtName("alerts"), diags),
		Paused:                boolPtrOrNil(t.Paused),
	}
	if out.Parameters == nil {
		// `parameters` is required by the API even when empty.
		out.Parameters = json.RawMessage(`{}`)
	}
	for _, a := range t.Anomalies {
		out.Anomalies = append(out.Anomalies, wireAnomaly{
			Type:                    a.Type.ValueString(),
			Destinations:            alerts.ToWire(a.Destinations, p.AtName("anomalies"), diags),
			MinBaselineSeconds:      int64PtrOrNil(a.MinBaselineSeconds),
			PercentageAboveBaseline: int64PtrOrNil(a.PercentageAboveBaseline),
			PercentageBelowBaseline: int64PtrOrNil(a.PercentageBelowBaseline),
			OperationTypes:          tfconv.GoStrings(a.OperationTypes),
			OperationNames:          tfconv.GoStrings(a.OperationNames),
			MaxAnomalies:            int64PtrOrNil(a.MaxAnomalies),
		})
	}
	return out
}

func taskGroupToWire(g taskGroupTF, p path.Path, diags *diag.Diagnostics) wireTaskGroup {
	out := wireTaskGroup{
		Tasks:     map[string]wireTask{},
		DependsOn: tfconv.GoStrings(g.DependsOn),
		Condition: strPtrOrNil(g.Condition),
		Name:      strPtrOrNil(g.Name),
		Meta:      jsonObject(g.Meta, p.AtName("meta"), diags),
		Paused:    boolPtrOrNil(g.Paused),
	}
	for _, id := range sortedKeys(g.Tasks) {
		out.Tasks[id] = taskToWire(g.Tasks[id], p.AtName("tasks").AtMapKey(id), diags)
	}
	if g.Matrix != nil {
		mp := p.AtName("matrix")
		out.Matrix = &wireMatrix{
			Inputs:          jsonObject(g.Matrix.Inputs, mp.AtName("inputs"), diags),
			MaxParallel:     int64PtrOrNil(g.Matrix.MaxParallel),
			Sequential:      boolPtrOrNil(g.Matrix.Sequential),
			ContinueOnError: boolPtrOrNil(g.Matrix.ContinueOnError),
		}
	}
	return out
}

func sensorToWire(s sensorTF, p path.Path, diags *diag.Diagnostics) wireSensor {
	out := wireSensor{
		Name:          s.Name.ValueString(),
		Cron:          s.Cron.ValueString(),
		Timezone:      s.Timezone.ValueString(),
		TimeoutMins:   s.TimeoutMins.ValueInt64(),
		Environments:  tfconv.GoStrings(s.Environments),
		Checks:        map[string]wireSensorCheck{},
		Condition:     strPtrOrNil(s.Condition),
		Exclude:       tfconv.GoStrings(s.Exclude),
		FrequencySecs: int64PtrOrNil(s.FrequencySecs),
		RunInputs:     jsonObject(s.RunInputs, p.AtName("run_inputs"), diags),
		Alerts:        alertsToWire(s.Alerts, p.AtName("alerts"), diags),
	}
	for _, id := range sortedKeys(s.Checks) {
		c := s.Checks[id]
		cp := p.AtName("checks").AtMapKey(id)
		params := jsonObject(c.Parameters, cp.AtName("parameters"), diags)
		if params == nil {
			params = json.RawMessage(`{}`)
		}
		out.Checks[id] = wireSensorCheck{
			Integration: c.Integration.ValueString(),
			Connection:  strPtrOrNil(c.Connection),
			SensorType:  c.SensorType.ValueString(),
			Parameters:  params,
			MapOutputs:  tfconv.GoStringMap(c.MapOutputs),
		}
	}
	return out
}

func operationMetadataToWire(m map[string]operationMetadataTF) map[string]wireOperationMetadata {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]wireOperationMetadata, len(m))
	for k, v := range m {
		out[k] = wireOperationMetadata{
			Integration:            v.Integration.ValueString(),
			Connection:             strPtrOrNil(v.Connection),
			S3BucketName:           strPtrOrNil(v.S3BucketName),
			S3KeyPrefix:            strPtrOrNil(v.S3KeyPrefix),
			DbtWarehouseIdentifier: strPtrOrNil(v.DbtWarehouseIdentifier),
		}
	}
	return out
}

func configurationToWire(c *configurationTF) *wireConfiguration {
	if c == nil {
		return nil
	}
	out := &wireConfiguration{
		Retries:    int64PtrOrNil(c.Retries),
		RetryDelay: int64PtrOrNil(c.RetryDelay),
		Timeout:    int64PtrOrNil(c.Timeout),
	}
	if c.Concurrency != nil {
		out.Concurrency = &wireConcurrency{
			MaxActive: parameterizedIntStr(c.Concurrency.MaxActive),
			OnLimit:   tfconv.StringPtrOrNil(c.Concurrency.OnLimit),
		}
	}
	return out
}

func alertsToWire(list []alertTF, p path.Path, diags *diag.Diagnostics) []wireAlert {
	if len(list) == 0 {
		return nil
	}
	out := make([]wireAlert, 0, len(list))
	for i, a := range list {
		out = append(out, wireAlert{
			Name:          a.Name.ValueString(),
			Statuses:      tfconv.GoStrings(a.Statuses),
			Destinations:  alerts.ToWire(a.Destinations, p.AtListIndex(i).AtName("destinations"), diags),
			CustomMessage: strPtrOrNil(a.CustomMessage),
			MaxLiveAlerts: parameterizedIntStr(a.MaxLiveAlerts),
		})
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------- wire -> TF ----------

func definitionFromWire(w *wireDefinition) (*definitionTF, error) {
	if w == nil {
		return nil, nil
	}

	out := &definitionTF{
		Version: types.StringValue(w.Version),
		Name:    types.StringValue(w.Name),
		Meta:    tfRawJSON(w.Meta),
	}

	for id, raw := range w.Pipeline {
		// A group is distinguishable from a task by its `tasks` key; the API's
		// union has no discriminator.
		var probe struct {
			Tasks map[string]json.RawMessage `json:"tasks"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, fmt.Errorf("decode pipeline entry %q: %w", id, err)
		}

		if probe.Tasks != nil {
			var g wireTaskGroup
			if err := json.Unmarshal(raw, &g); err != nil {
				return nil, fmt.Errorf("decode task group %q: %w", id, err)
			}
			if out.TaskGroups == nil {
				out.TaskGroups = map[string]taskGroupTF{}
			}
			out.TaskGroups[id] = taskGroupFromWire(g)
			continue
		}

		var t wireTask
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("decode task %q: %w", id, err)
		}
		if out.Tasks == nil {
			out.Tasks = map[string]taskTF{}
		}
		out.Tasks[id] = taskFromWire(t)
	}

	for _, s := range w.Schedule {
		out.Schedule = append(out.Schedule, scheduleTF{
			Name:        tfString(s.Name),
			Cron:        types.StringValue(s.Cron),
			Timezone:    tfString(s.Timezone),
			Environment: tfString(s.Environment),
			Branch:      tfString(s.Branch),
			Exclude:     tfconv.Strings(s.Exclude),
			RunInputs:   tfRawJSON(s.RunInputs),
		})
	}

	if len(w.Sensors) > 0 {
		out.Sensors = map[string]sensorTF{}
		for id, s := range w.Sensors {
			out.Sensors[id] = sensorFromWire(s)
		}
	}

	for _, t := range w.TriggerEvents {
		out.TriggerEvents = append(out.TriggerEvents, triggerEventTF{
			Type:       types.StringValue(t.Type),
			PipelineID: types.StringValue(t.PipelineID),
			Statuses:   tfconv.Strings(t.Statuses),
			RunInputs:  tfRawJSON(t.RunInputs),
		})
	}

	if w.Webhook != nil {
		out.Webhook = &webhookTF{
			Enabled:           types.BoolValue(w.Webhook.Enabled),
			OperationMetadata: operationMetadataFromWire(w.Webhook.OperationMetadata),
			RunInputs:         tfRawJSON(w.Webhook.RunInputs),
		}
	}

	out.Configuration = configurationFromWire(w.Configuration)

	if len(w.Inputs) > 0 {
		out.Inputs = map[string]inputTF{}
		for name, in := range w.Inputs {
			out.Inputs[name] = inputTF{
				Type:     types.StringValue(in.Type),
				Default:  tfRawJSON(in.Default),
				Optional: tfBool(in.Optional),
			}
		}
	}

	out.Alerts = alertsFromWire(w.Alerts)
	for _, a := range w.Anomalies {
		out.Anomalies = append(out.Anomalies, anomalyTF{
			Type:                    types.StringValue(a.Type),
			Destinations:            alerts.FromWire(a.Destinations),
			MinBaselineSeconds:      tfInt64(a.MinBaselineSeconds),
			PercentageAboveBaseline: tfInt64(a.PercentageAboveBaseline),
			PercentageBelowBaseline: tfInt64(a.PercentageBelowBaseline),
		})
	}

	return out, nil
}

func taskFromWire(t wireTask) taskTF {
	out := taskTF{
		Integration:           types.StringValue(t.Integration),
		IntegrationJob:        types.StringValue(t.IntegrationJob),
		Parameters:            tfRawJSON(t.Parameters),
		DependsOn:             tfconv.Strings(t.DependsOn),
		Condition:             tfString(t.Condition),
		Tags:                  tfconv.Strings(t.Tags),
		Connection:            tfString(t.Connection),
		OperationMetadata:     operationMetadataFromWire(t.OperationMetadata),
		TreatFailureAsWarning: tfBool(t.TreatFailureAsWarning),
		Configuration:         configurationFromWire(t.Configuration),
		Meta:                  tfRawJSON(t.Meta),
		Alerts:                alertsFromWire(t.Alerts),
		Paused:                tfBool(t.Paused),
	}
	out.Name = nameFromWire(&t.Name)
	for _, a := range t.Anomalies {
		out.Anomalies = append(out.Anomalies, taskAnomalyTF{
			Type:                    types.StringValue(a.Type),
			Destinations:            alerts.FromWire(a.Destinations),
			MinBaselineSeconds:      tfInt64(a.MinBaselineSeconds),
			PercentageAboveBaseline: tfInt64(a.PercentageAboveBaseline),
			PercentageBelowBaseline: tfInt64(a.PercentageBelowBaseline),
			OperationTypes:          tfconv.Strings(a.OperationTypes),
			OperationNames:          tfconv.Strings(a.OperationNames),
			MaxAnomalies:            tfInt64(a.MaxAnomalies),
		})
	}
	return out
}

func taskGroupFromWire(g wireTaskGroup) taskGroupTF {
	out := taskGroupTF{
		Tasks:     map[string]taskTF{},
		DependsOn: tfconv.Strings(g.DependsOn),
		Condition: tfString(g.Condition),
		Name:      nameFromWire(g.Name),
		Meta:      tfRawJSON(g.Meta),
		Paused:    tfBool(g.Paused),
	}
	for id, t := range g.Tasks {
		out.Tasks[id] = taskFromWire(t)
	}
	if g.Matrix != nil {
		out.Matrix = &matrixTF{
			Inputs:          tfRawJSON(g.Matrix.Inputs),
			MaxParallel:     tfInt64(g.Matrix.MaxParallel),
			Sequential:      tfBool(g.Matrix.Sequential),
			ContinueOnError: tfBool(g.Matrix.ContinueOnError),
		}
	}
	return out
}

func sensorFromWire(s wireSensor) sensorTF {
	out := sensorTF{
		Name:          types.StringValue(s.Name),
		Cron:          types.StringValue(s.Cron),
		Timezone:      types.StringValue(s.Timezone),
		TimeoutMins:   types.Int64Value(s.TimeoutMins),
		Environments:  tfconv.Strings(s.Environments),
		Checks:        map[string]sensorCheckTF{},
		Condition:     tfString(s.Condition),
		Exclude:       tfconv.Strings(s.Exclude),
		FrequencySecs: tfInt64(s.FrequencySecs),
		RunInputs:     tfRawJSON(s.RunInputs),
		Alerts:        alertsFromWire(s.Alerts),
	}
	for id, c := range s.Checks {
		out.Checks[id] = sensorCheckTF{
			Integration: types.StringValue(c.Integration),
			Connection:  tfString(c.Connection),
			SensorType:  types.StringValue(c.SensorType),
			Parameters:  tfRawJSON(c.Parameters),
			MapOutputs:  tfconv.StringMap(c.MapOutputs),
		}
	}
	return out
}

func operationMetadataFromWire(m map[string]wireOperationMetadata) map[string]operationMetadataTF {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]operationMetadataTF, len(m))
	for k, v := range m {
		out[k] = operationMetadataTF{
			Integration:            types.StringValue(v.Integration),
			Connection:             tfString(v.Connection),
			S3BucketName:           tfString(v.S3BucketName),
			S3KeyPrefix:            tfString(v.S3KeyPrefix),
			DbtWarehouseIdentifier: tfString(v.DbtWarehouseIdentifier),
		}
	}
	return out
}

func configurationFromWire(c *wireConfiguration) *configurationTF {
	if c == nil {
		return nil
	}
	out := &configurationTF{
		Retries:    tfInt64(c.Retries),
		RetryDelay: tfInt64(c.RetryDelay),
		Timeout:    tfInt64(c.Timeout),
	}
	if c.Concurrency != nil {
		out.Concurrency = &concurrencyTF{
			MaxActive: tfParameterizedIntStr(c.Concurrency.MaxActive),
			OnLimit:   tfconv.String(c.Concurrency.OnLimit),
		}
	}
	return out
}

func alertsFromWire(list []wireAlert) []alertTF {
	if len(list) == 0 {
		return nil
	}
	out := make([]alertTF, 0, len(list))
	for _, a := range list {
		out = append(out, alertTF{
			Name:          types.StringValue(a.Name),
			Statuses:      tfconv.Strings(a.Statuses),
			Destinations:  alerts.FromWire(a.Destinations),
			CustomMessage: tfString(a.CustomMessage),
			MaxLiveAlerts: tfParameterizedIntStr(a.MaxLiveAlerts),
		})
	}
	return out
}

// nameFromWire collapses the API's empty-string default back to null.
//
// TaskModel.name and TaskGroupModel.name both default to "" server-side and are
// echoed on every read, so a config that omits them would plan null and apply
// "" — which Terraform rejects as an inconsistent result.
func nameFromWire(name *string) types.String {
	if name == nil || *name == "" {
		return types.StringNull()
	}
	return types.StringValue(*name)
}
