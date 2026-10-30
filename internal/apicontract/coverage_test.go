package apicontract_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// coverage.yaml lists every operation the provider deliberately does not
// implement, grouped by why. It exists so that a spec refresh which adds an
// endpoint cannot pass silently: the new operation is neither called by the
// provider nor listed there, and this test fails until someone decides.
//
// The decision is the point. Most new endpoints are run history, incidents or
// one-shot actions that do not belong in Terraform state; a few are
// declarative configuration that does. Without a forcing function the second
// kind sit unnoticed — /monitors arrived in the spec and went unbuilt for days.
type coverageCategory struct {
	Reason     string   `yaml:"reason"`
	Operations []string `yaml:"operations"`
	// Properties are "Schema.property" entries the provider leaves out of a
	// request body it sends; see TestEveryWrittenPropertyIsClassified.
	Properties []string `yaml:"properties"`
}

func loadCoverage(t *testing.T) map[string]map[string]coverageCategory {
	t.Helper()
	raw, err := os.ReadFile("coverage.yaml")
	if err != nil {
		t.Fatalf("read coverage.yaml: %v", err)
	}
	var out map[string]map[string]coverageCategory
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse coverage.yaml: %v", err)
	}
	return out
}

// TestEverySpecOperationIsClassified checks each committed spec. admin is not
// committed, so its entries in coverage.yaml are not checked here.
func TestEverySpecOperationIsClassified(t *testing.T) {
	coverage := loadCoverage(t)
	for _, s := range apiSpecs {
		t.Run(s.name, func(t *testing.T) { checkClassified(t, s, coverage[s.name]) })
	}
}

func checkClassified(t *testing.T, s apiSpec, ledger map[string]coverageCategory) {
	spec := loadSpec(t, s)

	called := map[string]bool{}
	for _, c := range discoverCalls(t, spec, s) {
		called[c.method+" "+c.path] = true
	}

	excluded := map[string]string{}
	for name, cat := range ledger {
		if strings.TrimSpace(cat.Reason) == "" {
			t.Errorf("coverage.yaml: category %q has no reason; the reason is the point of the file", name)
		}
		for _, op := range cat.Operations {
			if prev, dup := excluded[op]; dup {
				t.Errorf("coverage.yaml: %q is in both %q and %q", op, prev, name)
			}
			excluded[op] = name
		}
	}

	inSpec := map[string]bool{}
	var unclassified []string
	for path, item := range spec.Paths {
		for method := range item {
			op := strings.ToUpper(method) + " " + path
			inSpec[op] = true
			switch {
			case called[op] && excluded[op] != "":
				t.Errorf("coverage.yaml lists %q as %q, but the provider now calls it — delete the entry", op, excluded[op])
			case !called[op] && excluded[op] == "":
				unclassified = append(unclassified, op)
			}
		}
	}

	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Errorf("%d spec operation(s) are neither called by the provider nor listed in coverage.yaml.\n"+
			"Decide for each: build a resource or data source for it, or add it to a category with a reason.\n"+
			"Declarative configuration belongs in Terraform; run history, incidents and one-shot actions do not.\n\n  %s",
			len(unclassified), strings.Join(unclassified, "\n  "))
	}

	var stale []string
	for op, cat := range excluded {
		if !inSpec[op] {
			stale = append(stale, fmt.Sprintf("%s (in %q)", op, cat))
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("coverage.yaml lists operation(s) the spec no longer has — the endpoint was removed or renamed upstream, so delete the entry:\n  %s",
			strings.Join(stale, "\n  "))
	}
}
