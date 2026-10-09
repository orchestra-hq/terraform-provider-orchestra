package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func doc(t *testing.T, body string) specDoc {
	t.Helper()
	d, err := parseSpec([]byte("openapi: 3.1.0\ninfo: {title: t, version: v1}\n" + body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

func ledgerOf(spec string, ops ...string) ledger {
	return ledger{spec: {"excluded": {Reason: "test", Operations: ops}}}
}

// monitorSpec is a one-resource spec shaped like the monitors API, with the
// groupBy and name properties the real spec has changed under the provider.
func monitorSpec(groupBy, name string) string {
	return `
paths:
  /monitors:
    post:
      summary: Create a monitor
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/Monitor'}
      responses:
        '201':
          description: ok
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Monitor'}
components:
  schemas:
    Monitor:
      type: object
      required: [name]
      properties:
        name: ` + name + `
        groupBy: ` + groupBy + `
    GroupBy: {type: string, enum: [pipeline, task]}
    GroupByLabel:
      type: object
      properties: {label: {type: string}, path: {type: string}}
`
}

func find(r *Report, t tier, group, subjectContains string) *finding {
	for i := range r.findings {
		f := &r.findings[i]
		if f.tier == t && f.group == group && strings.Contains(f.subject, subjectContains) {
			return f
		}
	}
	return nil
}

func dump(r *Report) string { return r.Markdown() }

// TestUnionReplacingAList is the bug that motivated the tool. groupBy went from
// a list of names to a list of names-or-objects, the provider's []string could
// no longer decode a monitor that used the new form, and nothing failed.
func TestUnionReplacingAList(t *testing.T) {
	old := doc(t, monitorSpec(`{type: array, items: {$ref: '#/components/schemas/GroupBy'}}`, `{type: string}`))
	cur := doc(t, monitorSpec(
		`{type: array, items: {anyOf: [{$ref: '#/components/schemas/GroupBy'}, {$ref: '#/components/schemas/GroupByLabel'}]}}`,
		`{type: string}`))

	r := Compare(Inputs{Old: old, New: cur, Ledger: ledger{}})

	f := find(r, attention, groupShape, "Monitor.groupBy")
	if f == nil {
		t.Fatalf("a property turning into a union was not flagged:\n%s", dump(r))
	}
	if !strings.Contains(strings.Join(f.notes, "\n"), "written by `POST /monitors`") {
		t.Errorf("the finding does not say which endpoint writes it: %v", f.notes)
	}
	if !r.NeedsAttention() {
		t.Error("NeedsAttention is false for a shape change")
	}
}

// TestTightenedConstraint covers maxLength appearing on monitor names and a
// 72-hour cap on timeouts: the API starts rejecting what it used to accept, and
// only a plan-time validator turns that into a useful error.
func TestTightenedConstraint(t *testing.T) {
	old := doc(t, monitorSpec(`{type: array, items: {type: string}}`, `{type: string, minLength: 1}`))
	cur := doc(t, monitorSpec(`{type: array, items: {type: string}}`, `{type: string, minLength: 1, maxLength: 100}`))

	r := Compare(Inputs{Old: old, New: cur, Ledger: ledger{}})

	if find(r, attention, groupConstr, "Monitor.name") == nil {
		t.Fatalf("a new maxLength was not flagged as a constraint change:\n%s", dump(r))
	}
	if find(r, attention, groupShape, "Monitor.name") != nil {
		t.Error("a constraint-only change was reported as a shape change")
	}
}

// TestDocumentationChurnIsIgnored: the docs site rewrote ~1300 lines of the
// spec in one refresh and none of it mattered.
func TestDocumentationChurnIsIgnored(t *testing.T) {
	old := doc(t, monitorSpec(`{type: array, items: {type: string}, description: old words}`, `{type: string, title: Old, example: a}`))
	cur := doc(t, monitorSpec(`{type: array, items: {type: string}, description: new words}`, `{type: string, title: New, example: b}`))

	r := Compare(Inputs{Old: old, New: cur, Ledger: ledger{}})

	if len(r.findings) != 0 {
		t.Errorf("prose-only changes were reported:\n%s", dump(r))
	}
}

// TestPropertyNamedDescriptionSurvives guards the stripper. A property that is
// literally called `description` (monitors have one) sits under `properties`
// and must not be mistaken for the documentation keyword.
//
// Only a property nested inside another property's schema exercises this:
// top-level properties are compared by name before any stripping happens. So
// the case that matters is an inline object whose own property is called
// `description`.
func TestPropertyNamedDescriptionSurvives(t *testing.T) {
	body := func(typ string) string {
		return `
paths:
  /things:
    post:
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/Thing'}
      responses: {'201': {description: ok}}
components:
  schemas:
    Thing:
      type: object
      properties:
        settings:
          type: object
          properties:
            description: {type: ` + typ + `}
`
	}
	r := Compare(Inputs{Old: doc(t, body("string")), New: doc(t, body("integer")), Ledger: ledger{}})
	if find(r, attention, groupShape, "Thing.settings") == nil {
		t.Fatalf("a change to a nested property named `description` was swallowed:\n%s", dump(r))
	}

	// And the keyword itself is still ignored at the same depth.
	prose := func(words string) string {
		return strings.Replace(body("string"), "{type: string}", "{type: string, description: "+words+"}", 1)
	}
	r = Compare(Inputs{Old: doc(t, prose("old")), New: doc(t, prose("new")), Ledger: ledger{}})
	if len(r.findings) != 0 {
		t.Errorf("prose on a nested property was reported:\n%s", dump(r))
	}
}

func TestNewEndpointNeedsClassifying(t *testing.T) {
	old := doc(t, "paths:\n  /a: {get: {summary: A, responses: {'200': {description: ok}}}}\n")
	cur := doc(t, "paths:\n  /a: {get: {summary: A, responses: {'200': {description: ok}}}}\n  /b: {get: {summary: B, responses: {'200': {description: ok}}}}\n")

	r := Compare(Inputs{Old: old, New: cur, Ledger: ledgerOf(engineSpec, "GET /a")})
	if find(r, attention, groupNew, "GET /b") == nil {
		t.Fatalf("an unclassified new endpoint was not flagged:\n%s", dump(r))
	}

	// Once someone has decided, it is no longer news.
	r = Compare(Inputs{Old: old, New: cur, Ledger: ledgerOf(engineSpec, "GET /a", "GET /b")})
	if find(r, attention, groupNew, "GET /b") != nil {
		t.Errorf("a classified endpoint was flagged again:\n%s", dump(r))
	}
}

func TestRemovedEndpoint(t *testing.T) {
	old := doc(t, "paths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n  /b: {get: {responses: {'200': {description: ok}}}}\n")
	cur := doc(t, "paths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n")

	// The provider calls /b (it is not in the ledger): removal is breaking.
	r := Compare(Inputs{Old: old, New: cur, Ledger: ledger{}})
	if find(r, attention, groupRemoved, "GET /b") == nil {
		t.Fatalf("a removed endpoint the provider calls was not flagged:\n%s", dump(r))
	}

	// The provider never called it (it is excluded): only the ledger entry is stale.
	r = Compare(Inputs{Old: old, New: cur, Ledger: ledgerOf(engineSpec, "GET /b")})
	if r.NeedsAttention() {
		t.Errorf("removing an excluded endpoint demanded attention:\n%s", dump(r))
	}
	if find(r, routine, groupStale, "GET /b") == nil {
		t.Errorf("the stale ledger entry was not reported:\n%s", dump(r))
	}
}

// TestEndpointMovedToAnotherSpec covers the docs site splitting one spec into
// three. /accounts left the Engine spec for the admin spec. The provider never
// called it, so reporting its "removal" as a break would be a false alarm on
// exactly the day the tool first runs.
func TestEndpointMovedToAnotherSpec(t *testing.T) {
	old := doc(t, "paths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n  /accounts: {get: {responses: {'200': {description: ok}}}}\n")
	cur := doc(t, "paths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n")
	l := ledger{
		engineSpec: {"x": {Operations: []string{"GET /a"}}},
		"admin":    {"accounts": {Operations: []string{"GET /accounts"}}},
	}

	admin := doc(t, "paths:\n  /accounts: {get: {responses: {'200': {description: ok}}}}\n")
	r := Compare(Inputs{Old: old, New: cur, Ledger: l, Also: map[string]specDoc{"admin": admin}})

	if r.NeedsAttention() {
		t.Errorf("an endpoint that moved to another spec was reported as a break:\n%s", dump(r))
	}
	if find(r, routine, groupMoved, "GET /accounts") == nil {
		t.Errorf("the move was not reported:\n%s", dump(r))
	}

	// With no home for it in the ledger it really is a removal.
	r = Compare(Inputs{Old: old, New: cur, Ledger: ledgerOf(engineSpec, "GET /a")})
	if find(r, attention, groupRemoved, "GET /accounts") == nil {
		t.Errorf("an unclassified removal was swallowed:\n%s", dump(r))
	}
}

func TestEnumValues(t *testing.T) {
	spec := func(values string) specDoc {
		return doc(t, `
paths:
  /a:
    get:
      parameters:
        - {name: kind, in: query, schema: {$ref: '#/components/schemas/Kind'}}
      responses: {'200': {description: ok}}
components:
  schemas:
    Kind: {type: string, enum: `+values+`}
`)
	}

	added := Compare(Inputs{Old: spec("[a, b]"), New: spec("[a, b, c]"), Ledger: ledger{}})
	if find(added, routine, groupEnumAdd, "Kind") == nil || added.NeedsAttention() {
		t.Errorf("an added enum value should be routine:\n%s", dump(added))
	}

	removed := Compare(Inputs{Old: spec("[a, b]"), New: spec("[a]"), Ledger: ledger{}})
	if find(removed, attention, groupEnumGone, "Kind") == nil {
		t.Errorf("a removed enum value should need attention:\n%s", dump(removed))
	}
}

func TestNewPropertyDependsOnDirection(t *testing.T) {
	spec := func(extra string) specDoc {
		return doc(t, `
paths:
  /things:
    post:
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/In'}
      responses:
        '201':
          description: ok
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Out'}
components:
  schemas:
    In:
      type: object
      properties: {a: {type: string}`+extra+`}
    Out:
      type: object
      properties: {a: {type: string}`+extra+`}
`)
	}
	r := Compare(Inputs{Old: spec(""), New: spec(", b: {type: string}"), Ledger: ledger{}})

	if find(r, attention, groupWritten, "In.b") == nil {
		t.Errorf("a new property on a schema the provider writes should need attention:\n%s", dump(r))
	}
	if find(r, look, groupReadProp, "Out.b") == nil {
		t.Errorf("a new property on a schema the provider only reads should be worth a look:\n%s", dump(r))
	}
}

func TestNewParameters(t *testing.T) {
	spec := func(param string) specDoc {
		return doc(t, `
paths:
  /things:
    get:
      parameters: [`+param+`]
      responses: {'200': {description: ok}}
`)
	}
	base := "{name: page, in: query, schema: {type: integer}}"

	optional := Compare(Inputs{Old: spec(base), New: spec(base + ", {name: sort, in: query, required: false, schema: {type: string}}"), Ledger: ledger{}})
	if find(optional, look, groupOptional, "query sort") == nil || optional.NeedsAttention() {
		t.Errorf("a new optional parameter should be worth a look, not blocking:\n%s", dump(optional))
	}

	required := Compare(Inputs{Old: spec(base), New: spec(base + ", {name: tenant, in: query, required: true, schema: {type: string}}"), Ledger: ledger{}})
	if find(required, attention, groupParams, "query tenant") == nil {
		t.Errorf("a new required parameter should need attention:\n%s", dump(required))
	}
}

// TestChangesOutsideTheSurfaceAreCountedNotReported: most of the spec is
// incidents, runs and logs, which the provider does not model. Reporting every
// change there would bury the ones that matter.
func TestChangesOutsideTheSurfaceAreCountedNotReported(t *testing.T) {
	spec := func(prop string) specDoc {
		return doc(t, `
paths:
  /a: {get: {responses: {'200': {description: ok}}}}
  /runs:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Run'}
components:
  schemas:
    Run:
      type: object
      properties: {`+prop+`}
`)
	}
	r := Compare(Inputs{Old: spec("id: {type: string}"), New: spec("id: {type: integer}, extra: {type: string}"), Ledger: ledgerOf(engineSpec, "GET /runs")})

	if len(r.findings) != 0 {
		t.Errorf("a change to a schema the provider does not use was reported:\n%s", dump(r))
	}
	if r.ignored == 0 {
		t.Error("the ignored change was not counted")
	}
}

// TestSplitSpecInventory covers the docs site splitting one spec into three.
// The agents API is not modelled, but a new endpoint in it is still a decision.
func TestSplitSpecInventory(t *testing.T) {
	engine := doc(t, "paths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n")
	agents := doc(t, "paths:\n  /agents: {get: {summary: List, responses: {'200': {description: ok}}}}\n  /skills: {get: {summary: Skills, responses: {'200': {description: ok}}}}\n")

	l := ledger{engineSpec: {"x": {Operations: []string{"GET /a"}}}, "agents": {"deferred": {Operations: []string{"GET /agents", "GET /gone"}}}}
	r := Compare(Inputs{Old: engine, New: engine, Ledger: l, Also: map[string]specDoc{"agents": agents}})

	if find(r, attention, groupNew, "GET /skills") == nil {
		t.Errorf("a new endpoint in an inventoried spec was not flagged:\n%s", dump(r))
	}
	if find(r, attention, groupNew, "GET /agents") != nil {
		t.Errorf("a classified endpoint in an inventoried spec was flagged:\n%s", dump(r))
	}
	if find(r, routine, groupStale, "GET /gone") == nil {
		t.Errorf("a ledger entry for a vanished endpoint was not reported:\n%s", dump(r))
	}
}

// TestComparedSpec covers a second API the provider calls: its schemas are
// diffed like the Engine spec's, and its findings say which API they are in.
func TestComparedSpec(t *testing.T) {
	engine := doc(t, "paths: {}\n")
	old := doc(t, monitorSpec("{type: array, items: {$ref: '#/components/schemas/GroupBy'}}", "{type: string}"))
	cur := doc(t, monitorSpec("{type: array, items: {$ref: '#/components/schemas/GroupBy'}}", "{type: string, maxLength: 10}"))

	r := Compare(Inputs{Old: engine, New: engine, Ledger: ledger{}, Compared: map[string][2]specDoc{"agents": {old, cur}}})
	f := find(r, attention, groupConstr, "Monitor")
	if f == nil {
		t.Fatalf("a tightened constraint in a compared spec was not reported:\n%s", r.Markdown())
	}
	if !strings.Contains(f.subject, "(agents API)") {
		t.Errorf("finding does not name its API: %q", f.subject)
	}

	// The same change in a spec that is only inventoried is not the
	// provider's business.
	r = Compare(Inputs{Old: engine, New: engine, Ledger: ledgerOf("agents", "POST /monitors"), Also: map[string]specDoc{"agents": cur}})
	if len(r.findings) != 0 {
		t.Errorf("an inventoried spec's schema change was reported:\n%s", r.Markdown())
	}
}

func TestCleanReport(t *testing.T) {
	spec := doc(t, "paths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n")
	r := Compare(Inputs{Old: spec, New: spec, Ledger: ledgerOf(engineSpec, "GET /a")})
	if r.NeedsAttention() || len(r.findings) != 0 {
		t.Errorf("identical specs produced findings:\n%s", dump(r))
	}
	if !strings.Contains(r.Markdown(), "Nothing the provider models has changed") {
		t.Errorf("clean report does not say so:\n%s", r.Markdown())
	}
}

// TestMovedSpecIsNotMistakenForEmpty: when the docs site moved the spec the
// download returned "Asset not found", which is valid YAML. Treating that as an
// empty API would report every endpoint as removed — or, worse, nothing at all.
func TestMovedSpecIsNotMistakenForEmpty(t *testing.T) {
	for name, body := range map[string]string{
		"404 page":    "Asset not found",
		"empty file":  "",
		"no paths":    "openapi: 3.1.0\ninfo: {title: t}\n",
		"a yaml list": "- a\n- b\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSpec([]byte(body)); err == nil {
				t.Errorf("%q was accepted as an OpenAPI document", body)
			}
		})
	}
}

