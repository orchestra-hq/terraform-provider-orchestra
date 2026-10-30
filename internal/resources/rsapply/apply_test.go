// Package rsapply_test drives resources through plan and apply against a stub
// API, asserting both the requests the provider makes and the state it
// produces.
//
// The provider previously called PUT /pipelines/{alias} and
// DELETE /pipelines/{alias}, neither of which exists, and read a pipeline via
// GET /pipelines?alias=… where the parameter is ignored. Recording the calls
// the stub receives is what makes that visible in a unit test.
package rsapply_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/provider"
)

type recordedCall struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
	// RawBody is the bytes as sent. Body decodes numbers as float64, so
	// assertions about numeric fidelity have to read RawBody.
	RawBody string
}

type stub struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (s *stub) record(r *http.Request) map[string]any {
	var body map[string]any
	var rawBody string
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			rawBody = string(raw)
			_ = json.Unmarshal(raw, &body)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, recordedCall{
		Method:  r.Method,
		Path:    r.URL.Path,
		Query:   r.URL.Query().Encode(),
		Body:    body,
		RawBody: rawBody,
	})
	return body
}

func (s *stub) find(method, path string) *recordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.calls {
		if s.calls[i].Method == method && s.calls[i].Path == path {
			return &s.calls[i]
		}
	}
	return nil
}

func (s *stub) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.calls {
		out = append(out, c.Method+" "+c.Path)
	}
	return out
}

// pipelineStub answers the endpoints the pipeline resource is supposed to use,
// echoing the definition back the way the real API does. Anything else is a
// test failure.
func pipelineStub(t *testing.T) (*stub, string) {
	t.Helper()

	s := &stub{}
	// Held as raw bytes, not map[string]any: decoding would round integers
	// above 2^53 and make the stub quietly lossier than the real API.
	var stored json.RawMessage
	// The API echoes the alias it was given, so the stub must too.
	storedAlias := "daily_sales"
	// Only the pause endpoint changes this; POST and PUT /pipelines have no
	// paused field.
	paused := false
	// Set by POST /pipelines and the folder endpoint; PUT /pipelines has none.
	folder := ""
	// Every save is a new version, published only when the request says so.
	latestVersion := 0
	var publishedVersion *int

	response := func() map[string]any {
		out := map[string]any{
			"id": "33333333-3333-3333-3333-333333333333", "name": "daily_sales",
			"yamlPath": "", "createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-02T00:00:00Z",
			"paused": paused, "numTasks": 1, "alias": storedAlias,
			"storageProvider": "ORCHESTRA", "latestVersionNumber": latestVersion,
			"publishedVersionNumber": publishedVersion,
		}
		if stored != nil {
			out["data"] = stored
		}
		// The API stores "" as no folder and omits a null one.
		if folder != "" {
			out["folder"] = folder
		}
		return out
	}

	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}

	mux.HandleFunc("/pipelines", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		switch r.Method {
		case http.MethodPost, http.MethodPut:
			if raw, ok := rawField(s.last().RawBody, "data"); ok {
				stored = applyServerDefaults(raw)
			}
			if raw, ok := rawField(s.last().RawBody, "alias"); ok {
				var a string
				if json.Unmarshal(raw, &a) == nil && a != "" {
					storedAlias = a
				}
			}
			if r.Method == http.MethodPost {
				folder, _ = body["folder"].(string)
			}
			latestVersion++
			if body["published"] == true {
				v := latestVersion
				publishedVersion = &v
			}
			code := http.StatusCreated
			if r.Method == http.MethodPut {
				code = http.StatusOK
			}
			write(w, code, response())
		case http.MethodDelete:
			write(w, http.StatusNoContent, nil)
		default:
			t.Errorf("unexpected %s /pipelines", r.Method)
			write(w, http.StatusMethodNotAllowed, nil)
		}
	})
	mux.HandleFunc("/pipeline", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusOK, response())
	})
	mux.HandleFunc("/pipelines/data", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusOK, stored)
	})
	// Plan-time validation.
	mux.HandleFunc("/pipelines/schema", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusOK, map[string]any{"message": "Pipeline schema is valid"})
	})
	mux.HandleFunc("PUT /pipelines/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		paused, _ = body["paused"].(bool)
		// PipelineResponse: no definition.
		out := response()
		delete(out, "data")
		write(w, http.StatusOK, out)
	})
	mux.HandleFunc("PUT /pipelines/{id}/folder", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		folder, _ = body["folder"].(string)
		out := response()
		delete(out, "data")
		write(w, http.StatusOK, out)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		t.Errorf("unexpected request to %s %s — the provider is calling an endpoint the API does not have",
			r.Method, r.URL.Path)
		write(w, http.StatusNotFound, map[string]any{"detail": "no such endpoint"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func newServer(t *testing.T, baseURL string) (tfprotov6.ProviderServer, *tfprotov6.GetProviderSchemaResponse) {
	t.Helper()
	ctx := context.Background()

	server := providerserver.NewProtocol6(provider.New("test")())()
	schemaResp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema: %v", err)
	}

	providerType := schemaResp.Provider.ValueType().(tftypes.Object)
	providerConfig := buildValue(t, providerType, map[string]any{
		"api_key":  "test-key",
		"base_url": baseURL,
	})
	dv, err := tfprotov6.NewDynamicValue(providerType, providerConfig)
	if err != nil {
		t.Fatalf("provider config: %v", err)
	}

	configureResp, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &dv})
	if err != nil {
		t.Fatalf("ConfigureProvider: %v", err)
	}
	failOnError(t, "configure", configureResp.Diagnostics)

	return server, schemaResp
}

func failOnError(t *testing.T, stage string, diags []*tfprotov6.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("%s: %s: %s", stage, d.Summary, d.Detail)
		}
	}
}

// planAndApply runs the create half of a resource lifecycle and returns the
// resulting state.
func planAndApply(
	t *testing.T,
	server tfprotov6.ProviderServer,
	typeName string,
	objType tftypes.Object,
	prior, config tftypes.Value,
	computed map[string]bool,
) tftypes.Value {
	t.Helper()
	ctx := context.Background()

	priorDV, err := tfprotov6.NewDynamicValue(objType, prior)
	if err != nil {
		t.Fatalf("prior state: %v", err)
	}
	configDV, err := tfprotov6.NewDynamicValue(objType, config)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	proposedDV, err := tfprotov6.NewDynamicValue(objType, proposedNew(prior, config, computed))
	if err != nil {
		t.Fatalf("proposed state: %v", err)
	}

	planResp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         typeName,
		PriorState:       &priorDV,
		ProposedNewState: &proposedDV,
		Config:           &configDV,
	})
	if err != nil {
		t.Fatalf("PlanResourceChange: %v", err)
	}
	failOnError(t, "plan", planResp.Diagnostics)

	// An in-place edit must not plan a replacement. The usual cause is an
	// Optional+Computed attribute carrying RequiresReplace without
	// UseStateForUnknown: it goes unknown at plan time and compares unequal to
	// the prior value, so Update becomes unreachable and every edit destroys
	// and re-creates the resource.
	if !prior.IsNull() && len(planResp.RequiresReplace) > 0 {
		for _, p := range planResp.RequiresReplace {
			t.Errorf("%s: in-place update plans a replacement because of %s", typeName, p)
		}
	}

	applyResp, err := server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     typeName,
		PriorState:   &priorDV,
		PlannedState: planResp.PlannedState,
		Config:       &configDV,
	})
	if err != nil {
		t.Fatalf("ApplyResourceChange: %v", err)
	}
	failOnError(t, "apply", applyResp.Diagnostics)

	state, err := applyResp.NewState.Unmarshal(objType)
	if err != nil {
		t.Fatalf("unmarshal new state: %v", err)
	}

	// Terraform rejects an unknown left in state after apply. Catch it here
	// rather than as an opaque "inconsistent result" at the CLI.
	assertNoUnknowns(t, typeName, state)

	planned, err := planResp.PlannedState.Unmarshal(objType)
	if err != nil {
		t.Fatalf("unmarshal planned state: %v", err)
	}
	assertMatchesPlan(t, typeName, planned, state)
	return state
}

// assertMatchesPlan reproduces the check Terraform core runs after every apply:
// a known planned value must survive into state unchanged. Violating it aborts
// the user's apply with "Provider produced inconsistent result after apply" and
// taints the resource, so it has to be caught here rather than at the CLI.
//
// Unknown planned values are skipped — those are exactly the ones the provider
// is expected to fill in.
func assertMatchesPlan(t *testing.T, typeName string, planned, state tftypes.Value) {
	t.Helper()
	err := tftypes.Walk(planned, func(p *tftypes.AttributePath, want tftypes.Value) (bool, error) {
		if !want.IsKnown() {
			return false, nil // provider fills this in; nothing to check below it
		}
		got, _, err := tftypes.WalkAttributePath(state, p)
		if err != nil {
			t.Errorf("%s: %s planned %s but is absent from state", typeName, p, want)
			return false, nil
		}
		gotValue, ok := got.(tftypes.Value)
		if !ok {
			return true, nil
		}
		if want.Equal(gotValue) {
			return true, nil
		}
		// A container that differs somewhere inside is reported by its leaves;
		// descend rather than printing the whole subtree. A null/non-null flip
		// has no leaves to descend into, so report it here — that is the
		// "planned [] but applied null" class.
		if want.IsNull() == gotValue.IsNull() && !isLeaf(want.Type()) {
			return true, nil
		}
		t.Errorf("%s: %s planned %s but applied %s", typeName, p, want, gotValue)
		return false, nil
	})
	if err != nil {
		t.Fatalf("walk planned state: %v", err)
	}
}

