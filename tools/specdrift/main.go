// Command specdrift compares the Orchestra OpenAPI specs the provider was built
// against with the published ones, and says what the provider has to do about
// the difference.
//
//	specdrift -new engine.yaml
//	specdrift -new engine.yaml -compare agents=openapi-agents.yaml,agents.yaml -also admin=admin.yaml -exit-code
//
// With -exit-code the status is 0 for no findings, 1 when something needs
// attention, 3 when there are only routine or optional findings, and 2 on error.
//
// A refresh that changes nothing the provider models should merge without
// thought, and one that does must not be missed. Silence is the failure mode:
// a monitor's groupBy turned from a list of names into a union, and a tightened
// maxLength went unvalidated, with every test still green.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// Exit statuses under -exit-code. A scheduled run branches on them: nothing to
// do, a pull request that needs a human, or one that is safe to merge as is.
const (
	exitClean     = 0
	exitAttention = 1
	exitError     = 2
	exitRoutine   = 3
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specdrift", flag.ContinueOnError)
	fs.SetOutput(stderr)

	oldPath := fs.String("old", "openapi.yaml", "the spec the provider was built against")
	newPath := fs.String("new", "", "the spec to compare it with (required)")
	ledgerPath := fs.String("coverage", "internal/apicontract/coverage.yaml", "the ledger of operations the provider leaves alone")
	exitCode := fs.Bool("exit-code", false, "exit 1 when something needs attention, 3 when there are only routine or optional findings")
	compared := map[string][2]string{}
	fs.Func("compare", "NAME=OLD,NEW: another spec the provider calls, committed and published, such as agents=openapi-agents.yaml,agents.yaml (repeatable)", func(s string) error {
		name, files, ok := strings.Cut(s, "=")
		oldFile, newFile, ok2 := strings.Cut(files, ",")
		if !ok || !ok2 || name == "" || oldFile == "" || newFile == "" {
			return fmt.Errorf("want NAME=OLD,NEW, got %q", s)
		}
		compared[name] = [2]string{oldFile, newFile}
		return nil
	})
	also := map[string]string{}
	fs.Func("also", "NAME=FILE: another spec to inventory for new endpoints only, such as admin=admin.yaml (repeatable)", func(s string) error {
		name, file, ok := strings.Cut(s, "=")
		if !ok || name == "" || file == "" {
			return fmt.Errorf("want NAME=FILE, got %q", s)
		}
		also[name] = file
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if *newPath == "" {
		fmt.Fprintln(stderr, "specdrift: -new is required")
		return exitError
	}

	in := Inputs{Compared: map[string][2]specDoc{}, Also: map[string]specDoc{}}
	var err error
	if in.Old, err = readSpec(*oldPath); err != nil {
		return fail(stderr, err)
	}
	if in.New, err = readSpec(*newPath); err != nil {
		return fail(stderr, err)
	}
	for name, files := range compared {
		var pair [2]specDoc
		for i, file := range files {
			if pair[i], err = readSpec(file); err != nil {
				return fail(stderr, err)
			}
		}
		in.Compared[name] = pair
	}
	for name, file := range also {
		if in.Also[name], err = readSpec(file); err != nil {
			return fail(stderr, err)
		}
	}
	raw, err := os.ReadFile(*ledgerPath)
	if err != nil {
		return fail(stderr, err)
	}
	if in.Ledger, err = loadLedger(raw); err != nil {
		return fail(stderr, err)
	}

	report := Compare(in)
	fmt.Fprint(stdout, report.Markdown())
	if !*exitCode {
		return exitClean
	}
	switch {
	case report.NeedsAttention():
		return exitAttention
	case len(report.findings) > 0:
		return exitRoutine
	}
	return exitClean
}

func readSpec(path string) (specDoc, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := parseSpec(b)
	if err != nil {
		// A moved spec comes back as a 404 page, which is not an OpenAPI file.
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "specdrift:", err)
	return exitError
}