// TestExitStatuses pins the contract the scheduled workflow branches on. A
// wrong status here either opens a pull request for nothing or, worse, stays
// quiet about drift.
func TestExitStatuses(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	const header = "openapi: 3.1.0\ninfo: {title: t, version: v1}\n"
	base := write("base.yaml", header+"paths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n")
	withNew := write("new-endpoint.yaml", header+"paths:\n  /a: {get: {responses: {'200': {description: ok}}}}\n  /b: {get: {responses: {'200': {description: ok}}}}\n")
	withEnum := func(vals string) string {
		return write("enum-"+vals+".yaml", header+`
paths:
  /a:
    get:
      parameters: [{name: k, in: query, schema: {$ref: '#/components/schemas/K'}}]
      responses: {'200': {description: ok}}
components:
  schemas:
    K: {type: string, enum: [`+vals+`]}
`)
	}
	ledgerFile := write("coverage.yaml", "engine:\n  none:\n    reason: test\n    operations: []\n")

	cases := []struct {
		name     string
		old, new string
		want     int
	}{
		{"nothing changed", base, base, exitClean},
		{"unclassified endpoint", base, withNew, exitAttention},
		{"enum value added only", withEnum("a"), withEnum("a, b"), exitRoutine},
		{"enum value removed", withEnum("a, b"), withEnum("a"), exitAttention},
		{"a 404 page instead of a spec", base, write("404.yaml", "Asset not found"), exitError},
		{"a file that does not exist", base, filepath.Join(dir, "missing.yaml"), exitError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			got := run([]string{"-old", tc.old, "-new", tc.new, "-coverage", ledgerFile, "-exit-code"}, &out, &errOut)
			if got != tc.want {
				t.Errorf("exit status %d, want %d\nstdout: %s\nstderr: %s", got, tc.want, out.String(), errOut.String())
			}
		})
	}

	// Without -exit-code the report is the product and the status is always 0.
	var out, errOut bytes.Buffer
	if got := run([]string{"-old", base, "-new", withNew, "-coverage", ledgerFile}, &out, &errOut); got != exitClean {
		t.Errorf("without -exit-code, status %d, want 0", got)
	}
}