func assertNoUnknowns(t *testing.T, typeName string, v tftypes.Value) {
	t.Helper()
	err := tftypes.Walk(v, func(p *tftypes.AttributePath, value tftypes.Value) (bool, error) {
		if !value.IsKnown() {
			t.Errorf("%s: %s is still unknown after apply", typeName, p)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("walk state: %v", err)
	}
}

func TestPipelineCreateUsesRealEndpoints(t *testing.T) {
	calls, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)

	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	config := buildValue(t, objType, map[string]any{
		"alias":     "daily_sales",
		"published": true,
		"data": map[string]any{
			"name": "Daily sales",
			"tasks": map[string]any{
				"ingest": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{"connector_id":"abc"}`,
					// An explicitly empty list is ordinary config and must
					// survive the round trip as empty, not come back null.
					"tags":       []any{},
					"depends_on": []any{},
				},
			},
		},
	})
	prior := tftypes.NewValue(objType, nil)

	state := planAndApply(t, server, "orchestra_pipeline", objType, prior, config, computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))

	// Create must POST /pipelines, not touch /pipelines/{alias}.
	create := calls.find(http.MethodPost, "/pipelines")
	if create == nil {
		t.Fatalf("no POST /pipelines; calls were %v", calls.paths())
	}
	if create.Body["published"] != true {
		t.Errorf("published was %#v, want true", create.Body["published"])
	}

	data, ok := create.Body["data"].(map[string]any)
	if !ok {
		t.Fatalf("create body has no `data` object: %#v", create.Body)
	}
	if data["version"] != "v1" {
		t.Errorf("version was %#v, want the v1 default", data["version"])
	}
	// The two Terraform maps must arrive as the API's single `pipeline` object.
	pipeline, ok := data["pipeline"].(map[string]any)
	if !ok {
		t.Fatalf("`data.pipeline` is missing or not an object: %#v", data)
	}
	if _, ok := pipeline["ingest"]; !ok {
		t.Errorf("`data.pipeline` has no `ingest` entry: %#v", pipeline)
	}
	if _, leaked := data["tasks"]; leaked {
		t.Error("`tasks` leaked to the wire; it only exists in the Terraform schema")
	}

	// The response is used directly, so no read-back is needed on create.
	for _, path := range calls.paths() {
		if strings.HasPrefix(path, "GET ") {
			t.Errorf("create issued %s; the create response already carries the pipeline", path)
		}
	}

	rendered := state.String()
	for _, want := range []string{"33333333-3333-3333-3333-333333333333", "daily_sales"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("state does not contain %q", want)
		}
	}
}

func TestPipelineUpdateAndDeleteUseSelectorEndpoints(t *testing.T) {
	calls, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	create := buildValue(t, objType, map[string]any{
		"alias":     "daily_sales",
		"published": true,
		"data": map[string]any{
			"name": "Daily sales",
			"tasks": map[string]any{
				"ingest": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{"connector_id":"abc"}`,
				},
			},
		},
	})
	created := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil), create, computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))

	// --- update ---
	updated := buildValue(t, objType, map[string]any{
		"alias":     "daily_sales",
		"published": false,
		"data": map[string]any{
			"name": "Daily sales",
			"tasks": map[string]any{
				"ingest": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{"connector_id":"def"}`,
				},
			},
		},
	})
	planAndApply(t, server, "orchestra_pipeline", objType, created, updated, computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))

	update := calls.find(http.MethodPut, "/pipelines")
	if update == nil {
		t.Fatalf("no PUT /pipelines; calls were %v", calls.paths())
	}
	// The selector belongs in the body for this endpoint.
	if update.Body["pipelineId"] != "33333333-3333-3333-3333-333333333333" && update.Body["alias"] != "daily_sales" {
		t.Errorf("update body carries no selector: %#v", update.Body)
	}

	// --- delete ---
	priorDV, err := tfprotov6.NewDynamicValue(objType, created)
	if err != nil {
		t.Fatalf("prior state: %v", err)
	}
	nullDV, err := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, nil))
	if err != nil {
		t.Fatalf("null state: %v", err)
	}

	deleteResp, err := server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     "orchestra_pipeline",
		PriorState:   &priorDV,
		PlannedState: &nullDV,
		Config:       &nullDV,
	})
	if err != nil {
		t.Fatalf("ApplyResourceChange (delete): %v", err)
	}
	failOnError(t, "delete", deleteResp.Diagnostics)

	del := calls.find(http.MethodDelete, "/pipelines")
	if del == nil {
		t.Fatalf("no DELETE /pipelines; calls were %v", calls.paths())
	}
	// The selector belongs in the query string for this endpoint.
	if !strings.Contains(del.Query, "pipeline_id=") && !strings.Contains(del.Query, "alias=") {
		t.Errorf("delete query carries no selector: %q", del.Query)
	}
}

// TestPipelineUpdateWithServerAssignedAliasDoesNotReplace covers the case where
// the practitioner omits `alias` and lets Orchestra assign it. alias and
// yaml_path are then Optional+Computed and null in config, which is exactly the
// shape that goes unknown at plan time and forces a spurious replacement.
func TestPipelineUpdateWithServerAssignedAliasDoesNotReplace(t *testing.T) {
	_, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)

	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"])

	config := func(connector string) tftypes.Value {
		return buildValue(t, objType, map[string]any{
			"published": true,
			"data": map[string]any{
				"name": "Daily sales",
				"tasks": map[string]any{
					"ingest": map[string]any{
						"integration":     "FIVETRAN",
						"integration_job": "FIVETRAN_SYNC_ALL",
						"parameters":      `{"connector_id":"` + connector + `"}`,
					},
				},
			},
		})
	}

	created := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil), config("abc"), comp)
	// planAndApply fails the test if this update plans a replacement.
	planAndApply(t, server, "orchestra_pipeline", objType, created, config("def"), comp)
}

// TestPipelineFreeFormJSONKeepsIntegerPrecision guards the free-form JSON
// fields. Decoding them into map[string]any makes every number a float64, which
// silently rounds integers above 2^53 — the range warehouse job IDs and
// Snowflake IDs occupy — both in the body sent to the API and in state.
func TestPipelineFreeFormJSONKeepsIntegerPrecision(t *testing.T) {
	calls, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)

	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)
	const bigID = "9007199254740993" // 2^53 + 1: the first integer float64 cannot hold

	config := buildValue(t, objType, map[string]any{
		"alias":     "daily_sales",
		"published": true,
		"data": map[string]any{
			"name": "Daily sales",
			"tasks": map[string]any{
				"ingest": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{"job_id":` + bigID + `}`,
				},
			},
		},
	})

	state := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil), config,
		computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))

	create := calls.find(http.MethodPost, "/pipelines")
	if create == nil {
		t.Fatalf("no POST /pipelines; calls were %v", calls.paths())
	}
	if !strings.Contains(create.RawBody, bigID) {
		t.Errorf("request body lost integer precision; want %s in %s", bigID, create.RawBody)
	}
	if !strings.Contains(state.String(), bigID) {
		t.Errorf("state lost integer precision; want %s", bigID)
	}
}

func TestPipelineReadUsesSingularEndpoint(t *testing.T) {
	calls, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	config := buildValue(t, objType, map[string]any{
		"alias":     "daily_sales",
		"published": true,
		"data": map[string]any{
			"name": "Daily sales",
			"tasks": map[string]any{
				"ingest": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{"connector_id":"abc"}`,
				},
			},
		},
	})
	created := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil), config, computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))

	currentDV, err := tfprotov6.NewDynamicValue(objType, created)
	if err != nil {
		t.Fatalf("current state: %v", err)
	}

	readResp, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
		TypeName:     "orchestra_pipeline",
		CurrentState: &currentDV,
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	failOnError(t, "read", readResp.Diagnostics)

	// GET /pipelines ignores selector parameters — a read must use the
	// singular GET /pipeline.
	if calls.find(http.MethodGet, "/pipeline") == nil {
		t.Errorf("read did not call GET /pipeline; calls were %v", calls.paths())
	}
	if got := calls.find(http.MethodGet, "/pipelines"); got != nil {
		t.Errorf("read called GET /pipelines?%s, which ignores selectors and returns the wrong pipeline", got.Query)
	}
}

func TestPipelineReadRemovesResourceOn404(t *testing.T) {
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"not found"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	server, schemaResp := newServer(t, srv.URL)
	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	state := buildValue(t, objType, map[string]any{
		"alias":     "gone",
		"id":        "33333333-3333-3333-3333-333333333333",
		"published": true,
	})
	stateDV, err := tfprotov6.NewDynamicValue(objType, state)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	readResp, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
		TypeName:     "orchestra_pipeline",
		CurrentState: &stateDV,
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	failOnError(t, "read", readResp.Diagnostics)

	newState, err := readResp.NewState.Unmarshal(objType)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !newState.IsNull() {
		t.Errorf("a pipeline deleted out of band should be removed from state, got %s", newState)
	}
}

func TestEnvironmentLifecycle(t *testing.T) {
	s := &stub{}
	// Held as raw bytes, not map[string]any: decoding would round integers
	// above 2^53 and make the stub quietly lossier than the real API.
	var stored map[string]any

	response := func() map[string]any {
		out := map[string]any{
			"accountId":     "22222222-2222-2222-2222-222222222222",
			"environmentId": "44444444-4444-4444-4444-444444444444",
			"name":          "production", "defaultEnv": false,
			"allowedBranches": []string{"main"}, "allowUnpublishedPipelines": true,
			"createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-02T00:00:00Z",
			"values": map[string]any{},
		}
		if stored != nil {
			out["values"] = stored
		}
		return out
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/environments", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		if values, ok := body["values"].(map[string]any); ok {
			stored = values
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(response())
	})
	mux.HandleFunc("/environments/", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		if values, ok := body["values"].(map[string]any); ok {
			stored = values
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
		http.Error(w, "unhandled", http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	server, schemaResp := newServer(t, srv.URL)
	objType := schemaResp.ResourceSchemas["orchestra_environment"].ValueType().(tftypes.Object)

	config := buildValue(t, objType, map[string]any{
		"name": "production",
		"values": map[string]any{
			"WAREHOUSE":   map[string]any{"type": "string", "value": "COMPUTE_WH"},
			"BATCH_SIZE":  map[string]any{"type": "int", "value": "500"},
			"STRICT_MODE": map[string]any{"type": "bool", "value": "true"},
		},
	})

	state := planAndApply(t, server, "orchestra_environment", objType, tftypes.NewValue(objType, nil), config, computedAttrs(schemaResp.ResourceSchemas["orchestra_environment"]))

	create := s.find(http.MethodPost, "/environments")
	if create == nil {
		t.Fatalf("no POST /environments; calls were %v", s.paths())
	}

	values, ok := create.Body["values"].(map[string]any)
	if !ok {
		t.Fatalf("create body has no `values`: %#v", create.Body)
	}
	// Terraform carries every value as a string; the provider must send each
	// one as the type the entry declares.
	for name, want := range map[string]any{
		"WAREHOUSE": "COMPUTE_WH", "BATCH_SIZE": float64(500), "STRICT_MODE": true,
	} {
		entry, ok := values[name].(map[string]any)
		if !ok {
			t.Errorf("values[%s] is missing: %#v", name, values)
			continue
		}
		if entry["value"] != want {
			t.Errorf("values[%s].value was %#v (%T), want %#v", name, entry["value"], entry["value"], want)
		}
	}

	if !strings.Contains(state.String(), "44444444-4444-4444-4444-444444444444") {
		t.Errorf("state does not carry the environment ID: %s", state)
	}
}

func TestPipelineSchemaRejectionSurfacesAtPlan(t *testing.T) {
	ctx := context.Background()
	s := &stub{}

	mux := http.NewServeMux()
	mux.HandleFunc("/pipelines/schema", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		http.Error(w, `{"detail":"task 'ingest' has an unknown integration_job"}`, http.StatusUnprocessableEntity)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	server, schemaResp := newServer(t, srv.URL)
	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	config := buildValue(t, objType, map[string]any{
		"alias":     "daily_sales",
		"published": true,
		"data": map[string]any{
			"name": "Daily sales",
			"tasks": map[string]any{
				"ingest": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{}`,
				},
			},
		},
	})
	configDV, err := tfprotov6.NewDynamicValue(objType, config)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	nullDV, err := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, nil))
	if err != nil {
		t.Fatalf("null: %v", err)
	}

	planResp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "orchestra_pipeline",
		PriorState:       &nullDV,
		ProposedNewState: &configDV,
		Config:           &configDV,
	})
	if err != nil {
		t.Fatalf("PlanResourceChange: %v", err)
	}

	if s.find(http.MethodPost, "/pipelines/schema") == nil {
		t.Fatalf("the plan did not validate the definition; calls were %v", s.paths())
	}

	var warned bool
	for _, d := range planResp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("a rejected definition must not block the plan: %s: %s", d.Summary, d.Detail)
		}
		if d.Severity == tfprotov6.DiagnosticSeverityWarning &&
			strings.Contains(d.Detail, "unknown integration_job") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a warning carrying the API's reason, got %#v", planResp.Diagnostics)
	}
}

