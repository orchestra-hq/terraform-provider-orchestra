package apicontract_test

import (
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/agents"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/environment"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/monitor"
	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/pipeline"
)

// TestEveryWrittenPropertyIsClassified is TestEverySpecOperationIsClassified
// one level down. Every property of every schema the provider sends in a
// request body, following $refs, must be sent by the provider or listed under
// `properties` in coverage.yaml with a reason.
//
// A property the provider has never heard of is worse than a missing
// endpoint: a monitor PUT replaces the whole definition, so until
// `subscribers` was mapped every apply silently cleared the subscribers set in
// the Orchestra UI.
//
// "Sent" is decided two ways. A schema encoded by a wire struct is checked
// against the struct's JSON tags: the roots come from each package's
// WireSchemas and nested schemas are paired by following the fields. A body
// built as a map literal has no type to inspect, so its properties need only
// appear as a quoted string in the file that makes the call, struct tags and
// query parameters aside. That catches a name the provider never uses, but
// not one it uses for something else.
func TestEveryWrittenPropertyIsClassified(t *testing.T) {
	coverage := loadCoverage(t)
	registry := map[string]reflect.Type{}
	for _, wire := range []map[string]any{pipeline.WireSchemas, monitor.WireSchemas, environment.WireSchemas, agents.WireSchemas} {
		for name, v := range wire {
			registry[name] = reflect.TypeOf(v)
		}
	}

	used := map[string]bool{}
	for _, s := range apiSpecs {
		t.Run(s.name, func(t *testing.T) { checkProperties(t, s, coverage[s.name], registry, used) })
	}
	for name, typ := range registry {
		if !used[name] {
			t.Errorf("WireSchemas pairs %s with %s, but no request body the provider sends reaches %s — it was renamed or removed upstream", name, typ, name)
		}
	}
}

type specDoc struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]any `yaml:"schemas"`
	} `yaml:"components"`
}

func checkProperties(t *testing.T, s apiSpec, ledger map[string]coverageCategory, registry map[string]reflect.Type, used map[string]bool) {
	raw, err := os.ReadFile(s.file)
	if err != nil {
		t.Fatalf("read %s: %v", s.file, err)
	}
	var doc specDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", s.file, err)
	}
	schemas := doc.Components.Schemas

	// Which operations send each schema, and from which files.
	writtenBy, files := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, c := range discoverCalls(t, loadSpec(t, s), s) {
		op, _ := doc.Paths[c.path][strings.ToLower(c.method)].(map[string]any)
		roots := map[string]bool{}
		collectRefs(op["requestBody"], roots)
		for name := range closure(schemas, roots) {
			addTo(writtenBy, name, c.method+" "+c.path)
			addTo(files, name, c.file)
		}
	}

	pairs := pairing{}
	for name, typ := range registry {
		if writtenBy[name] != nil {
			used[name] = true
			pairs.walk(schemas, name, typ, registry)
		}
	}

	excluded := map[string]string{}
	for name, cat := range ledger {
		for _, p := range cat.Properties {
			if prev, dup := excluded[p]; dup {
				t.Errorf("coverage.yaml: property %q is in both %q and %q", p, prev, name)
			}
			excluded[p] = name
		}
	}

	strs := map[string]map[string]bool{}
	written := map[string]bool{}
	var unclassified []string
	for _, name := range sortedSet(writtenBy) {
		for prop := range properties(schemas[name]) {
			key := name + "." + prop
			written[key] = true

			var missing []string
			if types := pairs[name]; len(types) > 0 {
				for _, typ := range types {
					if _, ok := jsonNames(typ)[prop]; !ok {
						missing = append(missing, "not a JSON tag on "+typ.String())
					}
				}
			} else {
				named := false
				for f := range files[name] {
					if strs[f] == nil {
						strs[f] = quotedStrings(t, f)
					}
					named = named || strs[f][prop]
				}
				if !named {
					missing = append(missing, fmt.Sprintf("not named in %s, and no type in a WireSchemas encodes %s",
						strings.Join(baseNames(files[name]), ", "), name))
				}
			}

			switch cat := excluded[key]; {
			case len(missing) == 0 && cat != "":
				t.Errorf("coverage.yaml lists %q as %q, but the provider now sends it — delete the entry", key, cat)
			case len(missing) > 0 && cat == "":
				unclassified = append(unclassified, fmt.Sprintf("%s (%s; in the body of %s)",
					key, strings.Join(missing, ", "), strings.Join(sortedSet(writtenBy[name]), ", ")))
			}
		}
	}

	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Errorf("%d propert(ies) of request bodies the provider sends are neither sent nor listed in coverage.yaml.\n"+
			"Send each from the type or file named, or add it to a category's `properties` with a reason.\n"+
			"Leaving one out of a PUT resets whatever was set in the Orchestra UI.\n\n  %s",
			len(unclassified), strings.Join(unclassified, "\n  "))
	}

	var stale []string
	for key, cat := range excluded {
		if !written[key] {
			stale = append(stale, fmt.Sprintf("%s (in %q)", key, cat))
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("coverage.yaml lists propert(ies) that no request body the provider sends has — removed or renamed upstream, so delete the entry:\n  %s",
			strings.Join(stale, "\n  "))
	}
}

