package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// engineSpec names the spec the provider is built against, in the coverage
// ledger and in reports. Any other spec given to Compare is only inventoried.
const engineSpec = "engine"

const refPrefix = "#/components/schemas/"

type specDoc map[string]any

func parseSpec(b []byte) (specDoc, error) {
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	doc, ok := normalize(raw).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("not an OpenAPI document: top level is %T", raw)
	}
	if _, ok := doc["paths"].(map[string]any); !ok {
		// A 404 page or a moved file parses as YAML too, so say what is missing.
		return nil, errors.New("not an OpenAPI document: no `paths`")
	}
	return doc, nil
}

// normalize makes every mapping a map[string]any. yaml.v3 yields
// map[any]any when a key is not a string, which an unquoted `200:` is.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			t[k] = normalize(c)
		}
		return t
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, c := range t {
			out[fmt.Sprint(k)] = normalize(c)
		}
		return out
	case []any:
		for i, c := range t {
			t[i] = normalize(c)
		}
		return t
	}
	return v
}

var httpMethods = []string{"get", "put", "post", "delete", "patch"}

// operations indexes every operation as "METHOD /path".
func (d specDoc) operations() map[string]map[string]any {
	out := map[string]map[string]any{}
	paths, _ := d["paths"].(map[string]any)
	for path, item := range paths {
		m, _ := item.(map[string]any)
		for _, method := range httpMethods {
			if op, ok := m[method].(map[string]any); ok {
				out[strings.ToUpper(method)+" "+path] = op
			}
		}
	}
	return out
}

func (d specDoc) schemas() map[string]any {
	components, _ := d["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	return schemas
}

// ---------- comparing structure, not prose ----------

// noise holds the keywords that change when someone edits documentation. The
// provider's behaviour does not depend on them, so they must not register as
// drift: the first refresh after the docs site changed rewrote ~1300 lines of
// the spec and none of it mattered.
var noise = map[string]bool{
	"title": true, "description": true, "summary": true, "example": true, "examples": true,
}

// nameMaps are keywords whose keys are names chosen by the API author rather
// than JSON Schema keywords. A property called `description` lives under
// `properties` and must survive stripping.
var nameMaps = map[string]bool{
	"properties": true, "patternProperties": true, "$defs": true, "definitions": true,
}

// constraintKeys tighten or loosen what a value may be without changing what
// it is. A change confined to these is a validator to adjust, not a new shape.
var constraintKeys = map[string]bool{
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
	"minLength": true, "maxLength": true, "pattern": true, "format": true,
	"minItems": true, "maxItems": true, "minProperties": true, "maxProperties": true,
	"multipleOf": true, "enum": true,
}

// strip returns v without documentation keywords.
func strip(v any) any { return walk(v, noise) }

// dropConstraints returns v without documentation keywords or constraints.
func dropConstraints(v any) any {
	both := map[string]bool{}
	for k := range noise {
		both[k] = true
	}
	for k := range constraintKeys {
		both[k] = true
	}
	return walk(v, both)
}

func walk(v any, drop map[string]bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			if nameMaps[k] {
				if named, ok := child.(map[string]any); ok {
					kept := make(map[string]any, len(named))
					for name, sub := range named {
						kept[name] = walk(sub, drop)
					}
					out[k] = kept
					continue
				}
			}
			if drop[k] {
				continue
			}
			out[k] = walk(child, drop)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, c := range t {
			out[i] = walk(c, drop)
		}
		return out
	}
	return v
}

func same(a, b any) bool { return reflect.DeepEqual(strip(a), strip(b)) }

// ---------- what the provider reaches ----------

func collectRefs(v any, into map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			if s, ok := c.(string); ok && k == "$ref" && strings.HasPrefix(s, refPrefix) {
				into[strings.TrimPrefix(s, refPrefix)] = true
				continue
			}
			collectRefs(c, into)
		}
	case []any:
		for _, c := range t {
			collectRefs(c, into)
		}
	}
}

// closure returns roots and every schema reachable from them.
func closure(schemas map[string]any, roots map[string]bool) map[string]bool {
	seen := map[string]bool{}
	queue := keys(roots)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		next := map[string]bool{}
		collectRefs(schemas[name], next)
		queue = append(queue, keys(next)...)
	}
	return seen
}

