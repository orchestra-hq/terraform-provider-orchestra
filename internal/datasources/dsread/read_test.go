// Package dsread_test drives every data source through the provider server
// against a stub API.
//
// Each data source decodes the whole config into a model whose result field is
// a Go slice or map of structs, and at read time those computed fields are
// null. This checks the framework can decode that, that the response shapes
// the API documents actually unmarshal, and that the results round-trip back
// into state.
package dsread_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/provider"
)

// stubAPI answers each endpoint with a minimal but realistic payload, using
// the property names and response shapes from the Engine spec.
func stubAPI(t *testing.T) *httptest.Server {
	t.Helper()

	paginated := func(results any) map[string]any {
		return map[string]any{"page": 1, "pageSize": 100, "total": 1, "results": results}
	}

	asset := map[string]any{
		"assetId": "11111111-1111-1111-1111-111111111111", "accountId": "22222222-2222-2222-2222-222222222222",
		"assetName": "orders", "assetType": "TABLE", "externalId": "db.schema.orders",
		"integration": "SNOWFLAKE", "integrationAccountId": "acct", "status": "HEALTHY",
		"createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-02T00:00:00Z",
		"owners": []string{"data@example.com"}, "upstreamDependencies": []string{},
		"freshnessConfig": map[string]any{"max_age_hours": 6},
		"meta":            map[string]any{"tier": "gold"},
		"usagePercentage": 12.5, "rowCount": 1000, "bytes": 2048,
	}

	pipeline := map[string]any{
		"id": "33333333-3333-3333-3333-333333333333", "name": "daily_sales",
		"yamlPath": "", "createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-02T00:00:00Z",
		"paused": false, "numTasks": 3, "alias": "daily_sales", "storageProvider": "ORCHESTRA",
		"inputs": map[string]any{
			"run_ingest": map[string]any{"type": "boolean", "default": true, "optional": true},
		},
	}

	environment := map[string]any{
		"accountId":     "22222222-2222-2222-2222-222222222222",
		"environmentId": "44444444-4444-4444-4444-444444444444",
		"name":          "production", "defaultEnv": true,
		"allowedBranches": []string{"main"}, "allowUnpublishedPipelines": false,
		"createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-02T00:00:00Z",
		"values": map[string]any{
			"WAREHOUSE":   map[string]any{"type": "string", "value": "COMPUTE_WH"},
			"BATCH_SIZE":  map[string]any{"type": "int", "value": 500},
			"STRICT_MODE": map[string]any{"type": "bool", "value": true},
		},
	}

	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, body any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encode stub response: %v", err)
		}
	}

	mux.HandleFunc("/assets", func(w http.ResponseWriter, r *http.Request) {
		write(w, paginated([]any{asset}))
	})
	mux.HandleFunc("/pipelines", func(w http.ResponseWriter, r *http.Request) {
		write(w, paginated([]any{pipeline}))
	})
	mux.HandleFunc("/pipeline", func(w http.ResponseWriter, r *http.Request) { write(w, pipeline) })
	mux.HandleFunc("/environments", func(w http.ResponseWriter, r *http.Request) {
		write(w, []any{environment})
	})
	mux.HandleFunc("/environments/", func(w http.ResponseWriter, r *http.Request) { write(w, environment) })
	mux.HandleFunc("/integrations/connections", func(w http.ResponseWriter, r *http.Request) {
		write(w, []any{map[string]any{
			"integration": "SNOWFLAKE", "authStatus": "SUCCEEDED",
			"connectionId": "name_ABCD", "connectionName": "Prod", "isDefault": true,
			"createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-02T00:00:00Z",
		}})
	})
	mux.HandleFunc("/monitors", func(w http.ResponseWriter, r *http.Request) {
		// Out of order on purpose: the data source sorts by `order`.
		write(w, []any{
			map[string]any{"monitorId": "second", "definition": map[string]any{"name": "Late", "enabled": true, "order": 1}},
			map[string]any{"monitorId": "first", "definition": map[string]any{"name": "Warehouse failures", "description": "dbt and Snowflake", "enabled": false, "order": 0}},
		})
	})
	// The Agents API, served from the same stub: base_url is not the Engine
	// API's path, so the provider sends both here.
	mux.HandleFunc("/skills", func(w http.ResponseWriter, r *http.Request) {
		write(w, paginated([]any{map[string]any{
			"skillId": "55555555-5555-5555-5555-555555555555", "name": "identify-long-running-dbt-tasks",
			"description": "Find the slowest dbt tasks", "orchestraManaged": true,
		}}))
	})
	mux.HandleFunc("/compute_resources/azure/federated_credentials", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"subjectId": "orchestra:workspace:abc", "audience": "api://AzureADTokenExchange"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
		http.Error(w, "unhandled", http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// requiredValues supplies a value for each data source's required selector;
// everything else is left null, as it is in a real config.
var requiredValues = map[string]map[string]string{
	"orchestra_environment": {"id": "44444444-4444-4444-4444-444444444444"},
	"orchestra_pipeline":    {"alias": "daily_sales"},
}

func TestDataSourcesRead(t *testing.T) {
	ctx := context.Background()
	srv := stubAPI(t)

	server := providerserver.NewProtocol6(provider.New("test")())()

	schemaResp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema: %v", err)
	}

	configureProvider(t, ctx, server, schemaResp, srv.URL)

	for typeName, dsSchema := range schemaResp.DataSourceSchemas {
		t.Run(typeName, func(t *testing.T) {
			objType := dsSchema.ValueType().(tftypes.Object)

			values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
			for name, attrType := range objType.AttributeTypes {
				if v, ok := requiredValues[typeName][name]; ok {
					values[name] = tftypes.NewValue(attrType, v)
					continue
				}
				values[name] = tftypes.NewValue(attrType, nil)
			}

			config, err := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, values))
			if err != nil {
				t.Fatalf("build config: %v", err)
			}

			resp, err := server.ReadDataSource(ctx, &tfprotov6.ReadDataSourceRequest{
				TypeName: typeName,
				Config:   &config,
			})
			if err != nil {
				t.Fatalf("ReadDataSource: %v", err)
			}
			for _, d := range resp.Diagnostics {
				if d.Severity == tfprotov6.DiagnosticSeverityError {
					t.Errorf("%s: %s: %s", typeName, d.Summary, d.Detail)
				}
			}
			if resp.State == nil {
				t.Fatalf("%s returned no state", typeName)
			}

			state, err := resp.State.Unmarshal(objType)
			if err != nil {
				t.Fatalf("unmarshal state: %v", err)
			}
			assertPopulated(t, typeName, state)
		})
	}
}