func TestPipelineSchemaRateLimitIsIgnored(t *testing.T) {
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/pipelines/schema", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	server, schemaResp := newServer(t, srv.URL)
	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	config := buildValue(t, objType, map[string]any{
		"alias":     "daily_sales",
		"published": true,
		"data": map[string]any{
			"name": "Daily sales",
			"tasks": map[string]any{
				"ingest": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{}`,
				},
			},
		},
	})
	configDV, err := tfprotov6.NewDynamicValue(objType, config)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	nullDV, err := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, nil))
	if err != nil {
		t.Fatalf("null: %v", err)
	}

	planResp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "orchestra_pipeline",
		PriorState:       &nullDV,
		ProposedNewState: &configDV,
		Config:           &configDV,
	})
	if err != nil {
		t.Fatalf("PlanResourceChange: %v", err)
	}

	// The endpoint is rate limited to 20 requests per minute per IP; hitting
	// that must not colour the plan.
	for _, d := range planResp.Diagnostics {
		t.Errorf("rate limiting produced a diagnostic: %s: %s", d.Summary, d.Detail)
	}
}

func isLeaf(t tftypes.Type) bool {
	return t.Is(tftypes.String) || t.Is(tftypes.Number) || t.Is(tftypes.Bool) || t.Is(tftypes.DynamicPseudoType)
}

// proposedNew reproduces the part of Terraform's objchange.ProposedNew that
// matters here: for an Optional+Computed attribute left null in config, the
// proposed state carries the prior value forward rather than null. Passing raw
// config instead makes every such attribute look unknown at plan time, which
// manufactures spurious RequiresReplace diffs that Terraform would never
// produce.
func proposedNew(prior, config tftypes.Value, computed map[string]bool) tftypes.Value {
	if prior.IsNull() || !prior.IsKnown() {
		return config // create: the proposal is the configuration
	}
	var priorAttrs, configAttrs map[string]tftypes.Value
	if err := prior.As(&priorAttrs); err != nil {
		return config
	}
	if err := config.As(&configAttrs); err != nil {
		return config
	}
	out := make(map[string]tftypes.Value, len(configAttrs))
	for name, cv := range configAttrs {
		if cv.IsNull() && computed[name] {
			if pv, ok := priorAttrs[name]; ok {
				out[name] = pv
				continue
			}
		}
		out[name] = cv
	}
	return tftypes.NewValue(config.Type(), out)
}

// computedAttrs reports which top-level attributes the schema marks Computed.
func computedAttrs(schema *tfprotov6.Schema) map[string]bool {
	out := map[string]bool{}
	for _, a := range schema.Block.Attributes {
		if a.Computed {
			out[a.Name] = true
		}
	}
	return out
}

// last returns the most recently recorded call.
func (s *stub) last() recordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return recordedCall{}
	}
	return s.calls[len(s.calls)-1]
}

// rawField pulls one top-level field out of a JSON object without decoding its
// contents, preserving number formatting exactly as sent.
func rawField(body, field string) (json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		return nil, false
	}
	v, ok := obj[field]
	return v, ok
}

// applyServerDefaults mirrors what the real API does to a stored definition:
// it serialises PipelineModel through its own models, so fields with a declared
// default come back populated even when the request omitted them. Verified
// against the live API, which returns `"webhook": {"enabled": false}` for a
// pipeline created without a webhook block.
//
// Inputs need nothing here: the live API returns an input's `options` as sent
// and omits the key when it was not set, rather than returning null.
func applyServerDefaults(raw json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	if _, ok := obj["webhook"]; !ok {
		obj["webhook"] = json.RawMessage(`{"enabled":false}`)
	}
	if raw, ok := obj["pipeline"]; ok {
		obj["pipeline"] = defaultEntries(raw)
	}
	if raw, ok := obj["anomalies"]; ok {
		obj["anomalies"] = defaultAnomalies(raw)
	}
	if raw, ok := obj["configuration"]; ok {
		obj["configuration"] = defaultConcurrency(raw)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

// defaultEntries fills in the per-entry defaults the API applies: a task or
// group with no name comes back with name "", and anomalies come back with
// their numeric fields populated.
func defaultEntries(raw json.RawMessage) json.RawMessage {
	var entries map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return raw
	}
	for _, entry := range entries {
		if _, ok := entry["name"]; !ok {
			entry["name"] = json.RawMessage(`""`)
		}
		if a, ok := entry["anomalies"]; ok {
			entry["anomalies"] = defaultAnomalies(a)
		}
		if tasks, ok := entry["tasks"]; ok {
			entry["tasks"] = defaultEntries(tasks)
		}
	}
	out, err := json.Marshal(entries)
	if err != nil {
		return raw
	}
	return out
}

// defaultAnomalies mirrors the values the live API returns for an anomaly
// given only a `type`: minBaselineSeconds 10 and percentageAboveBaseline 20.
// Note the spec claims minBaselineSeconds defaults to 0; the API disagrees,
// which is why the schema marks these Computed rather than carrying a default.
func defaultAnomalies(raw json.RawMessage) json.RawMessage {
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return raw
	}
	for _, a := range list {
		if _, ok := a["minBaselineSeconds"]; !ok {
			a["minBaselineSeconds"] = json.RawMessage(`10`)
		}
		if _, ok := a["percentageAboveBaseline"]; !ok {
			a["percentageAboveBaseline"] = json.RawMessage(`20`)
		}
	}
	out, err := json.Marshal(list)
	if err != nil {
		return raw
	}
	return out
}

// defaultConcurrency fills in onLimit, which the spec declares non-nullable
// with a default of SKIP.
func defaultConcurrency(raw json.RawMessage) json.RawMessage {
	var cfg map[string]json.RawMessage
	var c map[string]json.RawMessage
	if json.Unmarshal(raw, &cfg) != nil || json.Unmarshal(cfg["concurrency"], &c) != nil || c == nil {
		return raw
	}
	if _, ok := c["onLimit"]; !ok {
		c["onLimit"] = json.RawMessage(`"SKIP"`)
	}
	cfg["concurrency"], _ = json.Marshal(c)
	out, err := json.Marshal(cfg)
	if err != nil {
		return raw
	}
	return out
}

// TestPipelineServerDefaultsDoNotBreakApply covers the whole class: the API
// fills in declared defaults on the way out, so an Optional attribute left
// unset in config comes back populated. planAndApply fails if any planned
// value differs from the applied one.
func TestPipelineServerDefaultsDoNotBreakApply(t *testing.T) {
	_, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)

	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	config := buildValue(t, objType, map[string]any{
		"alias":     "defaults",
		"published": true,
		"data": map[string]any{
			"name": "defaults",
			// A group and a task with no `name`, and an anomaly with only a
			// `type` — the minimal spellings the docs encourage.
			"task_groups": map[string]any{
				"g1": map[string]any{
					"tasks": map[string]any{
						"t1": map[string]any{
							"integration":     "FIVETRAN",
							"integration_job": "FIVETRAN_SYNC_ALL",
							"parameters":      `{"connector_id":"x"}`,
							"anomalies": []any{
								map[string]any{"type": "task_duration_above_baseline"},
							},
						},
					},
				},
			},
			"anomalies": []any{
				map[string]any{"type": "pipeline_duration_above_baseline"},
			},
			// A limit with no on_limit, which the API returns as SKIP.
			"configuration": map[string]any{"retries": "1", "concurrency": map[string]any{"max_active": "2"}},
		},
	})

	planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil), config,
		computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))
}

// TestPipelineInputOptionsRoundTrip guards against an apply clearing an input's
// allowed values: PUT /pipelines replaces the whole definition, so anything the
// provider does not send is lost.
func TestPipelineInputOptionsRoundTrip(t *testing.T) {
	calls, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	config := buildValue(t, objType, map[string]any{
		"alias":     "inputs",
		"published": true,
		"data": map[string]any{
			"name": "inputs",
			"tasks": map[string]any{
				"t1": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{"connector_id":"x"}`,
				},
			},
			"inputs": map[string]any{
				"cadence": map[string]any{"type": "string", "default": `"daily"`, "options": []any{"daily", "weekly"}},
				"limit":   map[string]any{"type": "number"},
			},
		},
	})

	created := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil), config,
		computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))

	create := calls.find(http.MethodPost, "/pipelines")
	if create == nil {
		t.Fatalf("no POST /pipelines; calls were %v", calls.paths())
	}
	var body struct {
		Data struct {
			Inputs map[string]map[string]json.RawMessage `json:"inputs"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(create.RawBody), &body); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if got := string(body.Data.Inputs["cadence"]["options"]); got != `["daily","weekly"]` {
		t.Errorf("cadence options sent as %s, want [\"daily\",\"weekly\"]", got)
	}
	if got, ok := body.Data.Inputs["limit"]["options"]; ok {
		t.Errorf("limit sent options %s, want the key omitted", got)
	}

	currentDV, err := tfprotov6.NewDynamicValue(objType, created)
	if err != nil {
		t.Fatalf("current state: %v", err)
	}
	readResp, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
		TypeName:     "orchestra_pipeline",
		CurrentState: &currentDV,
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	failOnError(t, "read", readResp.Diagnostics)
	read, err := readResp.NewState.Unmarshal(objType)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if !read.Equal(created) {
		t.Errorf("read changed the state; a refresh would plan a diff\nwant: %s\ngot:  %s", created, read)
	}
}

// environmentStub answers the environment endpoints with the shapes the live
// API returns: the list omits `values` and the timestamps
// (ProtectedEnvironmentsResponse), the single-environment read includes them,
// and `defaultEnv` is owned by the server.
func environmentStub(t *testing.T, serverDefaultEnv bool) (*stub, string) {
	t.Helper()

	s := &stub{}
	const id = "44444444-4444-4444-4444-444444444444"
	stored := map[string]any{}
	name := "production"

	response := func() map[string]any {
		return map[string]any{
			"accountId":     "22222222-2222-2222-2222-222222222222",
			"environmentId": id,
			"name":          name,
			// Server-owned: the first environment in an account is promoted
			// whatever the request said.
			"defaultEnv":                serverDefaultEnv,
			"allowedBranches":           []string{"main"},
			"allowUnpublishedPipelines": true,
			"createdAt":                 "2026-01-01T00:00:00Z",
			"updatedAt":                 "2026-01-02T00:00:00Z",
			"values":                    stored,
		}
	}

	absorb := func(body map[string]any) {
		if v, ok := body["values"].(map[string]any); ok {
			stored = v
		}
		if n, ok := body["name"].(string); ok && n != "" {
			name = n
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/environments", func(w http.ResponseWriter, r *http.Request) {
		absorb(s.record(r))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(response())
	})
	mux.HandleFunc("/environments/", func(w http.ResponseWriter, r *http.Request) {
		absorb(s.record(r))
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
		http.Error(w, "unhandled", http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func environmentValue(t *testing.T, objType tftypes.Object, name, batchSize string) tftypes.Value {
	t.Helper()
	return buildValue(t, objType, map[string]any{
		"name": name,
		"values": map[string]any{
			"WAREHOUSE":  map[string]any{"type": "string", "value": "COMPUTE_WH"},
			"BATCH_SIZE": map[string]any{"type": "int", "value": batchSize},
			"DEBUG":      map[string]any{"type": "bool", "value": "false"},
		},
	})
}

// TestEnvironmentServerPromotedDefaultDoesNotBreakApply covers the case the
// unit suite previously missed entirely: the server promotes the environment to
// default even though the plan said false. Overwriting the planned value from
// the response aborts the apply.
func TestEnvironmentServerPromotedDefaultDoesNotBreakApply(t *testing.T) {
	_, baseURL := environmentStub(t, true)
	server, schemaResp := newServer(t, baseURL)

	objType := schemaResp.ResourceSchemas["orchestra_environment"].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas["orchestra_environment"])

	// default_env defaults to false, so the plan is false while the API says true.
	planAndApply(t, server, "orchestra_environment", objType,
		tftypes.NewValue(objType, nil), environmentValue(t, objType, "production", "500"), comp)
}

// TestEnvironmentUpdateReadAndDelete exercises the half of the lifecycle that
// had no coverage at all.
func TestEnvironmentUpdateReadAndDelete(t *testing.T) {
	calls, baseURL := environmentStub(t, false)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_environment"].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas["orchestra_environment"])

	created := planAndApply(t, server, "orchestra_environment", objType,
		tftypes.NewValue(objType, nil), environmentValue(t, objType, "production", "500"), comp)

	// --- update ---
	planAndApply(t, server, "orchestra_environment", objType, created,
		environmentValue(t, objType, "production", "1000"), comp)

	patch := calls.find(http.MethodPatch, "/environments/44444444-4444-4444-4444-444444444444")
	if patch == nil {
		t.Fatalf("no PATCH /environments/{id}; calls were %v", calls.paths())
	}
	values, ok := patch.Body["values"].(map[string]any)
	if !ok {
		t.Fatalf("patch body has no values object: %#v", patch.Body)
	}
	batch, ok := values["BATCH_SIZE"].(map[string]any)
	if !ok {
		t.Fatalf("patch body has no BATCH_SIZE: %#v", values)
	}
	// int values must go on the wire as JSON numbers, not strings.
	if got, want := batch["value"], float64(1000); got != want {
		t.Errorf("BATCH_SIZE was %#v, want the number %v", got, want)
	}
	// PATCH requires name and defaultEnv alongside values.
	for _, field := range []string{"name", "defaultEnv"} {
		if _, ok := patch.Body[field]; !ok {
			t.Errorf("patch body is missing %q, which the API requires", field)
		}
	}

	// --- read ---
	priorDV, err := tfprotov6.NewDynamicValue(objType, created)
	if err != nil {
		t.Fatalf("prior state: %v", err)
	}
	readResp, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
		TypeName: "orchestra_environment", CurrentState: &priorDV,
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	failOnError(t, "read", readResp.Diagnostics)
	if calls.find(http.MethodGet, "/environments/44444444-4444-4444-4444-444444444444") == nil {
		t.Errorf("read did not GET the environment by ID; calls were %v", calls.paths())
	}

	// --- delete ---
	nullDV, err := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, nil))
	if err != nil {
		t.Fatalf("null state: %v", err)
	}
	deleteResp, err := server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: "orchestra_environment", PriorState: &priorDV, PlannedState: &nullDV, Config: &nullDV,
	})
	if err != nil {
		t.Fatalf("ApplyResourceChange (delete): %v", err)
	}
	failOnError(t, "delete", deleteResp.Diagnostics)
	if calls.find(http.MethodDelete, "/environments/44444444-4444-4444-4444-444444444444") == nil {
		t.Errorf("delete did not DELETE by ID; calls were %v", calls.paths())
	}
}

// TestEnvironmentImport checks that an import populates enough state for the
// next plan to be empty — including `values`, which only the single-environment
// read returns.
func TestEnvironmentImport(t *testing.T) {
	const id = "44444444-4444-4444-4444-444444444444"
	calls, baseURL := environmentStub(t, false)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_environment"].ValueType().(tftypes.Object)

	importResp, err := server.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{
		TypeName: "orchestra_environment", ID: id,
	})
	if err != nil {
		t.Fatalf("ImportResourceState: %v", err)
	}
	failOnError(t, "import", importResp.Diagnostics)
	if len(importResp.ImportedResources) != 1 {
		t.Fatalf("got %d imported resources, want 1", len(importResp.ImportedResources))
	}

	imported, err := importResp.ImportedResources[0].State.Unmarshal(objType)
	if err != nil {
		t.Fatalf("unmarshal imported state: %v", err)
	}
	var attrs map[string]tftypes.Value
	if err := imported.As(&attrs); err != nil {
		t.Fatalf("decode imported state: %v", err)
	}
	if attrs["id"].IsNull() {
		t.Error("import left `id` null")
	}

	// Terraform refreshes right after import, so that is what has to produce a
	// complete state.
	importedDV, err := tfprotov6.NewDynamicValue(objType, imported)
	if err != nil {
		t.Fatalf("imported state: %v", err)
	}
	readResp, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
		TypeName: "orchestra_environment", CurrentState: &importedDV,
	})
	if err != nil {
		t.Fatalf("ReadResource after import: %v", err)
	}
	failOnError(t, "read after import", readResp.Diagnostics)

	refreshed, err := readResp.NewState.Unmarshal(objType)
	if err != nil {
		t.Fatalf("unmarshal refreshed state: %v", err)
	}
	var refreshedAttrs map[string]tftypes.Value
	if err := refreshed.As(&refreshedAttrs); err != nil {
		t.Fatalf("decode refreshed state: %v", err)
	}
	for _, field := range []string{"name", "values", "account_id", "default_env"} {
		if refreshedAttrs[field].IsNull() {
			t.Errorf("%s is null after import+refresh; the next plan would not be empty", field)
		}
	}
	if calls.find(http.MethodGet, "/environments/"+id) == nil {
		t.Errorf("import+refresh never read the environment; calls were %v", calls.paths())
	}
}

// TestEnvironmentRejectsNonCanonicalValues covers the plan-time guard. The API
// stores int and bool values as JSON scalars, so "007" comes back as "7" and
// would fail the apply as an inconsistent result. Rejecting it at plan time
// gives the user something actionable instead.
func TestEnvironmentRejectsNonCanonicalValues(t *testing.T) {
	_, baseURL := environmentStub(t, false)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_environment"].ValueType().(tftypes.Object)

	for _, tc := range []struct {
		name, valueType, value, wantIn string
	}{
		{"leading zeros", "int", "007", "Non-canonical integer"},
		{"explicit plus", "int", "+5", "Non-canonical integer"},
		{"uppercase bool", "bool", "TRUE", "Non-canonical boolean"},
		{"not an integer", "int", "many", "Invalid integer"},
		{"not a boolean", "bool", "yes-please", "Invalid boolean"},
		{"abbreviated bool", "bool", "T", "Non-canonical boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := buildValue(t, objType, map[string]any{
				"name": "production",
				"values": map[string]any{
					"N": map[string]any{"type": tc.valueType, "value": tc.value},
				},
			})
			configDV, err := tfprotov6.NewDynamicValue(objType, config)
			if err != nil {
				t.Fatalf("config: %v", err)
			}

			resp, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
				TypeName: "orchestra_environment", Config: &configDV,
			})
			if err != nil {
				t.Fatalf("ValidateResourceConfig: %v", err)
			}

			var got string
			for _, d := range resp.Diagnostics {
				if d.Severity == tfprotov6.DiagnosticSeverityError {
					got += d.Summary + " " + d.Detail
				}
			}
			if got == "" {
				t.Fatalf("%s=%q was accepted; it would fail at apply instead", tc.valueType, tc.value)
			}
			if !strings.Contains(got, tc.wantIn) {
				t.Errorf("diagnostic was %q, want it to mention %q", got, tc.wantIn)
			}
		})
	}
}

// TestEnvironmentAcceptsCanonicalValues is the other half: the guard must not
// reject ordinary configuration.
func TestEnvironmentAcceptsCanonicalValues(t *testing.T) {
	_, baseURL := environmentStub(t, false)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_environment"].ValueType().(tftypes.Object)
	config := buildValue(t, objType, map[string]any{
		"name": "production",
		"values": map[string]any{
			"N":    map[string]any{"type": "int", "value": "-3"},
			"ZERO": map[string]any{"type": "int", "value": "0"},
			"B":    map[string]any{"type": "bool", "value": "true"},
			"S":    map[string]any{"type": "string", "value": "007"},
			"CRED": map[string]any{"type": "integration_credential", "value": "conn_1"},
		},
	})
	configDV, err := tfprotov6.NewDynamicValue(objType, config)
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	resp, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
		TypeName: "orchestra_environment", Config: &configDV,
	})
	if err != nil {
		t.Fatalf("ValidateResourceConfig: %v", err)
	}
	failOnError(t, "validate", resp.Diagnostics)
}

// TestPipelineValidateConfig covers the plan-time checks on `data`. Both exist
// because the API merges task_groups and tasks into one `pipeline` object, so a
// collision silently drops an entry and an empty pipeline is rejected
// server-side with a far less helpful message.
func TestPipelineValidateConfig(t *testing.T) {
	_, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	task := map[string]any{
		"integration":     "FIVETRAN",
		"integration_job": "FIVETRAN_SYNC_ALL",
		"parameters":      `{"connector_id":"x"}`,
	}

	for _, tc := range []struct {
		name   string
		data   map[string]any
		wantIn string
	}{
		{
			name: "id used by both maps",
			data: map[string]any{
				"name":        "clash",
				"tasks":       map[string]any{"shared": task},
				"task_groups": map[string]any{"shared": map[string]any{"tasks": map[string]any{"t": task}}},
			},
			wantIn: "Duplicate pipeline entry ID",
		},
		{
			name:   "no entries at all",
			data:   map[string]any{"name": "empty"},
			wantIn: "Empty pipeline",
		},
		{
			name: "options on a non-string input",
			data: map[string]any{
				"name":   "opts",
				"tasks":  map[string]any{"t": task},
				"inputs": map[string]any{"limit": map[string]any{"type": "number", "options": []any{"1", "2"}}},
			},
			wantIn: "Options on a non-string input",
		},
		{
			name: "empty options",
			data: map[string]any{
				"name":   "opts",
				"tasks":  map[string]any{"t": task},
				"inputs": map[string]any{"cadence": map[string]any{"type": "string", "options": []any{}}},
			},
			wantIn: "at least 1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := buildValue(t, objType, map[string]any{
				"alias": "v", "published": true, "data": tc.data,
			})
			configDV, err := tfprotov6.NewDynamicValue(objType, config)
			if err != nil {
				t.Fatalf("config: %v", err)
			}

			resp, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
				TypeName: "orchestra_pipeline", Config: &configDV,
			})
			if err != nil {
				t.Fatalf("ValidateResourceConfig: %v", err)
			}

			var got string
			for _, d := range resp.Diagnostics {
				if d.Severity == tfprotov6.DiagnosticSeverityError {
					got += d.Summary + ": " + d.Detail + " "
				}
			}
			if !strings.Contains(got, tc.wantIn) {
				t.Errorf("diagnostics were %q, want %q", got, tc.wantIn)
			}
		})
	}

	t.Run("valid config passes", func(t *testing.T) {
		config := buildValue(t, objType, map[string]any{
			"alias": "v", "published": true,
			"data": map[string]any{"name": "fine", "tasks": map[string]any{"t1": task}},
		})
		configDV, err := tfprotov6.NewDynamicValue(objType, config)
		if err != nil {
			t.Fatalf("config: %v", err)
		}
		resp, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
			TypeName: "orchestra_pipeline", Config: &configDV,
		})
		if err != nil {
			t.Fatalf("ValidateResourceConfig: %v", err)
		}
		failOnError(t, "validate", resp.Diagnostics)
	})
}

// TestPipelineImport checks an import by alias gives a state the next plan can use, including the
// definition, fetched by a second request, and `published`, which the API does not return.
func TestPipelineImport(t *testing.T) {
	for name, published := range map[string]bool{"published": true, "draft": false} {
		t.Run(name, func(t *testing.T) {
			calls, baseURL := pipelineStub(t)
			server, schemaResp := newServer(t, baseURL)
			ctx := context.Background()

			objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

			// Create first so the stub holds a definition to hand back.
			config := buildValue(t, objType, map[string]any{
				"alias": "importable", "published": published,
				"data": map[string]any{
					"name": "importable",
					"tasks": map[string]any{"t1": map[string]any{
						"integration":     "FIVETRAN",
						"integration_job": "FIVETRAN_SYNC_ALL",
						"parameters":      `{"connector_id":"x"}`,
					}},
				},
			})
			planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil), config,
				computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))

			importResp, err := server.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{
				TypeName: "orchestra_pipeline", ID: "importable",
			})
			if err != nil {
				t.Fatalf("ImportResourceState: %v", err)
			}
			failOnError(t, "import", importResp.Diagnostics)
			if len(importResp.ImportedResources) != 1 {
				t.Fatalf("got %d imported resources, want 1", len(importResp.ImportedResources))
			}

			imported, err := importResp.ImportedResources[0].State.Unmarshal(objType)
			if err != nil {
				t.Fatalf("unmarshal imported state: %v", err)
			}

			importedDV, err := tfprotov6.NewDynamicValue(objType, imported)
			if err != nil {
				t.Fatalf("imported state: %v", err)
			}
			readResp, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
				TypeName: "orchestra_pipeline", CurrentState: &importedDV,
			})
			if err != nil {
				t.Fatalf("ReadResource after import: %v", err)
			}
			failOnError(t, "read after import", readResp.Diagnostics)

			refreshed, err := readResp.NewState.Unmarshal(objType)
			if err != nil {
				t.Fatalf("unmarshal refreshed state: %v", err)
			}
			var attrs map[string]tftypes.Value
			if err := refreshed.As(&attrs); err != nil {
				t.Fatalf("decode refreshed state: %v", err)
			}
			for _, field := range []string{"id", "alias", "data", "name"} {
				if attrs[field].IsNull() {
					t.Errorf("%s is null after import+refresh; the next plan would not be empty", field)
				}
			}
			if got := attrs["published"]; !got.Equal(tftypes.NewValue(tftypes.Bool, published)) {
				t.Errorf("published is %s after import, want %t", got, published)
			}
			if calls.find(http.MethodGet, "/pipelines/data") == nil {
				t.Errorf("import+refresh never fetched the definition; calls were %v", calls.paths())
			}
		})
	}
}

// TestEnvironmentDefaultEnvUsesFollowUpPatch covers the create-then-patch path.
// CreateEnvironmentsRequest has no defaultEnv field, so setting it requires a
// second call — and that call must carry the same values, or creating a default
// environment would quietly blank them.
func TestEnvironmentDefaultEnvUsesFollowUpPatch(t *testing.T) {
	calls, baseURL := environmentStub(t, true)
	server, schemaResp := newServer(t, baseURL)

	objType := schemaResp.ResourceSchemas["orchestra_environment"].ValueType().(tftypes.Object)
	config := buildValue(t, objType, map[string]any{
		"name":        "production",
		"default_env": true,
		"values": map[string]any{
			"WAREHOUSE": map[string]any{"type": "string", "value": "COMPUTE_WH"},
		},
	})

	planAndApply(t, server, "orchestra_environment", objType, tftypes.NewValue(objType, nil), config,
		computedAttrs(schemaResp.ResourceSchemas["orchestra_environment"]))

	if calls.find(http.MethodPost, "/environments") == nil {
		t.Fatalf("no POST /environments; calls were %v", calls.paths())
	}
	patch := calls.find(http.MethodPatch, "/environments/44444444-4444-4444-4444-444444444444")
	if patch == nil {
		t.Fatalf("default_env = true did not trigger a follow-up PATCH; calls were %v", calls.paths())
	}
	if patch.Body["defaultEnv"] != true {
		t.Errorf("follow-up patch did not set defaultEnv: %#v", patch.Body)
	}
	values, ok := patch.Body["values"].(map[string]any)
	if !ok || len(values) == 0 {
		t.Errorf("follow-up patch dropped the environment values: %#v", patch.Body)
	}
}

// TestPipelineWithoutAliasSettlesToNull covers the case the live API produces
// for a pipeline created without an alias: PipelineResponse omits `alias`
// entirely. The planned value is unknown, and an unknown left in state after
// apply is rejected by Terraform, so it has to settle to null.
func TestPipelineWithoutAliasSettlesToNull(t *testing.T) {
	s := &stub{}
	var stored json.RawMessage

	mux := http.NewServeMux()
	mux.HandleFunc("/pipelines", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if raw, ok := rawField(s.last().RawBody, "data"); ok {
			stored = applyServerDefaults(raw)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		// No "alias" key at all — exactly what the API returns.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "55555555-5555-5555-5555-555555555555", "name": "no alias",
			"yamlPath": "acct/pipe/versions/1.yaml", "createdAt": "2026-01-01T00:00:00Z",
			"updatedAt": "2026-01-02T00:00:00Z", "paused": false, "numTasks": 1,
			"storageProvider": "ORCHESTRA", "data": stored,
		})
	})
	mux.HandleFunc("/pipelines/schema", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"Pipeline schema is valid"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
		http.Error(w, "unhandled", http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	server, schemaResp := newServer(t, srv.URL)
	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	config := buildValue(t, objType, map[string]any{
		"published": true,
		"data": map[string]any{
			"name": "no alias",
			"tasks": map[string]any{"t1": map[string]any{
				"integration":     "FIVETRAN",
				"integration_job": "FIVETRAN_SYNC_ALL",
				"parameters":      `{"connector_id":"x"}`,
			}},
		},
	})

	// planAndApply fails if `alias` is left unknown in state.
	state := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil), config,
		computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))

	var attrs map[string]tftypes.Value
	if err := state.As(&attrs); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if !attrs["alias"].IsNull() {
		t.Errorf("alias settled to %s, want null", attrs["alias"])
	}
}

// TestPipelineReadNotFoundNamesTheSelector covers the not-found path where the
// API answers 200 with an empty body rather than 404. The diagnostic has to say
// which selector was used, or there is nothing to debug with.
func TestPipelineReadNotFoundNamesTheSelector(t *testing.T) {
	s := &stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/pipeline", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		w.Header().Set("Content-Type", "application/json")
		// 200 with no id: the pipeline is gone.
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		http.Error(w, "unhandled", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	server, schemaResp := newServer(t, srv.URL)
	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	prior := buildValue(t, objType, map[string]any{
		"alias": "vanished", "published": true,
		"id":         "66666666-6666-6666-6666-666666666666",
		"name":       "vanished",
		"yaml_path":  "acct/pipe/versions/1.yaml",
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-02T00:00:00Z",
		"num_tasks":  1,
		"paused":     false,
	})
	priorDV, err := tfprotov6.NewDynamicValue(objType, prior)
	if err != nil {
		t.Fatalf("prior state: %v", err)
	}

	readResp, err := server.ReadResource(context.Background(), &tfprotov6.ReadResourceRequest{
		TypeName: "orchestra_pipeline", CurrentState: &priorDV,
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	failOnError(t, "read", readResp.Diagnostics)

	// A missing pipeline is removed from state, not reported as an error.
	state, err := readResp.NewState.Unmarshal(objType)
	if err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if !state.IsNull() {
		t.Error("a pipeline the API no longer has was left in state")
	}
}

// monitorStub answers the monitor endpoints the way the live API does: it
// echoes the stored definition back with the server-side defaults filled in
// (`enabled`, `action.mute`, `action.subscribers`, `match.operationFailuresOnly`,
// `alsoSendToPipelineDestinations`) and assigns
// `order`, which is workspace-wide and not settable per monitor.
func monitorStub(t *testing.T) (*stub, string) {
	t.Helper()

	s := &stub{}
	const id = "77777777-7777-7777-7777-777777777777"
	var stored json.RawMessage

	response := func() map[string]any {
		var def map[string]any
		if stored != nil {
			_ = json.Unmarshal(stored, &def)
		}
		if def == nil {
			def = map[string]any{}
		}
		// Defaults the API applies on the way out, verified against the live
		// API: every match dimension comes back as a list even when it was not
		// sent, and `action`, with `action.alerts`, materialises as an object
		// even for a monitor created without one.
		if _, ok := def["enabled"]; !ok {
			def["enabled"] = true
		}
		def["order"] = 0
		match, _ := def["match"].(map[string]any)
		if match == nil {
			match = map[string]any{}
			def["match"] = match
		}
		for _, dim := range []string{"pipelines", "assets", "connections", "integrations"} {
			if _, ok := match[dim]; !ok {
				match[dim] = []any{}
			}
		}
		if _, ok := match["operationFailuresOnly"]; !ok {
			match["operationFailuresOnly"] = false
		}
		if _, ok := def["action"].(map[string]any); !ok {
			def["action"] = map[string]any{}
		}
		if a, ok := def["action"].(map[string]any); ok {
			if _, ok := a["mute"]; !ok {
				a["mute"] = false
			}
			if _, ok := a["subscribers"]; !ok {
				a["subscribers"] = []any{}
			}
			al, _ := a["alerts"].(map[string]any)
			if al == nil {
				al = map[string]any{}
				a["alerts"] = al
			}
			if _, ok := al["alsoSendToPipelineDestinations"]; !ok {
				al["alsoSendToPipelineDestinations"] = false
			}
		}
		return map[string]any{
			"monitorId": id, "definition": def,
			"createdAt": "2026-01-01T00:00:00Z", "createdBy": "00000000-0000-0000-0000-000000000001",
			"updatedAt": "2026-01-02T00:00:00Z", "updatedBy": "00000000-0000-0000-0000-000000000001",
		}
	}

	absorb := func() {
		if raw := s.last().RawBody; raw != "" {
			stored = json.RawMessage(raw)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/monitors", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		absorb()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(response())
	})
	mux.HandleFunc("/monitors/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPut {
			absorb()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
		http.Error(w, "unhandled", http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func monitorValue(t *testing.T, objType tftypes.Object, name, channel string) tftypes.Value {
	t.Helper()
	return buildValue(t, objType, map[string]any{
		"name":     name,
		"group_by": []any{"pipeline"},
		"match": map[string]any{
			"integrations": []any{"SNOWFLAKE", "DBT"},
		},
		"action": map[string]any{
			"severity":    "HIGH",
			"subscribers": []any{"00000000-0000-0000-0000-000000000002"},
			"alerts": map[string]any{
				"destinations": []any{
					map[string]any{"integration": "SLACK", "destination": channel},
				},
			},
		},
	})
}

// TestMonitorLifecycle covers create, in-place update, read and delete, and —
// through planAndApply — that no server-defaulted field breaks the apply and
// that an edit never plans a replacement.
func TestMonitorLifecycle(t *testing.T) {
	const id = "77777777-7777-7777-7777-777777777777"
	calls, baseURL := monitorStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_monitor"].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas["orchestra_monitor"])

	created := planAndApply(t, server, "orchestra_monitor", objType,
		tftypes.NewValue(objType, nil), monitorValue(t, objType, "warehouse", "#a"), comp)

	create := calls.find(http.MethodPost, "/monitors")
	if create == nil {
		t.Fatalf("no POST /monitors; calls were %v", calls.paths())
	}
	// The definition is the request body itself, not wrapped.
	if create.Body["name"] != "warehouse" {
		t.Errorf("create body is not the definition: %#v", create.Body)
	}
	match, ok := create.Body["match"].(map[string]any)
	if !ok || match["integrations"] == nil {
		t.Errorf("create body lost `match`: %#v", create.Body)
	}
	action, _ := create.Body["action"].(map[string]any)
	if got, _ := action["subscribers"].([]any); len(got) != 1 || got[0] != "00000000-0000-0000-0000-000000000002" {
		t.Errorf("create body sent subscribers %#v, want the one configured", action["subscribers"])
	}

	// --- update ---
	planAndApply(t, server, "orchestra_monitor", objType, created,
		monitorValue(t, objType, "warehouse", "#b"), comp)
	if calls.find(http.MethodPut, "/monitors/"+id) == nil {
		t.Errorf("update did not PUT by ID; calls were %v", calls.paths())
	}

	// --- read ---
	priorDV, err := tfprotov6.NewDynamicValue(objType, created)
	if err != nil {
		t.Fatalf("prior state: %v", err)
	}
	readResp, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
		TypeName: "orchestra_monitor", CurrentState: &priorDV,
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	failOnError(t, "read", readResp.Diagnostics)
	if calls.find(http.MethodGet, "/monitors/"+id) == nil {
		t.Errorf("read did not GET by ID; calls were %v", calls.paths())
	}

	// --- delete ---
	nullDV, err := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, nil))
	if err != nil {
		t.Fatalf("null state: %v", err)
	}
	deleteResp, err := server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: "orchestra_monitor", PriorState: &priorDV, PlannedState: &nullDV, Config: &nullDV,
	})
	if err != nil {
		t.Fatalf("ApplyResourceChange (delete): %v", err)
	}
	failOnError(t, "delete", deleteResp.Diagnostics)
	if calls.find(http.MethodDelete, "/monitors/"+id) == nil {
		t.Errorf("delete did not DELETE by ID; calls were %v", calls.paths())
	}
}

// TestMonitorRejectsEmptyMatch covers the rule the schema cannot express: a
// monitor naming no dimension would claim every event on the account.
func TestMonitorRejectsEmptyMatch(t *testing.T) {
	_, baseURL := monitorStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_monitor"].ValueType().(tftypes.Object)
	config := buildValue(t, objType, map[string]any{
		"name":     "catch everything",
		"group_by": []any{"pipeline"},
		"match":    map[string]any{},
		"action":   map[string]any{"subscribers": unknown}, // must not switch the check off
	})
	configDV, err := tfprotov6.NewDynamicValue(objType, config)
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	resp, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
		TypeName: "orchestra_monitor", Config: &configDV,
	})
	if err != nil {
		t.Fatalf("ValidateResourceConfig: %v", err)
	}
	var got string
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			got += d.Summary
		}
	}
	if !strings.Contains(got, "matches everything") {
		t.Errorf("an empty match was accepted; diagnostics were %q", got)
	}
}

// TestMonitorGroupByLabels drives the GroupByLabel half of the spec's
// anyOf[MonitorGroupByEnum, GroupByLabel] through plan and apply. The two forms
// are separate Terraform attributes merged onto one wire list, so this checks
// the merge, the encoding, and that both come back in the right attribute.
func TestMonitorGroupByLabels(t *testing.T) {
	calls, baseURL := monitorStub(t)
	server, schemaResp := newServer(t, baseURL)

	objType := schemaResp.ResourceSchemas["orchestra_monitor"].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas["orchestra_monitor"])

	config := buildValue(t, objType, map[string]any{
		"name":     "per customer",
		"group_by": []any{"pipeline"},
		"group_by_labels": []any{
			map[string]any{"label": "customer", "path": "$.meta.customer"},
		},
		"match": map[string]any{"integrations": []any{"SNOWFLAKE"}},
	})

	state := planAndApply(t, server, "orchestra_monitor", objType,
		tftypes.NewValue(objType, nil), config, comp)

	create := calls.find(http.MethodPost, "/monitors")
	if create == nil {
		t.Fatalf("no POST /monitors; calls were %v", calls.paths())
	}

	// On the wire it is one list: a bare string for the dimension, an object
	// for the label.
	raw, ok := rawField(create.RawBody, "groupBy")
	if !ok {
		t.Fatalf("create body has no groupBy: %s", create.RawBody)
	}
	const want = `["pipeline",{"label":"customer","path":"$.meta.customer"}]`
	if string(raw) != want {
		t.Errorf("groupBy encoded as %s, want %s", raw, want)
	}

	// And it splits back into the two attributes rather than landing in one.
	rendered := state.String()
	for _, fragment := range []string{`"customer"`, `"$.meta.customer"`, `"pipeline"`} {
		if !strings.Contains(rendered, fragment) {
			t.Errorf("state lost %s", fragment)
		}
	}
}

// TestMonitorRejectsNoGrouping covers the combined bound: the spec requires at
// least one groupBy entry, and here that is spread across two attributes.
func TestMonitorRejectsNoGrouping(t *testing.T) {
	_, baseURL := monitorStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	objType := schemaResp.ResourceSchemas["orchestra_monitor"].ValueType().(tftypes.Object)
	config := buildValue(t, objType, map[string]any{
		"name":  "no grouping",
		"match": map[string]any{"integrations": []any{"SNOWFLAKE"}},
	})
	configDV, err := tfprotov6.NewDynamicValue(objType, config)
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	resp, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
		TypeName: "orchestra_monitor", Config: &configDV,
	})
	if err != nil {
		t.Fatalf("ValidateResourceConfig: %v", err)
	}
	var got string
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			got += d.Summary
		}
	}
	if !strings.Contains(got, "Nothing to group by") {
		t.Errorf("a monitor with no grouping was accepted; diagnostics were %q", got)
	}
}

// TestConstraintsTightenedByTheAPI covers two limits the API added after the
// provider shipped: a pipeline's configuration.timeout is capped at 72 hours,
// and a monitor's name at 100 characters. Without a plan-time validator the
// first sign of either is a 422 in the middle of an apply; with one it is a
// precise error at plan time. The boundaries are tested on both sides because
// an off-by-one here either rejects a valid value or lets an invalid one through.
func TestConstraintsTightenedByTheAPI(t *testing.T) {
	_, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()

	validate := func(t *testing.T, typeName string, values map[string]any) string {
		t.Helper()
		objType := schemaResp.ResourceSchemas[typeName].ValueType().(tftypes.Object)
		configDV, err := tfprotov6.NewDynamicValue(objType, buildValue(t, objType, values))
		if err != nil {
			t.Fatalf("config: %v", err)
		}
		resp, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
			TypeName: typeName, Config: &configDV,
		})
		if err != nil {
			t.Fatalf("ValidateResourceConfig: %v", err)
		}
		var errs string
		for _, d := range resp.Diagnostics {
			if d.Severity == tfprotov6.DiagnosticSeverityError {
				errs += d.Summary + " " + d.Detail + " "
			}
		}
		return errs
	}

	pipeline := func(timeout int) map[string]any {
		return map[string]any{
			"alias": "p", "published": true,
			"data": map[string]any{
				"name":          "p",
				"configuration": map[string]any{"timeout": timeout},
				"tasks": map[string]any{"t": map[string]any{
					"integration": "FIVETRAN", "integration_job": "FIVETRAN_SYNC_ALL",
					"parameters": `{"connector_id":"x"}`,
				}},
			},
		}
	}
	monitor := func(name string) map[string]any {
		return map[string]any{
			"name":     name,
			"group_by": []any{"pipeline"},
			"match":    map[string]any{"integrations": []any{"SNOWFLAKE"}},
		}
	}

	for _, tc := range []struct {
		name     string
		typeName string
		values   map[string]any
		wantErr  bool
	}{
		{"timeout at the 72h cap", "orchestra_pipeline", pipeline(259200), false},
		{"timeout one second over", "orchestra_pipeline", pipeline(259201), true},
		{"timeout of zero", "orchestra_pipeline", pipeline(0), false},
		{"negative timeout", "orchestra_pipeline", pipeline(-1), true},
		{"monitor name of 100 characters", "orchestra_monitor", monitor(strings.Repeat("a", 100)), false},
		{"monitor name of 101 characters", "orchestra_monitor", monitor(strings.Repeat("a", 101)), true},
		{"empty monitor name", "orchestra_monitor", monitor(""), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := validate(t, tc.typeName, tc.values)
			if tc.wantErr && got == "" {
				t.Error("an out-of-range value was accepted at plan time; the API would reject it mid-apply")
			}
			if !tc.wantErr && got != "" {
				t.Errorf("a valid value was rejected: %s", got)
			}
		})
	}
}

func pausedConfig(t *testing.T, objType tftypes.Object, paused any) tftypes.Value {
	t.Helper()
	cfg := map[string]any{
		"alias":     "daily_sales",
		"published": true,
		"data": map[string]any{
			"name": "Daily sales",
			"tasks": map[string]any{
				"ingest": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{"connector_id":"abc"}`,
					// Its numerics are Computed, so they go unknown in any
					// plan that changes something; a naive comparison of
					// plan and state then sees a definition change.
					"anomalies": []any{
						map[string]any{"type": "task_duration_above_baseline"},
					},
				},
			},
		},
	}
	if paused != nil {
		cfg["paused"] = paused
	}
	return buildValue(t, objType, cfg)
}

