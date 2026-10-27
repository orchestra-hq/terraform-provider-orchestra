package apicontract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every (method, path) the provider calls. Keep this list in step with the
// client call sites; the test below asserts each one exists in the spec.
//
// This exists because the provider once called PUT /pipelines/{alias},
// DELETE /pipelines/{alias} and GET /pipelines?alias=... — none of which the
// API has. Two failed loudly; the third silently returned the wrong pipeline.
type call struct{ method, path, who string }

var calls = []call{
	{"GET", "/pipeline", "pipeline resource: read"},
	{"GET", "/pipelines/data", "pipeline resource: read definition"},
	{"POST", "/pipelines", "pipeline resource: create"},
	{"PUT", "/pipelines", "pipeline resource: update"},
	{"DELETE", "/pipelines", "pipeline resource: delete"},
	{"POST", "/pipelines/schema", "pipeline resource: plan-time validation"},
	{"PUT", "/pipelines/{pipeline_id_or_alias}/pause", "pipeline resource: pause"},
	{"PUT", "/pipelines/{pipeline_id_or_alias}/folder", "pipeline resource: move to a folder"},
	{"GET", "/pipelines", "pipelines data source"},
	{"GET", "/assets", "assets data source"},
	{"GET", "/compute_resources/azure/federated_credentials", "azure federated credentials data source"},
	{"GET", "/environments", "environments data source"},
	{"POST", "/environments", "environment resource: create"},
	{"GET", "/environments/{environment_id}", "environment resource: read"},
	{"PATCH", "/environments/{environment_id}", "environment resource: update"},
	{"DELETE", "/environments/{environment_id}", "environment resource: delete"},
	{"GET", "/integrations/connections", "integration connections data source"},
	{"POST", "/integrations/{integration}/auth", "integration connection resource: create"},
	{"GET", "/integrations/{integration}/auth/{connection_id}", "integration connection resource: read"},
	{"PATCH", "/integrations/{integration}/auth/{connection_id}", "integration connection resource: update"},
	{"DELETE", "/integrations/{integration}/auth/{connection_id}", "integration connection resource: delete, and undo an OAuth create"},
	{"GET", "/monitors/{monitor_id}", "monitor resource: read"},
	{"POST", "/monitors", "monitor resource: create"},
	{"PUT", "/monitors/{monitor_id}", "monitor resource: update"},
	{"DELETE", "/monitors/{monitor_id}", "monitor resource: delete"},
	{"GET", "/incident_settings", "incident settings resource: read"},
	{"PATCH", "/incident_settings", "incident settings resource: create and update"},
	{"GET", "/monitors", "monitors data source; monitor order resource: read"},
	{"POST", "/monitors/reorder", "monitor order resource: create and update"},
}

// agentsCalls is the same list for the Agents API, which code under
// internal/agents/ calls.
var agentsCalls = []call{
	{"POST", "/agents", "agent resource: create"},
	{"GET", "/agents/{agent_id}", "agent resource: read"},
	{"PATCH", "/agents/{agent_id}", "agent resource: update"},
	{"DELETE", "/agents/{agent_id}", "agent resource: delete"},
	{"GET", "/agents/{agent_id}/skills", "agent resource: read skills"},
	{"PUT", "/agents/{agent_id}/skills", "agent resource: set skills"},
	{"GET", "/agents/{agent_id}/integrations", "agent resource: read integrations"},
	{"PUT", "/agents/{agent_id}/integrations", "agent resource: set integrations"},
	{"POST", "/skills", "skill resource: create"},
	{"POST", "/skills/import", "skill resource: create from git"},
	{"GET", "/skills/{skill_id}", "skill resource: read"},
	{"PATCH", "/skills/{skill_id}", "skill resource: update"},
	{"DELETE", "/skills/{skill_id}", "skill resource: delete"},
	{"GET", "/skills", "skills data source"},
}

func callsFor(s apiSpec) []call {
	if s == agentsAPI {
		return agentsCalls
	}
	return calls
}

