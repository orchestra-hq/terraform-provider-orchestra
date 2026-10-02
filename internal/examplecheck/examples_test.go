// Package examplecheck validates the files under examples/.
//
// tfplugindocs copies these verbatim into docs/, so an example naming an
// attribute the schema does not have ships straight to the registry as
// documentation nobody can apply. Terraform itself would catch it, but the
// Terraform CLI is not needed here: the provider's own schema is the authority.
package examplecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource/schema"

	orchestra "github.com/orchestra-hq/terraform-provider-orchestra/internal/provider"

	"context"
)

func examplesDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatalf("resolve examples dir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("examples dir: %v", err)
	}
	return dir
}

// schemas returns the top-level attribute and block names for every resource
// and data source the provider registers.
func schemas(t *testing.T) (map[string]map[string]bool, map[string]map[string]bool) {
	t.Helper()
	ctx := context.Background()
	p := orchestra.New("test")()

	resources := map[string]map[string]bool{}
	for _, f := range p.(provider.Provider).Resources(ctx) {
		r := f()
		var md resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "orchestra"}, &md)
		var sr resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sr)
		resources[md.TypeName] = topLevelNames(sr.Schema)
	}

	dataSources := map[string]map[string]bool{}
	for _, f := range p.(provider.Provider).DataSources(ctx) {
		d := f()
		var md datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "orchestra"}, &md)
		var sr datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &sr)
		dataSources[md.TypeName] = topLevelNamesDS(sr.Schema)
	}
	return resources, dataSources
}

func topLevelNames(s fwresource.Schema) map[string]bool {
	out := map[string]bool{}
	for name := range s.Attributes {
		out[name] = true
	}
	for name := range s.Blocks {
		out[name] = true
	}
	return out
}

func topLevelNamesDS(s fwdatasource.Schema) map[string]bool {
	out := map[string]bool{}
	for name := range s.Attributes {
		out[name] = true
	}
	for name := range s.Blocks {
		out[name] = true
	}
	return out
}

// TestExamplesUseRealAttributes walks every example and checks that each
// resource/data block names a registered type and sets only attributes that
// type actually has.
func TestExamplesUseRealAttributes(t *testing.T) {
	resources, dataSources := schemas(t)
	root := examplesDir(t)

	var checked int
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".tf") {
			return err
		}

		parser := hclparse.NewParser()
		file, diags := parser.ParseHCLFile(p)
		if diags.HasErrors() {
			t.Errorf("%s does not parse as HCL: %s", rel(root, p), diags.Error())
			return nil
		}

		content, _, _ := file.Body.PartialContent(&hcl.BodySchema{
			Blocks: []hcl.BlockHeaderSchema{
				{Type: "resource", LabelNames: []string{"type", "name"}},
				{Type: "data", LabelNames: []string{"type", "name"}},
			},
		})

		for _, block := range content.Blocks {
			typeName := block.Labels[0]
			if !strings.HasPrefix(typeName, "orchestra_") {
				continue
			}
			known := resources
			kind := "resource"
			if block.Type == "data" {
				known = dataSources
				kind = "data source"
			}

			attrs, ok := known[typeName]
			if !ok {
				t.Errorf("%s: %s %q is not registered by the provider", rel(root, p), kind, typeName)
				continue
			}
			checked++

			body, _, _ := block.Body.PartialContent(&hcl.BodySchema{})
			justAttrs, _ := block.Body.JustAttributes()
			_ = body
			for name := range justAttrs {
				if !attrs[name] {
					t.Errorf("%s: %s %q has no attribute %q", rel(root, p), kind, typeName, name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk examples: %v", err)
	}
	if checked == 0 {
		t.Fatal("no orchestra blocks found in examples/ — the walk is not finding them")
	}
	t.Logf("checked %d resource/data blocks", checked)
}

// TestEveryTypeHasAnExample keeps the registry docs complete: tfplugindocs
// renders an empty example section for anything missing one.
func TestEveryTypeHasAnExample(t *testing.T) {
	resources, dataSources := schemas(t)
	root := examplesDir(t)

	for name := range resources {
		if _, err := os.Stat(filepath.Join(root, "resources", name, "resource.tf")); err != nil {
			t.Errorf("resource %s has no examples/resources/%s/resource.tf", name, name)
		}
	}
	for name := range dataSources {
		if _, err := os.Stat(filepath.Join(root, "data-sources", name, "data-source.tf")); err != nil {
			t.Errorf("data source %s has no examples/data-sources/%s/data-source.tf", name, name)
		}
	}
}

func rel(root, p string) string {
	r, err := filepath.Rel(filepath.Dir(root), p)
	if err != nil {
		return p
	}
	return r
}