// TestPipelinePausedOnCreate covers the follow-up call: POST /pipelines has no
// paused field, so a pipeline created paused needs the pause endpoint too.
func TestPipelinePausedOnCreate(t *testing.T) {
	calls, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)
	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)

	state := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil),
		pausedConfig(t, objType, true), computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"]))

	pause := calls.find(http.MethodPut, "/pipelines/33333333-3333-3333-3333-333333333333/pause")
	if pause == nil {
		t.Fatalf("no PUT /pipelines/{id}/pause; calls were %v", calls.paths())
	}
	if pause.Body["paused"] != true {
		t.Errorf("pause body was %#v, want paused: true", pause.Body)
	}
	if !strings.Contains(state.String(), `"paused":tftypes.Bool<"true">`) {
		t.Errorf("state is not paused: %s", state)
	}
}

// TestPipelinePauseOnlySkipsDefinitionUpdate pins two things: leaving `paused`
// unset never calls the pause endpoint, and changing only `paused` does not
// PUT the definition, which would save a new version for nothing.
func TestPipelinePauseOnlySkipsDefinitionUpdate(t *testing.T) {
	calls, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)
	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"])

	created := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil),
		pausedConfig(t, objType, nil), comp)
	for _, c := range calls.paths() {
		if strings.HasSuffix(c, "/pause") {
			t.Fatalf("`paused` was unset, yet create called %s", c)
		}
	}

	planAndApply(t, server, "orchestra_pipeline", objType, created, pausedConfig(t, objType, true), comp)

	if calls.find(http.MethodPut, "/pipelines") != nil {
		t.Errorf("a pause-only change sent PUT /pipelines; calls were %v", calls.paths())
	}
	if calls.find(http.MethodPut, "/pipelines/33333333-3333-3333-3333-333333333333/pause") == nil {
		t.Errorf("no pause call; calls were %v", calls.paths())
	}
}

