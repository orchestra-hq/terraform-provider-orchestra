package rsapply_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// connectionStub behaves as the live API does where it matters: it masks
// secrets in every response, rejects a PATCH that mixes field groups, and
// answers an OAuth integration (HUBSPOT here) with a redirect instead.
func connectionStub(t *testing.T) (*stub, string) {
	t.Helper()
	s := &stub{}
	conn := map[string]any{}

	write := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}
	masked := func() map[string]any {
		out := map[string]any{}
		for k, v := range conn {
			out[k] = v
		}
		params := map[string]any{}
		for k, v := range conn["parameters"].(map[string]any) {
			params[k] = v
		}
		if _, ok := params["bearer_token"]; ok {
			params["bearer_token"] = "*******k3n"
		}
		out["parameters"] = params
		return out
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /integrations/{integration}/auth", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		if r.PathValue("integration") == "HUBSPOT" {
			write(w, http.StatusCreated, map[string]any{"connectionId": "hubspot_1", "integration": "HUBSPOT",
				"accountId": "b7a2c6f1-3e4d-4b8a-9c21-5f6e7d8a9b0c", "redirectUrl": "https://app.hubspot.com/oauth/authorize"})
			return
		}
		conn = map[string]any{
			"connectionId": "http_api_1", "integration": r.PathValue("integration"), "connectionName": body["connectionName"],
			"isDefault": false, "credentialStore": "ORCHESTRA", "authStatus": "SUCCEEDED", "message": nil,
			"parameters": body["parameters"],
		}
		write(w, http.StatusCreated, masked())
	})
	mux.HandleFunc("GET /integrations/{integration}/auth/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusOK, masked())
	})
	mux.HandleFunc("PATCH /integrations/{integration}/auth/{id}", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		groups := 0
		for _, k := range []string{"connectionName", "isDefault", "parameters"} {
			if _, ok := body[k]; ok {
				groups++
			}
		}
		if _, ok := body["credentialStore"]; ok && body["parameters"] == nil || groups != 1 {
			write(w, http.StatusBadRequest, map[string]any{"detail": "Update one of connectionName, isDefault or parameters"})
			return
		}
		for k, v := range body {
			conn[k] = v
		}
		write(w, http.StatusOK, masked())
	})
	mux.HandleFunc("DELETE /integrations/{integration}/auth/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		w.WriteHeader(http.StatusNoContent)
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

func TestIntegrationConnectionLifecycle(t *testing.T) {
	calls, baseURL := connectionStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()
	const name = "orchestra_integration_connection"
	objType := schemaResp.ResourceSchemas[name].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas[name])
	params := func(token string) string {
		return `{"base_url":"https://example.com","auth_type":"BEARER_TOKEN","bearer_token":"` + token + `"}`
	}

	created := planAndApply(t, server, name, objType, tftypes.NewValue(objType, nil), buildValue(t, objType, map[string]any{
		"integration": "HTTP", "name": "API", "parameters": params("s3cret"), "is_default": true,
	}), comp)
	if p := calls.find(http.MethodPatch, "/integrations/HTTP/auth/http_api_1"); p == nil || p.RawBody != `{"isDefault":true}` {
		t.Errorf("is_default was not applied with a follow-up PATCH: %+v", p)
	}
	if !strings.Contains(created.String(), "s3cret") {
		t.Errorf("state lost the configured secret to the masked echo:\n%s", created)
	}

	// The stub rejects a PATCH that carries both, as the API does.
	planAndApply(t, server, name, objType, created, buildValue(t, objType, map[string]any{
		"integration": "HTTP", "name": "API v2", "parameters": params("n3w"), "is_default": true,
	}), comp)

	current, _ := tfprotov6.NewDynamicValue(objType, created)
	read, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{TypeName: name, CurrentState: &current})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	failOnError(t, "read", read.Diagnostics)
	refreshed, _ := read.NewState.Unmarshal(objType)
	if s := refreshed.String(); strings.Contains(s, "*******") || !strings.Contains(s, `"name":tftypes.String<"API v2">`) {
		t.Errorf("a refresh should read the name back and leave parameters alone:\n%s", s)
	}
}