// opRoots splits an operation's schema references into what the provider
// sends and what it receives. Only successful responses count: every
// operation also references the validation-error schemas, which would put
// them in the provider's surface for no reason.
func opRoots(op map[string]any) (written, read map[string]bool) {
	written, read = map[string]bool{}, map[string]bool{}
	collectRefs(op["requestBody"], written)
	collectRefs(op["parameters"], read)
	responses, _ := op["responses"].(map[string]any)
	for code, resp := range responses {
		if strings.HasPrefix(code, "2") {
			collectRefs(resp, read)
		}
	}
	return written, read
}

// ---------- the report ----------

type tier int

const (
	attention tier = iota // the provider is, or will be, wrong until someone acts
	look                  // optional; the provider could do more
	routine               // handled by `make openapi`, or housekeeping
)

var tierTitles = map[tier]string{
	attention: "Needs attention",
	look:      "Worth a look",
	routine:   "Routine",
}

const (
	groupNew       = "New endpoints to classify"
	groupRemoved   = "Endpoints the provider calls were removed"
	groupParams    = "Parameters changed on endpoints the provider calls"
	groupBodies    = "Request or response bodies changed"
	groupShape     = "Schema shape changed"
	groupConstr    = "Constraints changed (add or adjust a plan-time validator)"
	groupWritten   = "New properties on schemas the provider writes"
	groupRequired  = "Required properties changed"
	groupEnumGone  = "Enum values removed"
	groupOptional  = "New optional parameters"
	groupReadProp  = "New properties on schemas the provider reads"
	groupNewSchema = "New schemas reachable from endpoints the provider calls"
	groupEnumAdd   = "Enum values added (make openapi regenerates the generated ones)"
	groupStale     = "Stale entries in coverage.yaml"
	groupMoved     = "Endpoints that moved to another spec"
)

var groupOrder = []string{
	groupNew, groupRemoved, groupParams, groupBodies, groupShape, groupConstr,
	groupWritten, groupRequired, groupEnumGone, groupOptional, groupReadProp,
	groupNewSchema, groupEnumAdd, groupMoved, groupStale,
}

type finding struct {
	tier    tier
	group   string
	subject string
	notes   []string
}

// Report is the outcome of a comparison.
type Report struct {
	findings []finding
	ignored  int // schemas that changed outside the provider's surface
}

func (r *Report) add(t tier, group, subject string, notes ...string) {
	r.findings = append(r.findings, finding{t, group, subject, notes})
}

func (r *Report) count(t tier) int {
	n := 0
	for _, f := range r.findings {
		if f.tier == t {
			n++
		}
	}
	return n
}

// NeedsAttention reports whether the provider is, or will be, wrong until
// someone acts. It is what decides a scheduled run's exit status.
func (r *Report) NeedsAttention() bool { return r.count(attention) > 0 }

// Markdown renders the report for a pull request body or a terminal.
func (r *Report) Markdown() string {
	var b strings.Builder
	b.WriteString("# Orchestra API spec drift\n\n")

	if len(r.findings) == 0 {
		b.WriteString("Nothing the provider models has changed.")
		if r.ignored > 0 {
			fmt.Fprintf(&b, " %d schema change(s) outside the provider's surface were ignored.", r.ignored)
		}
		b.WriteString("\n")
		return b.String()
	}

	fmt.Fprintf(&b, "**%d need attention · %d worth a look · %d routine**", r.count(attention), r.count(look), r.count(routine))
	if r.ignored > 0 {
		fmt.Fprintf(&b, " · %d schema change(s) outside the provider's surface ignored", r.ignored)
	}
	b.WriteString("\n")

	for _, t := range []tier{attention, look, routine} {
		if r.count(t) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n", tierTitles[t])
		for _, g := range groupOrder {
			var group []finding
			for _, f := range r.findings {
				if f.tier == t && f.group == g {
					group = append(group, f)
				}
			}
			if len(group) == 0 {
				continue
			}
			fmt.Fprintf(&b, "\n### %s\n\n", g)
			for _, f := range group {
				fmt.Fprintf(&b, "- %s\n", f.subject)
				for _, n := range f.notes {
					fmt.Fprintf(&b, "  - %s\n", n)
				}
			}
		}
	}
	return b.String()
}