// incidentSettingsStub is a workspace that already has incident settings, some
// of them away from the spec's defaults. PATCH merges, as the spec says: an
// omitted setting keeps its stored value.
func incidentSettingsStub(t *testing.T) (*stub, string) {
	t.Helper()
	s := &stub{}
	stored := map[string]any{
		"incidentsEnabled": true, "requireAlertConfigured": true, "environmentIds": []any{},
		"requireDefaultBranchOrPublishedVersion": true,
		"warningsEnabled":                        false, "groupWarningWithFailed": false, "autoResolveOnSuccess": false,
		"maxAlertsPerIncident": 3, "alertBudgetRefillCadence": "weekly", "autoDiagnoseIncidents": false,
		"updatedAt": "2026-01-01T00:00:00Z",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/incident_settings", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		switch r.Method {
		case http.MethodPatch:
			for k, v := range body {
				stored[k] = v
			}
		case http.MethodGet:
		default:
			t.Errorf("unexpected %s /incident_settings", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stored)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return s, srv.URL
}

// TestIncidentSettingsLifecycle pins the singleton's contract: only configured
// settings are sent, the rest keep what the workspace already had, and destroy
// touches nothing.
func TestIncidentSettingsLifecycle(t *testing.T) {
	calls, baseURL := incidentSettingsStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()
	const name = "orchestra_incident_settings"
	objType := schemaResp.ResourceSchemas[name].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas[name])

	created := planAndApply(t, server, name, objType, tftypes.NewValue(objType, nil),
		buildValue(t, objType, map[string]any{"max_alerts_per_incident": 5}), comp)

	patch := calls.find(http.MethodPatch, "/incident_settings")
	if patch == nil {
		t.Fatalf("no PATCH /incident_settings; calls were %v", calls.paths())
	}
	if len(patch.Body) != 1 || patch.Body["maxAlertsPerIncident"] != float64(5) {
		t.Errorf("create sent %v; want only the configured maxAlertsPerIncident, so the rest keep their values", patch.Body)
	}
	for _, want := range []string{`"incidents_enabled":tftypes.Bool<"true">`, `"alert_budget_refill_cadence":tftypes.String<"weekly">`, `"id":tftypes.String<"incident_settings">`} {
		if !strings.Contains(created.String(), want) {
			t.Errorf("state is missing %s, which the workspace already had: %s", want, created)
		}
	}

	planAndApply(t, server, name, objType, created, buildValue(t, objType, map[string]any{
		"max_alerts_per_incident": 6,
		"auto_resolve_on_success": true,
		"environment_ids":         []any{"44444444-4444-4444-4444-444444444444"},
	}), comp)

	priorDV, _ := tfprotov6.NewDynamicValue(objType, created)
	nullDV, _ := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, nil))
	before := len(calls.paths())
	resp, err := server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: name, PriorState: &priorDV, PlannedState: &nullDV, Config: &nullDV,
	})
	if err != nil {
		t.Fatalf("ApplyResourceChange (delete): %v", err)
	}
	failOnError(t, "delete", resp.Diagnostics)
	if got := calls.paths()[before:]; len(got) != 0 {
		t.Errorf("destroy called %v; it should only forget the settings", got)
	}
}