// assertPopulated regression-tests the field mappings that were silently
// wrong: `freshnessConfig` and `inputs` were rendered as empty objects that
// could never hold a value, and the Azure credentials response was decoded
// with snake_case tags against a camelCase payload, so `subject_id` always
// came back empty.
func assertPopulated(t *testing.T, typeName string, state tftypes.Value) {
	t.Helper()

	markers := map[string][]string{
		"orchestra_assets":                      {"max_age_hours", "gold", "orders"},
		"orchestra_pipelines":                   {"run_ingest", "daily_sales"},
		"orchestra_pipeline":                    {"run_ingest", "daily_sales"},
		"orchestra_environment":                 {"COMPUTE_WH", "500", "true"},
		"orchestra_environments":                {"production"},
		"orchestra_integration_connections":     {"name_ABCD"},
		"orchestra_monitors":                    {`"id":tftypes.String<"first">`, "Warehouse failures", "dbt and Snowflake"},
		"orchestra_skills":                      {"identify-long-running-dbt-tasks", `"orchestra_managed":tftypes.Bool<"true">`},
		"orchestra_azure_federated_credentials": {"orchestra:workspace:abc", "api://AzureADTokenExchange"},
	}

	wanted, ok := markers[typeName]
	if !ok {
		t.Fatalf("no marker values declared for %s; add them so a silently empty read is caught", typeName)
	}

	rendered := state.String()
	for _, want := range wanted {
		if !strings.Contains(rendered, want) {
			t.Errorf("%s state does not contain %q — the field was not populated", typeName, want)
		}
	}
}

func configureProvider(
	t *testing.T,
	ctx context.Context,
	server tfprotov6.ProviderServer,
	schemaResp *tfprotov6.GetProviderSchemaResponse,
	baseURL string,
) {
	t.Helper()

	objType := schemaResp.Provider.ValueType().(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attrType := range objType.AttributeTypes {
		switch name {
		case "api_key":
			values[name] = tftypes.NewValue(attrType, "test-key")
		case "base_url":
			values[name] = tftypes.NewValue(attrType, baseURL)
		default:
			values[name] = tftypes.NewValue(attrType, nil)
		}
	}

	config, err := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, values))
	if err != nil {
		t.Fatalf("build provider config: %v", err)
	}

	resp, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &config})
	if err != nil {
		t.Fatalf("ConfigureProvider: %v", err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("configure: %s: %s", d.Summary, d.Detail)
		}
	}
}