// ---------- the comparison ----------

// Inputs are the specs and the ledger Compare works from.
type Inputs struct {
	Old, New specDoc
	Ledger   ledger
	// Also holds current copies of the specs that are only inventoried, by
	// name. The provider does not talk to them, but a new endpoint in one is
	// still a decision someone should make.
	Also map[string]specDoc
}

// Compare reports what the move from Old to New means for the provider.
//
// The provider's surface is every operation in Old that the ledger does not
// exclude. The ledger is kept honest by a test, so that is exactly what the
// provider implements.
func Compare(in Inputs) *Report {
	r := &Report{}
	oldOps, newOps := in.Old.operations(), in.New.operations()
	excluded := in.Ledger.excluded(engineSpec)

	// The docs site has split this spec before, moving endpoints out. An
	// operation that left it for a spec the ledger classifies is not something
	// the provider ever called.
	elsewhere := in.Ledger.excludedElsewhere(engineSpec)
	moved := map[string]string{}
	for op := range oldOps {
		if _, inNew := newOps[op]; inNew {
			continue
		}
		if where, ok := elsewhere[op]; ok {
			moved[op] = where
		}
	}

	covered := map[string]bool{}
	for op := range oldOps {
		_, skip := excluded[op]
		_, gone := moved[op]
		if !skip && !gone {
			covered[op] = true
		}
	}

	r.endpoints(oldOps, newOps, excluded, moved)
	for _, op := range sorted(covered) {
		o, inOld := oldOps[op]
		n, inNew := newOps[op]
		if inOld && inNew {
			r.operation(op, o, n)
		}
	}
	r.schemas(in, covered, oldOps, newOps)
	r.inventories(in)
	return r
}

func (r *Report) endpoints(oldOps, newOps map[string]map[string]any, excluded, moved map[string]string) {
	for _, op := range sortedOps(newOps) {
		if _, inOld := oldOps[op]; inOld {
			continue
		}
		if _, known := excluded[op]; known {
			continue
		}
		r.add(attention, groupNew, code(op), summaryNote(newOps[op])+"implement it, or classify it in `internal/apicontract/coverage.yaml` with a reason")
	}
	for _, op := range sortedOps(oldOps) {
		if _, inNew := newOps[op]; inNew {
			continue
		}
		if where, ok := moved[op]; ok {
			r.add(routine, groupMoved, code(op), fmt.Sprintf("now classified under `%s`; nothing to do", where))
			continue
		}
		if cat, known := excluded[op]; known {
			r.add(routine, groupStale, code(op), fmt.Sprintf("removed upstream; it was classified `%s`, so delete the entry", cat))
			continue
		}
		r.add(attention, groupRemoved, code(op), "the provider calls this")
	}
	// An entry that matched nothing in either spec was never right.
	for _, op := range sortedStrings(excluded) {
		_, inOld := oldOps[op]
		_, inNew := newOps[op]
		if !inOld && !inNew {
			r.add(routine, groupStale, code(op), "not in either spec; delete the entry")
		}
	}
}

func (r *Report) operation(op string, o, n map[string]any) {
	oldP, newP := paramsOf(o), paramsOf(n)
	for _, key := range sortedKeys(oldP, newP) {
		po, inOld := oldP[key]
		pn, inNew := newP[key]
		subject := fmt.Sprintf("%s — parameter `%s`", code(op), key)
		switch {
		case !inOld && pn["required"] == true:
			r.add(attention, groupParams, subject, "new and **required**: every request now needs it")
		case !inOld:
			r.add(look, groupOptional, subject, "new and optional; could be exposed", "schema: "+compact(pn["schema"]))
		case !inNew:
			r.add(attention, groupParams, subject, "removed")
		case !same(po, pn):
			r.add(attention, groupParams, subject, "was: "+compact(po), "now: "+compact(pn))
		}
	}

	if !same(o["requestBody"], n["requestBody"]) {
		r.add(attention, groupBodies, code(op)+" — request body", "was: "+compact(o["requestBody"]), "now: "+compact(n["requestBody"]))
	}
	oldR, newR := successResponses(o), successResponses(n)
	if !reflect.DeepEqual(strip(oldR), strip(newR)) {
		r.add(attention, groupBodies, code(op)+" — response", "was: "+compact(oldR), "now: "+compact(newR))
	}
}