// monitorOrderStub holds three monitors and enforces what the real
// POST /monitors/reorder does: the list must name every monitor exactly once,
// or it answers 400. race makes a monitor appear just before the next reorder
// is checked, as one created elsewhere between the provider's read and its
// reorder would.
func monitorOrderStub(t *testing.T) (s *stub, url string, reorderInUI func(ids ...string), race func(id string)) {
	t.Helper()
	s = &stub{}
	order := []string{"m1", "m2", "m3"}
	appearing := ""
	list := func() []any {
		out := []any{}
		for i, id := range order {
			out = append(out, map[string]any{
				"monitorId": id, "createdAt": "2026-01-01T00:00:00Z", "createdBy": "u",
				"updatedAt": "2026-01-01T00:00:00Z", "updatedBy": "u",
				"definition": map[string]any{
					"name": id, "enabled": true, "order": i,
					"match": map[string]any{"integrations": []any{"SNOWFLAKE"}}, "groupBy": []any{"pipeline"},
				},
			})
		}
		return out
	}
	write := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /monitors", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusOK, list())
	})
	mux.HandleFunc("POST /monitors/reorder", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		if appearing != "" {
			order, appearing = append(order, appearing), ""
		}
		var ids []string
		for _, id := range body["monitorIds"].([]any) {
			ids = append(ids, id.(string))
		}
		if len(ids) != len(order) || !sameMembers(ids, order) {
			write(w, http.StatusBadRequest, map[string]any{"detail": "monitor_ids must name every monitor on the account exactly once."})
			return
		}
		order = ids
		write(w, http.StatusOK, list())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return s, srv.URL, func(ids ...string) { order = ids }, func(id string) { appearing = id }
}

