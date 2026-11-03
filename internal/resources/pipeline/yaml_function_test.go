package pipeline

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func decodeYAML(t *testing.T, doc string) (map[string]any, error) {
	t.Helper()
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	return yamlToData(parsed)
}

func TestYAMLToData(t *testing.T) {
	data, err := decodeYAML(t, `
name: Daily
meta: {owner: data}
pipeline:
  ingest:
    integration: FIVETRAN
    integration_job: FIVETRAN_SYNC_ALL
    parameters: {connector_id: abc}
  transform:
    depends_on: [ingest]
    matrix: {inputs: {region: [eu, us]}}
    tasks:
      dbt:
        integration: DBT_CORE
        integration_job: DBT_CORE_EXECUTE
        parameters: {commands: dbt build}
        alerts:
          - statuses: [FAILED]
            destinations:
              - integration: DATADOG
                parameters: {priority: 5}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := data["pipeline"]; ok {
		t.Error("pipeline was not split into tasks and task_groups")
	}
	task := data["tasks"].(map[string]any)["ingest"].(map[string]any)
	if task["parameters"] != `{"connector_id":"abc"}` {
		t.Errorf("task parameters = %#v, want JSON", task["parameters"])
	}
	if data["meta"] != `{"owner":"data"}` {
		t.Errorf("meta = %#v, want JSON", data["meta"])
	}
	group := data["task_groups"].(map[string]any)["transform"].(map[string]any)
	if got := group["matrix"].(map[string]any)["inputs"]; got != `{"region":["eu","us"]}` {
		t.Errorf("matrix inputs = %#v, want JSON", got)
	}
	dbt := group["tasks"].(map[string]any)["dbt"].(map[string]any)
	if dbt["parameters"] != `{"commands":"dbt build"}` {
		t.Errorf("grouped task parameters = %#v, want JSON", dbt["parameters"])
	}
	dest := dbt["alerts"].([]any)[0].(map[string]any)["destinations"].([]any)[0].(map[string]any)
	if dest["priority"] != 5 || dest["parameters"] != nil {
		t.Errorf("destination = %#v, want parameters flattened into it", dest)
	}
	if _, err := toTerraform(data); err != nil {
		t.Errorf("toTerraform: %v", err)
	}
}

func TestYAMLToDataRejectsUnknownKeys(t *testing.T) {
	_, err := decodeYAML(t, `
name: Daily
pipeline:
  transform:
    depend_on: [ingest]
    tasks: {}
`)
	if err == nil || !strings.Contains(err.Error(), "pipeline.transform.depend_on") {
		t.Errorf("err = %v, want one naming pipeline.transform.depend_on", err)
	}
}