// pairing records which Go types encode each schema.
type pairing map[string][]reflect.Type

// walk pairs a schema with a type, then each property's object schemas with
// the type of the field that carries it. A schema the registry names keeps
// its registered type, which is how a field marshalled by hand, or carried as
// raw JSON, still gets checked.
func (p pairing) walk(schemas map[string]any, name string, typ reflect.Type, registry map[string]reflect.Type) {
	for _, seen := range p[name] {
		if seen == typ {
			return
		}
	}
	p[name] = append(p[name], typ)

	fields := jsonNames(typ)
	for prop, ps := range properties(schemas[name]) {
		f, ok := fields[prop]
		if !ok {
			continue
		}
		for _, ref := range objectRefs(schemas, ps) {
			if rt, ok := registry[ref]; ok {
				p.walk(schemas, ref, rt, registry)
			} else if ft := elem(f.Type); ft.Kind() == reflect.Struct {
				p.walk(schemas, ref, ft, registry)
			}
		}
	}
}

// jsonNames maps the names encoding/json gives a struct's fields to the fields.
func jsonNames(typ reflect.Type) map[string]reflect.StructField {
	out := map[string]reflect.StructField{}
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" {
			name = f.Name
		}
		if f.IsExported() && name != "-" {
			out[name] = f
		}
	}
	return out
}

// elem strips pointers, slices and maps down to the type of one element.
func elem(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	return t
}

// objectRefs returns the schemas with properties that a property schema
// reaches, seeing through unions such as AlertParametersModel, which only
// wrap others.
func objectRefs(schemas map[string]any, v any) []string {
	var out []string
	seen := map[string]bool{}
	var visit func(any)
	visit = func(v any) {
		refs := map[string]bool{}
		collectRefs(v, refs)
		for name := range refs {
			if seen[name] {
				continue
			}
			seen[name] = true
			if len(properties(schemas[name])) > 0 {
				out = append(out, name)
			} else {
				visit(schemas[name])
			}
		}
	}
	visit(v)
	return out
}

const refPrefix = "#/components/schemas/"

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
	queue := sortedSet(roots)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		next := map[string]bool{}
		collectRefs(schemas[name], next)
		queue = append(queue, sortedSet(next)...)
	}
	return seen
}

func properties(schema any) map[string]any {
	m, _ := schema.(map[string]any)
	p, _ := m["properties"].(map[string]any)
	return p
}

// quotedStrings returns the double-quoted string literals in a source file,
// except query parameter names. Struct tags are raw strings, so a response
// struct's fields do not count as sent.
func quotedStrings(t *testing.T, file string) map[string]bool {
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var sc scanner.Scanner
	fset := token.NewFileSet()
	sc.Init(fset.AddFile(file, fset.Base(), len(src)), src, nil, 0)

	out := map[string]bool{}
	var prev [3]string
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			return out
		}
		if tok == token.STRING && lit[0] == '"' && prev != [3]string{".", "Set", "("} {
			if s, err := strconv.Unquote(lit); err == nil {
				out[s] = true
			}
		}
		if lit == "" {
			lit = tok.String()
		}
		prev = [3]string{prev[1], prev[2], lit}
	}
}

func addTo(m map[string]map[string]bool, k, v string) {
	if m[k] == nil {
		m[k] = map[string]bool{}
	}
	m[k][v] = true
}

func sortedSet[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func baseNames(files map[string]bool) []string {
	var out []string
	for _, f := range sortedSet(files) {
		out = append(out, filepath.Base(f))
	}
	return out
}
