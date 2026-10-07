package main

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// category is one reason the provider leaves a group of operations alone.
type category struct {
	Reason     string   `yaml:"reason"`
	Operations []string `yaml:"operations"`
}

// ledger records, per spec, every operation the provider deliberately does not
// implement and why. internal/apicontract/coverage.yaml is the committed copy.
//
// It lists exclusions only. What the provider does implement is read from its
// own source by the contract tests, so the two cannot disagree for long: an
// operation that is neither called nor listed fails the build.
type ledger map[string]map[string]category

func loadLedger(b []byte) (ledger, error) {
	var l ledger
	if err := yaml.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("parse coverage ledger: %w", err)
	}
	return l, nil
}

// excluded maps each operation the ledger excludes for a spec to its category.
func (l ledger) excluded(spec string) map[string]string {
	out := map[string]string{}
	for name, c := range l[spec] {
		for _, op := range c.Operations {
			out[op] = name
		}
	}
	return out
}

// excludedElsewhere maps each operation the ledger classifies under a spec
// other than the given one to "spec/category". When the docs site moves an
// endpoint between specs, this is how the old home learns where it went.
func (l ledger) excludedElsewhere(spec string) map[string]string {
	out := map[string]string{}
	for other, cats := range l {
		if other == spec {
			continue
		}
		for name, c := range cats {
			for _, op := range c.Operations {
				out[op] = other + "/" + name
			}
		}
	}
	return out
}
