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

// agentsStub serves the Agents API from the stub's root, which is where the
// provider sends it when base_url is not the Engine API's path.
//
// It fills in the defaults the live API does: an empty prompt list and the
// workspace's LLM key. A model is not one of them; the live API stores none
// until one is set. It masks secrets in responses, so a provider that read
// them back would fail.
func agentsStub(t *testing.T) (*stub, string) {
	t.Helper()
	s := &stub{}
	agent := map[string]any{}
	attachedSkills, attachedIntegrations := []any{}, []any{}
	skills := map[string]map[string]any{}

	write := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}
	page := func(results []any) map[string]any {
		return map[string]any{"page": 1, "pageSize": 100, "total": len(results), "results": results}
	}
	agentResponse := func() map[string]any {
		out := map[string]any{}
		for k, v := range agent {
			out[k] = v
		}
		if out["secrets"] != nil {
			out["secrets"] = map[string]any{"TOKEN": "****"}
		}
		return out
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /agents", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		agent = map[string]any{
			"agentId":       "aaaaaaaa-0000-0000-0000-000000000001",
			"samplePrompts": []any{}, "gitEnabled": false, "llmKeyType": map[string]any{"type": "ACCOUNT_DEFAULT"},
			"allowedEnvironmentIds": []any{},
		}
		for k, v := range body {
			if v != nil {
				agent[k] = v
			}
		}
		write(w, http.StatusCreated, agentResponse())
	})
	mux.HandleFunc("GET /agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusOK, agentResponse())
	})
	mux.HandleFunc("PATCH /agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		// An explicit null clears a field; an omitted one keeps its value.
		for k, v := range s.record(r) {
			agent[k] = v
		}
		write(w, http.StatusOK, agentResponse())
	})
	mux.HandleFunc("DELETE /agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusNoContent, nil)
	})
	mux.HandleFunc("PUT /agents/{id}/skills", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		_ = json.Unmarshal([]byte(s.last().RawBody), &attachedSkills)
		write(w, http.StatusNoContent, nil)
	})
	mux.HandleFunc("GET /agents/{id}/skills", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusOK, page(attachedSkills))
	})
	mux.HandleFunc("PUT /agents/{id}/integrations", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		_ = json.Unmarshal([]byte(s.last().RawBody), &attachedIntegrations)
		write(w, http.StatusOK, attachedIntegrations)
	})
	mux.HandleFunc("GET /agents/{id}/integrations", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusOK, page(attachedIntegrations))
	})

	skillResponse := func(id string) map[string]any {
		return skills[id]
	}
	mux.HandleFunc("POST /skills", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		skills["s1"] = map[string]any{"skillId": "s1", "name": "triage", "description": "Triage failures",
			"markdown": body["markdown"], "storageProvider": "ORCHESTRA"}
		write(w, http.StatusCreated, skillResponse("s1"))
	})
	mux.HandleFunc("POST /skills/import", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		skills["s2"] = map[string]any{"skillId": "s2", "name": "from-git", "description": "Synced",
			"markdown": "---\nname: from-git\n---\nsynced, and changes on every sync", "storageProvider": "GITHUB",
			"repository": body["repository"], "defaultBranch": body["defaultBranch"], "gitPath": body["path"]}
		write(w, http.StatusCreated, skillResponse("s2"))
	})
	mux.HandleFunc("GET /skills/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		write(w, http.StatusOK, skillResponse(r.PathValue("id")))
	})
	mux.HandleFunc("PATCH /skills/{id}", func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		skills[r.PathValue("id")]["markdown"] = body["markdown"]
		write(w, http.StatusOK, skillResponse(r.PathValue("id")))
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

func TestAgentLifecycle(t *testing.T) {
	calls, baseURL := agentsStub(t)
	server, schemaResp := newServer(t, baseURL)
	const name = "orchestra_agent"
	objType := schemaResp.ResourceSchemas[name].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas[name])

	created := planAndApply(t, server, name, objType, tftypes.NewValue(objType, nil), buildValue(t, objType, map[string]any{
		"name":                    "Triage",
		"description":             "Looks into failures",
		"secrets":                 map[string]any{"TOKEN": "s3cret"},
		"skill_ids":               []any{"s1"},
		"allowed_environment_ids": []any{"11111111-1111-4111-8111-111111111111"},
	}), comp)

	create := calls.find(http.MethodPost, "/agents")
	if create == nil {
		t.Fatalf("no POST /agents; calls were %v", calls.paths())
	}
	if _, sent := create.Body["defaultModelId"]; sent {
		t.Errorf("an unset model was sent as %v; leaving it out lets Orchestra pick", create.Body["defaultModelId"])
	}
	if ids, _ := create.Body["allowedEnvironmentIds"].([]any); len(ids) != 1 || ids[0] != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("allowedEnvironmentIds was sent as %v", create.Body["allowedEnvironmentIds"])
	}
	if got := calls.find(http.MethodPut, "/agents/aaaaaaaa-0000-0000-0000-000000000001/skills"); got == nil || got.RawBody != `[{"skillId":"s1"}]` {
		t.Errorf("skills were not attached as configured: %+v", got)
	}
	state := created.String()
	for _, want := range []string{
		`"default_model_id":tftypes.String<null>`,      // unset, and not sent as ""
		`"TOKEN":tftypes.String<"s3cret">`,             // not the masked echo
		`"integrations":tftypes.Set[tftypes.String]<>`, // unset, so read back
	} {
		if !strings.Contains(state, want) {
			t.Errorf("state is missing %s:\n%s", want, state)
		}
	}

	// Removing the description must clear it, not leave the old one.
	planAndApply(t, server, name, objType, created, buildValue(t, objType, map[string]any{
		"name":      "Triage",
		"secrets":   map[string]any{"TOKEN": "s3cret"},
		"skill_ids": []any{},
	}), comp)
	patch := calls.find(http.MethodPatch, "/agents/aaaaaaaa-0000-0000-0000-000000000001")
	if patch == nil {
		t.Fatalf("no PATCH; calls were %v", calls.paths())
	}
	if v, sent := patch.Body["description"]; !sent || v != nil {
		t.Errorf("description was sent as %v (sent: %v); a removed one must be sent as null to clear it", v, sent)
	}
	// Dropping the attribute resets it to the default, which must be sent so the restriction is lifted.
	if ids, sent := patch.Body["allowedEnvironmentIds"].([]any); !sent || len(ids) != 0 {
		t.Errorf("allowedEnvironmentIds was sent as %v; removing it must send []", patch.Body["allowedEnvironmentIds"])
	}
}