// Query parameters the provider sends, by endpoint. A parameter the endpoint
// does not declare is silently ignored by the API, which is how the bad
// GET /pipelines?alias=... read went unnoticed.
var queryParams = map[string][]string{
	"GET /pipeline":                 {"pipeline_id", "alias", "repository", "yaml_path", "version", "branch", "commit"},
	"GET /pipelines/data":           {"pipeline_id", "alias", "repository", "yaml_path", "version", "branch", "commit"},
	"GET /pipelines":                {"page", "page_size", "name"},
	"DELETE /pipelines":             {"pipeline_id", "alias", "repository", "yaml_path"},
	"GET /integrations/connections": {"integration", "authStatus"},
	"GET /assets": {
		"asset_type", "integration", "integration_account_id", "workspace_id",
		"database_name", "schema_name", "status", "asset_ids",
		"sort_column", "sort_direction", "page", "page_size",
	},
}

// agentsQueryParams is the same for the Agents API. ListAll adds page and
// page_size to every list call.
var agentsQueryParams = map[string][]string{
	"GET /skills":                         {"search", "orchestra_managed", "page", "page_size"},
	"GET /agents/{agent_id}/skills":       {"page", "page_size"},
	"GET /agents/{agent_id}/integrations": {"page", "page_size"},
}

func queryParamsFor(s apiSpec) map[string][]string {
	if s == agentsAPI {
		return agentsQueryParams
	}
	return queryParams
}

type operation struct {
	Parameters []struct {
		Name string `yaml:"name"`
		In   string `yaml:"in"`
	} `yaml:"parameters"`
}

// pathItem holds the operations on one path. Not every key under a path is an
// HTTP method — OpenAPI allows `servers`, `parameters`, `summary` and more as
// siblings, and the spec does use them (`/accounts` carries its own `servers`
// list). Decoding into a plain map of operations fails on those, so the methods
// are picked out by name.
type pathItem map[string]operation

func (p *pathItem) UnmarshalYAML(value *yaml.Node) error {
	methods := map[string]bool{
		"get": true, "put": true, "post": true, "delete": true,
		"options": true, "head": true, "patch": true, "trace": true,
	}
	out := pathItem{}
	// A mapping node alternates key, value, key, value.
	for i := 0; i+1 < len(value.Content); i += 2 {
		name := value.Content[i].Value
		if !methods[strings.ToLower(name)] {
			continue
		}
		var op operation
		if err := value.Content[i+1].Decode(&op); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		out[strings.ToLower(name)] = op
	}
	*p = out
	return nil
}

type openAPI struct {
	Paths      map[string]pathItem `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Enum []string `yaml:"enum"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

// apiSpec is one OpenAPI spec the provider calls. name is its key in
// coverage.yaml; file is its committed copy, from the repository root.
type apiSpec struct{ name, file string }

var (
	engineAPI = apiSpec{"engine", "openapi.yaml"}
	agentsAPI = apiSpec{"agents", "openapi-agents.yaml"}
	apiSpecs  = []apiSpec{engineAPI, agentsAPI}
)

// owns reports whether a provider source file calls this spec. The Agents API
// has its own base URL, so its code lives apart, under internal/agents/;
// everything else calls the Engine API.
func (s apiSpec) owns(file string) bool {
	return strings.Contains(filepath.ToSlash(file), "internal/agents/") == (s == agentsAPI)
}

func loadSpec(t *testing.T, s apiSpec) openAPI {
	t.Helper()

	specPath := filepath.Join("..", "..", s.file)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v (fetch it with: make openapi)", specPath, err)
	}

	var spec openAPI
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	if len(spec.Paths) == 0 {
		t.Fatalf("%s declares no paths", specPath)
	}
	return spec
}

func TestEveryCalledEndpointExists(t *testing.T) {
	for _, s := range apiSpecs {
		spec := loadSpec(t, s)
		for _, call := range callsFor(s) {
			checkCallExists(t, s, spec, call)
		}
	}
}