func paramsOf(op map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	list, _ := op["parameters"].([]any)
	for _, item := range list {
		if p, ok := item.(map[string]any); ok {
			out[fmt.Sprint(p["in"])+" "+fmt.Sprint(p["name"])] = p
		}
	}
	return out
}

func successResponses(op map[string]any) map[string]any {
	out := map[string]any{}
	responses, _ := op["responses"].(map[string]any)
	for code, resp := range responses {
		if strings.HasPrefix(code, "2") {
			out[code] = resp
		}
	}
	return out
}

func (r *Report) schemas(in Inputs, covered map[string]bool, oldOps, newOps map[string]map[string]any) {
	oldS, newS := in.Old.schemas(), in.New.schemas()

	writes, reads := map[string][]string{}, map[string][]string{}
	for _, op := range sorted(covered) {
		for _, side := range []struct {
			ops     map[string]map[string]any
			schemas map[string]any
		}{{newOps, newS}, {oldOps, oldS}} {
			o, ok := side.ops[op]
			if !ok {
				continue
			}
			w, rd := opRoots(o)
			for s := range closure(side.schemas, w) {
				writes[s] = appendOnce(writes[s], op)
			}
			for s := range closure(side.schemas, rd) {
				reads[s] = appendOnce(reads[s], op)
			}
		}
	}

	reached := map[string]bool{}
	for s := range writes {
		reached[s] = true
	}
	for s := range reads {
		reached[s] = true
	}

	for _, name := range sorted(reached) {
		o, inOld := oldS[name]
		n, inNew := newS[name]
		isWritten := len(writes[name]) > 0
		where := whereNote(writes[name], reads[name])
		switch {
		case !inNew:
			// Gone entirely; the property that referenced it changed on its parent.
		case !inOld:
			r.add(look, groupNewSchema, code(name), where)
		default:
			r.classify(name, diffSchema(schemaMap(o), schemaMap(n)), isWritten, where)
		}
	}

	for name, n := range newS {
		if reached[name] {
			continue
		}
		if o, inOld := oldS[name]; !inOld || !same(o, n) {
			r.ignored++
		}
	}
}

func (r *Report) classify(name string, changes []change, isWritten bool, where string) {
	for _, c := range changes {
		subject := code(name)
		if c.property != "" {
			subject = code(name + "." + c.property)
		}
		switch c.kind {
		case propAdded:
			if isWritten {
				r.add(attention, groupWritten, subject, "the provider cannot set it, and an update may drop a value set elsewhere", "shape: "+compact(c.now), where)
			} else {
				r.add(look, groupReadProp, subject, "shape: "+compact(c.now), where)
			}
		case propRemoved:
			r.add(attention, groupShape, subject, "property removed", "was: "+compact(c.was), where)
		case shapeChanged:
			r.add(attention, groupShape, subject, "was: "+compact(c.was), "now: "+compact(c.now), where)
		case constraintChanged:
			r.add(attention, groupConstr, subject, "was: "+compact(c.was), "now: "+compact(c.now), where)
		case requiredAdded:
			t := look
			if isWritten {
				t = attention
			}
			r.add(t, groupRequired, subject, "now required", where)
		case requiredRemoved:
			r.add(look, groupRequired, subject, "no longer required", where)
		case enumAdded:
			r.add(routine, groupEnumAdd, subject, "added: "+c.detail)
		case enumRemoved:
			r.add(attention, groupEnumGone, subject, "removed: "+c.detail, where)
		case otherChanged:
			r.add(attention, groupShape, subject, fmt.Sprintf("`%s` changed", c.detail), where)
		}
	}
}

func (r *Report) inventories(in Inputs) {
	for _, name := range sortedDocs(in.Also) {
		live := in.Also[name].operations()
		excluded := in.Ledger.excluded(name)
		for _, op := range sortedOps(live) {
			if _, known := excluded[op]; !known {
				r.add(attention, groupNew, fmt.Sprintf("%s (%s API)", code(op), name),
					summaryNote(live[op])+"classify it in `internal/apicontract/coverage.yaml`, or implement it")
			}
		}
		for _, op := range sortedStrings(excluded) {
			if _, ok := live[op]; !ok {
				r.add(routine, groupStale, fmt.Sprintf("%s (%s API)", code(op), name), "no longer in the spec; delete the entry")
			}
		}
	}
}