func sameMembers(a, b []string) bool {
	seen := map[string]int{}
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		seen[v]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// TestMonitorOrder covers the resource's contract: the configured monitors go
// first and the rest follow, a monitor moved in front of them shows as drift,
// and destroy changes nothing.
func TestMonitorOrder(t *testing.T) {
	calls, baseURL, reorderInUI, _ := monitorOrderStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()
	const name = "orchestra_monitor_order"
	objType := schemaResp.ResourceSchemas[name].ValueType().(tftypes.Object)

	created := planAndApply(t, server, name, objType, tftypes.NewValue(objType, nil),
		buildValue(t, objType, map[string]any{"monitor_ids": []any{"m3", "m1"}}),
		computedAttrs(schemaResp.ResourceSchemas[name]))

	reorder := calls.find(http.MethodPost, "/monitors/reorder")
	if reorder == nil {
		t.Fatalf("no POST /monitors/reorder; calls were %v", calls.paths())
	}
	if got := reorder.RawBody; got != `{"monitorIds":["m3","m1","m2"]}` {
		t.Errorf("reorder sent %s; want the configured monitors first and m2 after them", got)
	}

	// Someone moves m2 to the top in the UI.
	reorderInUI("m2", "m3", "m1")
	current, _ := tfprotov6.NewDynamicValue(objType, created)
	read, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{TypeName: name, CurrentState: &current})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	failOnError(t, "read", read.Diagnostics)
	refreshed, _ := read.NewState.Unmarshal(objType)
	if want := `"monitor_ids":tftypes.List[tftypes.String]<tftypes.String<"m2">, tftypes.String<"m3">, tftypes.String<"m1">>`; !strings.Contains(refreshed.String(), want) {
		t.Errorf("a monitor moved in front should show as drift; state is %s", refreshed)
	}

	before := len(calls.paths())
	nullDV, _ := tfprotov6.NewDynamicValue(objType, tftypes.NewValue(objType, nil))
	resp, err := server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: name, PriorState: &current, PlannedState: &nullDV, Config: &nullDV,
	})
	if err != nil {
		t.Fatalf("ApplyResourceChange (delete): %v", err)
	}
	failOnError(t, "delete", resp.Diagnostics)
	if got := calls.paths()[before:]; len(got) != 0 {
		t.Errorf("destroy called %v; it should leave the order as it is", got)
	}
}