func checkCallExists(t *testing.T, s apiSpec, spec openAPI, call call) {
	{
		t.Run(s.name+": "+call.method+" "+call.path, func(t *testing.T) {
			methods, ok := spec.Paths[call.path]
			if !ok {
				t.Fatalf("%s calls %s %s, but the spec has no such path", call.who, call.method, call.path)
			}
			if _, ok := methods[strings.ToLower(call.method)]; !ok {
				declared := make([]string, 0, len(methods))
				for m := range methods {
					declared = append(declared, strings.ToUpper(m))
				}
				sort.Strings(declared)
				t.Fatalf("%s calls %s %s, but the spec declares only %v on that path",
					call.who, call.method, call.path, declared)
			}
		})
	}
}

func TestEverySentQueryParameterIsDeclared(t *testing.T) {
	for _, s := range apiSpecs {
		spec := loadSpec(t, s)
		for endpoint, sent := range queryParamsFor(s) {
			checkParamsDeclared(t, s, spec, endpoint, sent)
		}
	}
}

func checkParamsDeclared(t *testing.T, s apiSpec, spec openAPI, endpoint string, sent []string) {
	{
		t.Run(s.name+": "+endpoint, func(t *testing.T) {
			method, p, ok := strings.Cut(endpoint, " ")
			if !ok {
				t.Fatalf("malformed endpoint key %q", endpoint)
			}

			op, ok := spec.Paths[p][strings.ToLower(method)]
			if !ok {
				t.Fatalf("spec has no %s %s", method, p)
			}

			declared := map[string]bool{}
			for _, param := range op.Parameters {
				if param.In == "query" {
					declared[param.Name] = true
				}
			}

			for _, name := range sent {
				if !declared[name] {
					t.Errorf("provider sends ?%s to %s, which the spec does not declare — the API will ignore it", name, endpoint)
				}
			}
		})
	}
}

// TestGeneratedEnumsMatchSpec guards the generated validator lists against
// drift. The assets `integration` validator was once 13 values behind, which
// rejected valid configuration at plan time.
func TestGeneratedEnumsMatchSpec(t *testing.T) {
	spec := loadSpec(t, engineAPI)

	raw, err := os.ReadFile(filepath.Join("..", "enums", "enums_gen.go"))
	if err != nil {
		t.Fatalf("read generated enums: %v", err)
	}
	generated := string(raw)

	blockRe := regexp.MustCompile(`(?s)// (\w+) is components\.schemas\.(\w+)\.\nvar \w+ = \[\]string\{(.*?)\n\}`)
	valueRe := regexp.MustCompile(`"([^"]+)"`)

	matches := blockRe.FindAllStringSubmatch(generated, -1)
	if len(matches) == 0 {
		t.Fatal("no enum blocks found in internal/enums/enums_gen.go")
	}

	for _, m := range matches {
		goName, schemaName, body := m[1], m[2], m[3]

		t.Run(goName, func(t *testing.T) {
			schema, ok := spec.Components.Schemas[schemaName]
			if !ok {
				t.Fatalf("spec has no schema %s", schemaName)
			}

			var got []string
			for _, v := range valueRe.FindAllStringSubmatch(body, -1) {
				got = append(got, v[1])
			}

			want := append([]string(nil), schema.Enum...)
			sort.Strings(want)
			sort.Strings(got)

			if strings.Join(want, ",") != strings.Join(got, ",") {
				t.Errorf("%s is out of date with components.schemas.%s.\nRegenerate with: python3 scripts/generate_enums.py\nmissing: %v\nextra: %v",
					goName, schemaName, difference(want, got), difference(got, want))
			}
		})
	}
}

func difference(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, v := range b {
		inB[v] = true
	}
	var out []string
	for _, v := range a {
		if !inB[v] {
			out = append(out, v)
		}
	}
	return out
}

// specParams returns the query parameters the spec declares for an operation.
func specParams(spec openAPI, method, path string) []string {
	op, ok := spec.Paths[path][strings.ToLower(method)]
	if !ok {
		return nil
	}
	var out []string
	for _, p := range op.Parameters {
		if p.In == "query" {
			out = append(out, p.Name)
		}
	}
	return out
}