// ---------- diffing one schema ----------

type changeKind int

const (
	propAdded changeKind = iota
	propRemoved
	shapeChanged
	constraintChanged
	requiredAdded
	requiredRemoved
	enumAdded
	enumRemoved
	otherChanged
)

type change struct {
	kind     changeKind
	property string
	was, now any
	detail   string
}

func diffSchema(oldS, newS map[string]any) []change {
	var out []change

	oldP, newP := schemaMap(oldS["properties"]), schemaMap(newS["properties"])
	for _, name := range sortedKeys(oldP, newP) {
		o, inOld := oldP[name]
		n, inNew := newP[name]
		switch {
		case !inOld:
			out = append(out, change{kind: propAdded, property: name, now: strip(n)})
		case !inNew:
			out = append(out, change{kind: propRemoved, property: name, was: strip(o)})
		case !same(o, n):
			kind := shapeChanged
			if reflect.DeepEqual(dropConstraints(o), dropConstraints(n)) {
				kind = constraintChanged
			}
			out = append(out, change{kind: kind, property: name, was: strip(o), now: strip(n)})
		}
	}

	oldReq, newReq := stringSet(oldS["required"]), stringSet(newS["required"])
	for _, name := range sorted(newReq) {
		if !oldReq[name] {
			out = append(out, change{kind: requiredAdded, property: name})
		}
	}
	for _, name := range sorted(oldReq) {
		if !newReq[name] {
			out = append(out, change{kind: requiredRemoved, property: name})
		}
	}

	oldE, newE := stringSet(oldS["enum"]), stringSet(newS["enum"])
	if added := minus(newE, oldE); len(added) > 0 {
		out = append(out, change{kind: enumAdded, detail: strings.Join(added, ", ")})
	}
	if removed := minus(oldE, newE); len(removed) > 0 {
		out = append(out, change{kind: enumRemoved, detail: strings.Join(removed, ", ")})
	}

	for _, key := range sortedKeys(oldS, newS) {
		if key == "properties" || key == "required" || key == "enum" || noise[key] {
			continue
		}
		if !same(oldS[key], newS[key]) {
			out = append(out, change{kind: otherChanged, detail: key})
		}
	}
	return out
}

// ---------- small helpers ----------

func schemaMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func stringSet(v any) map[string]bool {
	out := map[string]bool{}
	list, _ := v.([]any)
	for _, item := range list {
		out[fmt.Sprint(item)] = true
	}
	return out
}

func minus(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func code(s string) string { return "`" + s + "`" }

func summaryNote(op map[string]any) string {
	if s, _ := op["summary"].(string); s != "" {
		return s + " — "
	}
	return ""
}

// compact renders a value as one short line of JSON. Keys come out sorted, so
// the same change always reads the same way.
func compact(v any) string {
	b, err := json.Marshal(strip(v))
	if err != nil {
		return code(fmt.Sprint(v))
	}
	s := string(b)
	if len(s) > 360 {
		s = s[:360] + "…"
	}
	return code(s)
}

func whereNote(writes, reads []string) string {
	if len(writes) > 0 {
		return "written by " + opList(writes)
	}
	return "read by " + opList(reads)
}

func opList(ops []string) string {
	sort.Strings(ops)
	var shown []string
	for i, op := range ops {
		if i == 3 {
			shown = append(shown, fmt.Sprintf("+%d more", len(ops)-3))
			break
		}
		shown = append(shown, code(op))
	}
	return strings.Join(shown, ", ")
}

func appendOnce(list []string, s string) []string {
	for _, e := range list {
		if e == s {
			return list
		}
	}
	return append(list, s)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sorted(m map[string]bool) []string {
	out := keys(m)
	sort.Strings(out)
	return out
}

func sortedStrings(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedOps(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedDocs(m map[string]specDoc) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedKeys returns the union of the keys of two maps, sorted.
func sortedKeys[A, B any](a map[string]A, b map[string]B) []string {
	seen := map[string]bool{}
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	return sorted(seen)
}
