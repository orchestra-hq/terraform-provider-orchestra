// Discovery of the provider's own call sites, so the endpoint list below is
// checked against the code rather than maintained alongside it by hand.
package apicontract_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Matches DoJSON(ctx, http.MethodGet, "/path"  and  ListAll(ctx, "/path".
// A path built by concatenation ("/environments/"+id, or
// "/pipelines/"+id+"/pause") keeps its literal prefix and suffix; pathToSpec
// turns those back into the spec's templated form.
var (
	doJSONRE = regexp.MustCompile(`DoJSON\(\s*ctx,\s*http\.Method(\w+),\s*"([^"]+)"(?:\s*\+[^,"]+\+\s*"([^"]+)")?`)
	listRE   = regexp.MustCompile(`ListAll\(\s*ctx,\s*"([^"]+)"(?:\s*\+[^,"]+\+\s*"([^"]+)")?`)
	setRE    = regexp.MustCompile(`\.Set\("([^"]+)"`)
)

type callSite struct {
	method, path, file string
}

// pathToSpec maps a Go path literal onto the spec's path template. A trailing
// slash in the source means an ID was concatenated on, e.g. "/environments/"
// is the spec's /environments/{environment_id}; suffix is any literal after
// the ID, e.g. "/pause".
func pathToSpec(spec openAPI, raw, suffix string) string {
	if _, ok := spec.Paths[raw]; ok && suffix == "" {
		return raw
	}
	if !strings.HasSuffix(raw, "/") {
		return raw + suffix
	}
	for p := range spec.Paths {
		param, ok := strings.CutPrefix(p, raw)
		if !ok {
			continue
		}
		param, ok = strings.CutSuffix(param, suffix)
		if ok && strings.HasPrefix(param, "{") && strings.HasSuffix(param, "}") && !strings.Contains(param, "/") {
			return p
		}
	}
	return raw + "{?}" + suffix
}

func providerSource(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join("..", "..", "internal")
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[p] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
	return out
}

// discoverCalls finds the call sites into one spec.
func discoverCalls(t *testing.T, spec openAPI, s apiSpec) []callSite {
	t.Helper()
	var found []callSite
	for file, src := range providerSource(t) {
		if strings.HasSuffix(file, "client.go") || !s.owns(file) {
			continue // the transport itself takes the path as an argument
		}
		for _, m := range doJSONRE.FindAllStringSubmatch(src, -1) {
			found = append(found, callSite{strings.ToUpper(m[1]), pathToSpec(spec, m[2], m[3]), file})
		}
		for _, m := range listRE.FindAllStringSubmatch(src, -1) {
			found = append(found, callSite{"GET", pathToSpec(spec, m[1], m[2]), file})
		}
	}
	return found
}

// TestEveryCallSiteIsListed is the half that the hand-written list cannot
// provide on its own: it fails when the provider starts calling something the
// list does not mention, which is the direction the original bugs travelled.
func TestEveryCallSiteIsListed(t *testing.T) {
	for _, s := range apiSpecs {
		spec := loadSpec(t, s)
		listed := map[string]bool{}
		for _, c := range callsFor(s) {
			listed[c.method+" "+c.path] = true
		}
		found := discoverCalls(t, spec, s)
		if len(found) == 0 {
			t.Fatalf("found no %s API call sites in internal/ — the patterns in this file have gone stale", s.name)
		}
		for _, c := range found {
			if !listed[c.method+" "+c.path] {
				t.Errorf("%s calls %s %s, which is not in the %s calls list in contract_test.go", c.file, c.method, c.path, s.name)
			}
		}
	}
}

// TestEverySetQueryParameterIsDeclared catches a renamed or invented query
// parameter. The API ignores an undeclared parameter rather than rejecting it,
// so nothing else in the suite would notice.
func TestEverySetQueryParameterIsDeclaredInSource(t *testing.T) {
	for _, s := range apiSpecs {
		checkSetParams(t, s)
	}
}

func checkSetParams(t *testing.T, s apiSpec) {
	spec := loadSpec(t, s)
	byFile := map[string][]callSite{}
	for _, c := range discoverCalls(t, spec, s) {
		byFile[c.file] = append(byFile[c.file], c)
	}

	for file, src := range providerSource(t) {
		sites := byFile[file]
		if len(sites) == 0 {
			continue
		}
		for _, m := range setRE.FindAllStringSubmatch(src, -1) {
			param := m[1]
			declared := false
			for _, c := range sites {
				for _, p := range specParams(spec, c.method, c.path) {
					if p == param {
						declared = true
					}
				}
			}
			if !declared {
				t.Errorf("%s sends query parameter %q, which none of the endpoints it calls declares", file, param)
			}
		}
	}
}
