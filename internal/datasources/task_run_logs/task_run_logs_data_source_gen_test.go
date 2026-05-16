package task_run_logs_test
import (
	"context"
	"testing"

	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/task_run_logs"
)

func TestTaskRunLogsDataSourceSchema_RequiredAttributes(t *testing.T) {
	ctx := context.Background()
	s := task_run_logs.TaskRunLogsDataSourceSchema(ctx)

	tests := []struct {
		name string
	}{
		{"pipeline_run_id"},
		{"task_run_id"},
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
			if !sa.Required {
				t.Errorf("attribute %q should be Required", tc.name)
			}
		})
	}
}

func TestTaskRunLogsDataSourceSchema_FilenamesAttribute(t *testing.T) {
	ctx := context.Background()
	s := task_run_logs.TaskRunLogsDataSourceSchema(ctx)

	a, ok := s.Attributes["filenames"]
	if !ok {
		t.Fatal("expected filenames attribute to exist")
	}
	la, ok := a.(dschema.ListAttribute)
	if !ok {
		t.Fatalf("expected filenames to be ListAttribute, got %T", a)
	}
	if !la.Computed {
		t.Error("filenames should be Computed")
	}
	if !la.ElementType.Equal(types.StringType) {
		t.Errorf("filenames element type should be StringType, got %s", la.ElementType)
	}
}

func TestTaskRunLogsDataSourceSchema_AttributeCount(t *testing.T) {
	ctx := context.Background()
	s := task_run_logs.TaskRunLogsDataSourceSchema(ctx)

	if got := len(s.Attributes); got != 3 {
		t.Errorf("expected 3 attributes, got %d", got)
	}
}