func TestSkillLifecycle(t *testing.T) {
	calls, baseURL := agentsStub(t)
	server, schemaResp := newServer(t, baseURL)
	ctx := context.Background()
	const name = "orchestra_skill"
	objType := schemaResp.ResourceSchemas[name].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas[name])

	doc := func(body string) string { return "---\nname: triage\ndescription: Triage failures\n---\n" + body }
	created := planAndApply(t, server, name, objType, tftypes.NewValue(objType, nil),
		buildValue(t, objType, map[string]any{"markdown": doc("v1")}), comp)
	planAndApply(t, server, name, objType, created, buildValue(t, objType, map[string]any{"markdown": doc("v2")}), comp)
	if p := calls.find(http.MethodPatch, "/skills/s1"); p == nil || p.Body["markdown"] != doc("v2") {
		t.Errorf("the edit was not sent as a PATCH of the document: %+v", p)
	}

	gitSkill := planAndApply(t, server, name, objType, tftypes.NewValue(objType, nil), buildValue(t, objType, map[string]any{
		"git": map[string]any{"repository": "acme/skills", "default_branch": "main", "path": "triage.md"},
	}), comp)
	if calls.find(http.MethodPost, "/skills/import") == nil {
		t.Fatalf("a git skill was not imported; calls were %v", calls.paths())
	}

	// A git-backed skill's document changes on every sync, so a refresh must
	// not pull it into `markdown`, where it would show as drift.
	current, _ := tfprotov6.NewDynamicValue(objType, gitSkill)
	read, err := server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{TypeName: name, CurrentState: &current})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	failOnError(t, "read", read.Diagnostics)
	refreshed, _ := read.NewState.Unmarshal(objType)
	if !strings.Contains(refreshed.String(), `"markdown":tftypes.String<null>`) {
		t.Errorf("a git skill's synced document leaked into markdown: %s", refreshed)
	}
}

// secrets_wo reaches the API but never state, and an unrelated update still
// sends it: every PATCH carries secrets, so a missing value would clear them.
func TestAgentWriteOnlySecrets(t *testing.T) {
	calls, baseURL := agentsStub(t)
	server, schemaResp := newServer(t, baseURL)
	const name = "orchestra_agent"
	objType := schemaResp.ResourceSchemas[name].ValueType().(tftypes.Object)
	comp := computedAttrs(schemaResp.ResourceSchemas[name])
	config := func(agentName string) tftypes.Value {
		return buildValue(t, objType, map[string]any{
			"name": agentName, "secrets_wo": map[string]any{"TOKEN": "s3cret"}, "secrets_wo_version": 1,
		})
	}

	created := planAndApply(t, server, name, objType, tftypes.NewValue(objType, nil), config("Triage"), comp)
	if p := calls.find(http.MethodPost, "/agents"); p == nil || !strings.Contains(p.RawBody, `"TOKEN":"s3cret"`) {
		t.Errorf("create did not send the write-only secrets: %+v", p)
	}
	if s := created.String(); strings.Contains(s, "s3cret") {
		t.Errorf("the write-only secret reached state:\n%s", s)
	}

	planAndApply(t, server, name, objType, created, config("Triage v2"), comp)
	if p := calls.find(http.MethodPatch, "/agents/aaaaaaaa-0000-0000-0000-000000000001"); p == nil || !strings.Contains(p.RawBody, `"TOKEN":"s3cret"`) {
		t.Errorf("a rename did not re-send the write-only secrets, which would clear them: %+v", p)
	}
}