func TestIntegrationConnectionOAuthIsUndone(t *testing.T) {
	calls, baseURL := connectionStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()
	const name = "orchestra_integration_connection"
	objType := schemaResp.ResourceSchemas[name].ValueType().(tftypes.Object)

	config := buildValue(t, objType, map[string]any{"integration": "HUBSPOT", "name": "CRM", "parameters": `{}`})
	null := tftypes.NewValue(objType, nil)
	priorDV, _ := tfprotov6.NewDynamicValue(objType, null)
	configDV, _ := tfprotov6.NewDynamicValue(objType, config)
	proposedDV, _ := tfprotov6.NewDynamicValue(objType, proposedNew(null, config, computedAttrs(schemaResp.ResourceSchemas[name])))
	plan, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: name, PriorState: &priorDV, ProposedNewState: &proposedDV, Config: &configDV,
	})
	if err != nil {
		t.Fatalf("PlanResourceChange: %v", err)
	}
	failOnError(t, "plan", plan.Diagnostics)
	applied, err := server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: name, PriorState: &priorDV, PlannedState: plan.PlannedState, Config: &configDV,
	})
	if err != nil {
		t.Fatalf("ApplyResourceChange: %v", err)
	}
	if len(applied.Diagnostics) == 0 || applied.Diagnostics[0].Severity != tfprotov6.DiagnosticSeverityError {
		t.Errorf("an OAuth integration should fail the apply, got %+v", applied.Diagnostics)
	}
	if calls.find(http.MethodDelete, "/integrations/HUBSPOT/auth/hubspot_1") == nil {
		t.Errorf("the half-made connection was not deleted; calls were %v", calls.paths())
	}
}

// parameters_wo reaches the API but never state, and a version bump re-sends
// it: Terraform cannot diff a value it does not keep.
func TestIntegrationConnectionWriteOnlyParameters(t *testing.T) {
	calls, baseURL := connectionStub(t)
	server, schemaResp := newServer(t, baseURL)
	const name = "orchestra_integration_connection"
	objType := schemaResp.ResourceSchemas[name].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas[name])
	config := func(token string, version int) tftypes.Value {
		return buildValue(t, objType, map[string]any{
			"integration": "HTTP", "name": "API", "parameters_wo_version": version,
			"parameters_wo": `{"base_url":"https://example.com","auth_type":"BEARER_TOKEN","bearer_token":"` + token + `"}`,
		})
	}

	created := planAndApply(t, server, name, objType, tftypes.NewValue(objType, nil), config("s3cret", 1), comp)
	if p := calls.find(http.MethodPost, "/integrations/HTTP/auth"); p == nil || !strings.Contains(p.RawBody, "s3cret") {
		t.Errorf("create did not send the write-only parameters: %+v", p)
	}
	if s := created.String(); strings.Contains(s, "s3cret") {
		t.Errorf("the write-only secret reached state:\n%s", s)
	}

	updated := planAndApply(t, server, name, objType, created, config("n3w", 2), comp)
	if p := calls.find(http.MethodPatch, "/integrations/HTTP/auth/http_api_1"); p == nil || !strings.Contains(p.RawBody, "n3w") {
		t.Errorf("a version bump did not re-send the write-only parameters: %+v", p)
	}
	if s := updated.String(); strings.Contains(s, "n3w") {
		t.Errorf("the write-only secret reached state:\n%s", s)
	}
}

func TestIntegrationConnectionNeedsExactlyOneParameters(t *testing.T) {
	_, baseURL := connectionStub(t)
	server, schemaResp := newServer(t, baseURL)
	objType := schemaResp.ResourceSchemas["orchestra_integration_connection"].ValueType().(tftypes.Object)

	for desc, tc := range map[string]struct {
		data    map[string]any
		wantErr bool
	}{
		"neither":    {map[string]any{"integration": "HTTP", "name": "API"}, true},
		"both":       {map[string]any{"integration": "HTTP", "name": "API", "parameters": `{}`, "parameters_wo": `{}`}, true},
		"parameters": {map[string]any{"integration": "HTTP", "name": "API", "parameters": `{}`}, false},
		"write-only": {map[string]any{"integration": "HTTP", "name": "API", "parameters_wo": `{}`}, false},
	} {
		configDV, _ := tfprotov6.NewDynamicValue(objType, buildValue(t, objType, tc.data))
		resp, err := server.ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{
			TypeName: "orchestra_integration_connection", Config: &configDV,
			ClientCapabilities: &tfprotov6.ValidateResourceConfigClientCapabilities{WriteOnlyAttributesAllowed: true},
		})
		if err != nil {
			t.Fatalf("ValidateResourceConfig: %v", err)
		}
		gotErr := false
		for _, d := range resp.Diagnostics {
			gotErr = gotErr || d.Severity == tfprotov6.DiagnosticSeverityError
		}
		if gotErr != tc.wantErr {
			t.Errorf("%s: got diagnostics %+v, want error %v", desc, resp.Diagnostics, tc.wantErr)
		}
	}
}
