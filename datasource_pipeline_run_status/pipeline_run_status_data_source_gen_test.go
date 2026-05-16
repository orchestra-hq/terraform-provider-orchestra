package datasource_pipeline_run_status_test

import (
	"context"
	"testing"

	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/orchestra-hq/terraform-provider-orchestra/datasource_pipeline_run_status"
)

func TestPipelineRunStatusDataSourceSchema_RequiredAttributes(t *testing.T) {
	ctx := context.Background()
	s := datasource_pipeline_run_status.PipelineRunStatusDataSourceSchema(ctx)

	a, ok := s.Attributes["id"]
	if !ok {
		t.Fatal("expected attribute id to exist")
	}
	sa, ok := a.(dschema.StringAttribute)
	if !ok {
		t.Fatalf("expected id to be StringAttribute, got %T", a)
	}
	if !sa.Required {
		t.Error("id should be Required")
	}
	if sa.Optional || sa.Computed {
		t.Error("id should only be Required")
	}
}

func TestPipelineRunStatusDataSourceSchema_ComputedAttributes(t *testing.T) {
	ctx := context.Background()
	s := datasource_pipeline_run_status.PipelineRunStatusDataSourceSchema(ctx)

	tests := []struct {
		name string
	}{
		{"pipeline_id"},
		{"pipeline_name"},
		{"run_status"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := s.Attributes[tc.name]
			if !ok {
				t.Fatalf("expected attribute %q to exist", tc.name)
			}
			sa, ok := a.(dschema.StringAttribute)
			if !ok {
				t.Fatalf("expected %q to be StringAttribute, got %T", tc.name, a)
			}
			if !sa.Computed {
				t.Errorf("attribute %q should be Computed", tc.name)
			}
		})
	}
}

func TestPipelineRunStatusDataSourceSchema_AttributeCount(t *testing.T) {
	ctx := context.Background()
	s := datasource_pipeline_run_status.PipelineRunStatusDataSourceSchema(ctx)

	if got := len(s.Attributes); got != 4 {
		t.Errorf("expected 4 attributes, got %d", got)
	}
}