// TestMonitorWithoutAction covers the shape TestAccMonitorOrder_lifecycle hit
// live: a monitor declared without `action` comes back with a default one, so
// planning null for it aborts the apply.
func TestMonitorWithoutAction(t *testing.T) {
	calls, baseURL := monitorStub(t)
	server, schemaResp := newServer(t, baseURL)
	objType := schemaResp.ResourceSchemas["orchestra_monitor"].ValueType().(tftypes.Object)

	planAndApply(t, server, "orchestra_monitor", objType, tftypes.NewValue(objType, nil),
		buildValue(t, objType, map[string]any{
			"name":     "No action",
			"match":    map[string]any{"integrations": []any{"SNOWFLAKE"}},
			"group_by": []any{"pipeline"},
		}), computedAttrs(schemaResp.ResourceSchemas["orchestra_monitor"]))

	create := calls.find(http.MethodPost, "/monitors")
	if create == nil {
		t.Fatalf("no POST /monitors; calls were %v", calls.paths())
	}
	// The stub echoes a null back, and the API rejects one, so only the body shows [] was sent.
	action, _ := create.Body["action"].(map[string]any)
	if got, ok := action["subscribers"].([]any); !ok || len(got) != 0 {
		t.Errorf("subscribers sent as %#v, want []", action["subscribers"])
	}
	match, _ := create.Body["match"].(map[string]any)
	if got, ok := match["operationFailuresOnly"].(bool); !ok || got {
		t.Errorf("operationFailuresOnly sent as %#v, want false", match["operationFailuresOnly"])
	}
}

// TestMonitorOrderSurvivesANewMonitor covers the race between reading the
// monitors and reordering them. Terraform creates monitors the order does not
// depend on in parallel with it, so within one apply a monitor can appear in
// between, and the API rejects a reorder that does not name it.
func TestMonitorOrderSurvivesANewMonitor(t *testing.T) {
	calls, baseURL, _, race := monitorOrderStub(t)
	server, schemaResp := newServer(t, baseURL)
	const name = "orchestra_monitor_order"
	objType := schemaResp.ResourceSchemas[name].ValueType().(tftypes.Object)

	race("m4")
	planAndApply(t, server, name, objType, tftypes.NewValue(objType, nil),
		buildValue(t, objType, map[string]any{"monitor_ids": []any{"m3", "m1"}}),
		computedAttrs(schemaResp.ResourceSchemas[name]))

	var reorders []string
	for _, p := range calls.paths() {
		if p == "POST /monitors/reorder" {
			reorders = append(reorders, p)
		}
	}
	if len(reorders) != 2 {
		t.Errorf("want one rejected reorder and one retry, got %d: %v", len(reorders), calls.paths())
	}
}

func folderConfig(t *testing.T, objType tftypes.Object, connector string, folder any) tftypes.Value {
	t.Helper()
	cfg := map[string]any{
		"alias":     "daily_sales",
		"published": true,
		"data": map[string]any{
			"name": "Daily sales",
			"tasks": map[string]any{
				"ingest": map[string]any{
					"integration":     "FIVETRAN",
					"integration_job": "FIVETRAN_SYNC_ALL",
					"parameters":      `{"connector_id":"` + connector + `"}`,
				},
			},
		},
	}
	if folder != nil {
		cfg["folder"] = folder
	}
	return buildValue(t, objType, cfg)
}

// TestPipelineFolder covers the folder's three paths: POST /pipelines takes it,
// a change goes through the folder endpoint without saving a new version, and
// leaving it unset never moves the pipeline. The API omits a folder it does not
// have, so `folder = ""` must survive that as "".
func TestPipelineFolder(t *testing.T) {
	calls, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)
	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"])
	const move = "/pipelines/33333333-3333-3333-3333-333333333333/folder"
	moves := func() (n int) {
		for _, c := range calls.paths() {
			if c == "PUT "+move {
				n++
			}
		}
		return n
	}

	created := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil),
		folderConfig(t, objType, "abc", "finance"), comp)
	if got := calls.find(http.MethodPost, "/pipelines").Body["folder"]; got != "finance" {
		t.Errorf("create sent folder %#v, want \"finance\"", got)
	}
	if moves() != 0 {
		t.Errorf("create moved the pipeline; POST /pipelines already set its folder")
	}

	moved := planAndApply(t, server, "orchestra_pipeline", objType, created,
		folderConfig(t, objType, "abc", "finance/daily"), comp)
	if calls.find(http.MethodPut, "/pipelines") != nil {
		t.Errorf("a folder-only change sent PUT /pipelines, saving a new version; calls were %v", calls.paths())
	}
	if c := calls.find(http.MethodPut, move); c == nil || c.Body["folder"] != "finance/daily" {
		t.Fatalf("no move to finance/daily; calls were %v", calls.paths())
	}

	// Unset, alongside a definition change: the folder is left alone.
	kept := planAndApply(t, server, "orchestra_pipeline", objType, moved,
		folderConfig(t, objType, "def", nil), comp)
	if moves() != 1 {
		t.Errorf("an unset folder moved the pipeline; calls were %v", calls.paths())
	}
	if !strings.Contains(kept.String(), `"folder":tftypes.String<"finance/daily">`) {
		t.Errorf("state lost the folder: %s", kept)
	}

	cleared := planAndApply(t, server, "orchestra_pipeline", objType, kept,
		folderConfig(t, objType, "def", ""), comp)
	if moves() != 2 {
		t.Errorf("clearing the folder did not move the pipeline; calls were %v", calls.paths())
	}
	if !strings.Contains(cleared.String(), `"folder":tftypes.String<"">`) {
		t.Errorf("state is not at the root: %s", cleared)
	}
}

// TestPipelineEmptyFolderAtRootIsANoOp covers a pipeline at the root, as one
// created or imported without a folder is, whose config gains `folder = ""`.
// The plan differs from state (null), but nothing needs moving: no call is made,
// and the update applies what state already held rather than blanking it.
func TestPipelineEmptyFolderAtRootIsANoOp(t *testing.T) {
	calls, baseURL := pipelineStub(t)
	server, schemaResp := newServer(t, baseURL)
	objType := schemaResp.ResourceSchemas["orchestra_pipeline"].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas["orchestra_pipeline"])

	created := planAndApply(t, server, "orchestra_pipeline", objType, tftypes.NewValue(objType, nil),
		folderConfig(t, objType, "abc", nil), comp)
	if _, sent := calls.find(http.MethodPost, "/pipelines").Body["folder"]; sent {
		t.Error("an unset folder was sent on create")
	}
	before := len(calls.paths())

	// planAndApply fails the test if created_at or name come back changed.
	state := planAndApply(t, server, "orchestra_pipeline", objType, created,
		folderConfig(t, objType, "abc", ""), comp)
	for _, c := range calls.paths()[before:] {
		if strings.HasPrefix(c, "PUT ") {
			t.Errorf("a no-op folder change called %s", c)
		}
	}
	if !strings.Contains(state.String(), `"folder":tftypes.String<"">`) {
		t.Errorf("state is not at the root: %s", state)
	}
}
