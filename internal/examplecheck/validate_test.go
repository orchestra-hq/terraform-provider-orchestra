package examplecheck

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hc-install/fs"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/terraform-exec/tfexec"
)

// terraformBinary finds the Terraform CLI, preferring one already on PATH and
// falling back to a download. CI installs it, so the download path is only for
// a developer who has not.
func terraformBinary(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	if bin, err := (&fs.AnyVersion{Product: &product.Terraform}).Find(ctx); err == nil {
		return bin
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	bin, err := (&releases.LatestVersion{Product: product.Terraform}).Install(ctx)
	if err != nil {
		t.Skipf("no Terraform CLI on PATH and none could be downloaded (%v); "+
			"install Terraform to run the example validation", err)
	}
	return bin
}

// buildProvider compiles the provider so Terraform can load it through a
// dev_overrides block, and returns the directory holding the binary.
func buildProvider(t *testing.T, dir string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	out := filepath.Join(dir, "terraform-provider-orchestra")
	cmd := exec.Command("go", "build", "-o", out, root)
	cmd.Dir = root
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the provider failed: %v\n%s", err, combined)
	}
	return dir
}

// TestExamplesValidate runs `terraform validate` over every example.
//
// tfplugindocs copies these files verbatim into docs/, so a broken example is
// published documentation nobody can apply. Terraform catches what the schema
// check in examples_test.go cannot: bad enum values, missing required
// attributes, and type errors inside nested objects. An earlier version of
// examples/resources/orchestra_pipeline used integration_job =
// "FIVETRAN_RUN_SYNC", which no validator in the repo rejected but Terraform
// does.
//
// No API key is involved: validate never contacts Orchestra.
func TestExamplesValidate(t *testing.T) {
	tfBin := terraformBinary(t)

	work := t.TempDir()
	binDir := buildProvider(t, filepath.Join(work, "bin"))
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	// dev_overrides lets Terraform load the freshly built provider without a
	// registry round trip, and makes `terraform init` unnecessary.
	tfrc := filepath.Join(work, "dev.tfrc")
	if err := os.WriteFile(tfrc, []byte(`provider_installation {
  dev_overrides {
    "orchestra-hq/orchestra" = "`+binDir+`"
  }
  direct {}
}
`), 0o600); err != nil {
		t.Fatalf("write CLI config: %v", err)
	}

	root := examplesDir(t)
	dirs, err := exampleDirs(root)
	if err != nil {
		t.Fatalf("list examples: %v", err)
	}
	if len(dirs) == 0 {
		t.Fatal("no example directories found")
	}

	for _, dir := range dirs {
		name := rel(root, dir)
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Validate a copy: the examples deliberately carry no
			// `required_providers` block, because it would be noise on every
			// documentation page, but Terraform needs one or it resolves
			// hashicorp/orchestra instead of orchestra-hq/orchestra.
			caseDir := t.TempDir()
			if err := copyTF(dir, caseDir); err != nil {
				t.Fatalf("copy example: %v", err)
			}
			if !declaresProvider(caseDir) {
				versions := filepath.Join(caseDir, "versions.tf")
				if err := os.WriteFile(versions, []byte(`terraform {
  required_providers {
    orchestra = {
      source = "orchestra-hq/orchestra"
    }
  }
}
`), 0o600); err != nil {
					t.Fatalf("write versions.tf: %v", err)
				}
			}

			tf, err := tfexec.NewTerraform(caseDir, tfBin)
			if err != nil {
				t.Fatalf("tfexec: %v", err)
			}
			if err := tf.SetEnv(map[string]string{"TF_CLI_CONFIG_FILE": tfrc}); err != nil {
				t.Fatalf("set env: %v", err)
			}

			out, err := tf.Validate(context.Background())
			if err != nil {
				t.Fatalf("terraform validate failed to run: %v", err)
			}
			if !out.Valid {
				for _, d := range out.Diagnostics {
					where := ""
					if d.Range != nil {
						where = filepath.Base(d.Range.Filename) + ":" +
							itoa(d.Range.Start.Line) + ": "
					}
					t.Errorf("%s%s: %s", where, d.Summary, d.Detail)
				}
			}
		})
	}
}

func exampleDirs(root string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || p == root {
			return err
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tf") {
				out = append(out, p)
				break
			}
		}
		return nil
	})
	return out, err
}

func copyTF(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func declaresProvider(dir string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil &&
			strings.Contains(string(b), "required_providers") {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
