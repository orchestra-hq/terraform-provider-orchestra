package task_runs_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/task_runs"
)

// Schema tests

func TestTaskRunsDataSourceSchema_FilterAttributes(t *testing.T) {
	ctx := context.Background()
	s := task_runs.TaskRunsDataSourceSchema(ctx)

	filters := []string{"integration", "pipeline_ids", "status", "task_run_ids", "time_from", "time_to"}
	for _, name := range filters {
		t.Run(name, func(t *testing.T) {
			a, ok := s.Attributes[name]
			if !ok {
				t.Fatalf("expected filter attribute %q to exist", name)
			}
			sa, ok := a.(dschema.StringAttribute)
			if !ok {
				t.Fatalf("expected %q to be StringAttribute, got %T", name, a)
			}
			if !sa.Optional {
				t.Errorf("filter attribute %q should be Optional", name)
			}
			if sa.Required || sa.Computed {
				t.Errorf("filter attribute %q should only be Optional", name)
			}
		})
	}
}

func TestTaskRunsDataSourceSchema_ResultsAttribute(t *testing.T) {
	ctx := context.Background()
	s := task_runs.TaskRunsDataSourceSchema(ctx)

	a, ok := s.Attributes["results"]
	if !ok {
		t.Fatal("expected results attribute")
	}
	setAttr, ok := a.(dschema.SetNestedAttribute)
	if !ok {
		t.Fatalf("results should be SetNestedAttribute, got %T", a)
	}
	if !setAttr.Computed {
		t.Error("results should be Computed")
	}
}

func TestTaskRunsDataSourceSchema_ResultsNestedStringFields(t *testing.T) {
	ctx := context.Background()
	nested := task_runs.TaskRunsDataSourceSchema(ctx).
		Attributes["results"].(dschema.SetNestedAttribute).NestedObject.Attributes

	stringFields := []string{
		"account_id", "completed_at", "connection_id", "created_at",
		"external_message", "external_status", "id", "integration",
		"integration_job", "message", "pipeline_id", "pipeline_run_id",
		"platform_link", "run_parameters", "started_at", "status",
		"task_id", "task_name", "task_parameters", "updated_at",
	}
	for _, name := range stringFields {
		t.Run(name, func(t *testing.T) {
			a, ok := nested[name]
			if !ok {
				t.Fatalf("expected nested field %q", name)
			}
			if _, ok := a.(dschema.StringAttribute); !ok {
				t.Errorf("field %q should be StringAttribute, got %T", name, a)
			}
		})
	}
}

func TestTaskRunsDataSourceSchema_ResultsNestedInt64Fields(t *testing.T) {
	ctx := context.Background()
	nested := task_runs.TaskRunsDataSourceSchema(ctx).
		Attributes["results"].(dschema.SetNestedAttribute).NestedObject.Attributes

	a, ok := nested["number_of_attempts"]
	if !ok {
		t.Fatal("expected number_of_attempts field")
	}
	if _, ok := a.(dschema.Int64Attribute); !ok {
		t.Errorf("number_of_attempts should be Int64Attribute, got %T", a)
	}
}

// ResultsType tests

func TestResultsType_String(t *testing.T) {
	rt := task_runs.ResultsType{}
	if got := rt.String(); got != "ResultsType" {
		t.Errorf("String() = %q, want ResultsType", got)
	}
}

func TestResultsType_Equal(t *testing.T) {
	emptyObjectType := types.ObjectType{AttrTypes: map[string]attr.Type{}}
	tests := []struct {
		name  string
		other attr.Type
		want  bool
	}{
		{
			name:  "same type",
			other: task_runs.ResultsType{ObjectType: emptyObjectType},
			want:  true,
		},
		{
			name:  "string type",
			other: types.StringType,
			want:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := task_runs.ResultsType{ObjectType: emptyObjectType}
			if got := rt.Equal(tc.other); got != tc.want {
				t.Errorf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResultsType_ValueFromTerraform(t *testing.T) {
	ctx := context.Background()
	rt := task_runs.ResultsType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}

	tests := []struct {
		name        string
		in          tftypes.Value
		wantNull    bool
		wantUnknown bool
	}{
		{
			name:     "nil type → null",
			in:       tftypes.Value{},
			wantNull: true,
		},
		{
			name:        "unknown value → unknown",
			in:          tftypes.NewValue(objType, tftypes.UnknownValue),
			wantUnknown: true,
		},
		{
			name:     "null value → null",
			in:       tftypes.NewValue(objType, nil),
			wantNull: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, err := rt.ValueFromTerraform(ctx, tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNull && !val.IsNull() {
				t.Error("expected null")
			}
			if tc.wantUnknown && !val.IsUnknown() {
				t.Error("expected unknown")
			}
		})
	}
}

// ResultsValue constructor tests

func TestNewResultsValueNull(t *testing.T) {
	v := task_runs.NewResultsValueNull()
	if !v.IsNull() {
		t.Error("IsNull() should be true")
	}
	if v.IsUnknown() {
		t.Error("IsUnknown() should be false")
	}
}

func TestNewResultsValueUnknown(t *testing.T) {
	v := task_runs.NewResultsValueUnknown()
	if !v.IsUnknown() {
		t.Error("IsUnknown() should be true")
	}
	if v.IsNull() {
		t.Error("IsNull() should be false")
	}
}

func TestNewResultsValue_ErrorCases(t *testing.T) {
	tests := []struct {
		name      string
		attrTypes map[string]attr.Type
		attrs     map[string]attr.Value
	}{
		{
			name:      "extra attribute returns error",
			attrTypes: map[string]attr.Type{},
			attrs:     map[string]attr.Value{"extra": types.StringValue("x")},
		},
		{
			name:      "missing attribute returns error",
			attrTypes: map[string]attr.Type{"required": types.StringType},
			attrs:     map[string]attr.Value{},
		},
		{
			name:      "type mismatch returns error",
			attrTypes: map[string]attr.Type{"field": types.StringType},
			attrs:     map[string]attr.Value{"field": types.BoolValue(true)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := task_runs.NewResultsValue(tc.attrTypes, tc.attrs)
			if !diags.HasError() {
				t.Error("expected error, got none")
			}
		})
	}
}

func TestNewResultsValueMust_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from NewResultsValueMust on invalid input")
		}
	}()
	task_runs.NewResultsValueMust(
		map[string]attr.Type{},
		map[string]attr.Value{"extra": types.StringValue("x")},
	)
}

// ResultsValue method tests

func TestResultsValue_ToObjectValue_States(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		v           task_runs.ResultsValue
		wantNull    bool
		wantUnknown bool
	}{
		{"null → null ObjectValue", task_runs.NewResultsValueNull(), true, false},
		{"unknown → unknown ObjectValue", task_runs.NewResultsValueUnknown(), false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj, diags := tc.v.ToObjectValue(ctx)
			if diags.HasError() {
				t.Fatalf("unexpected error: %v", diags)
			}
			if tc.wantNull && !obj.IsNull() {
				t.Error("expected null ObjectValue")
			}
			if tc.wantUnknown && !obj.IsUnknown() {
				t.Error("expected unknown ObjectValue")
			}
		})
	}
}

func TestResultsValue_Equal(t *testing.T) {
	null1 := task_runs.NewResultsValueNull()
	null2 := task_runs.NewResultsValueNull()
	unknown := task_runs.NewResultsValueUnknown()

	tests := []struct {
		name string
		a    task_runs.ResultsValue
		b    attr.Value
		want bool
	}{
		{"null == null", null1, null2, true},
		{"unknown == unknown", unknown, unknown, true},
		{"null != unknown", null1, unknown, false},
		{"wrong type", null1, types.StringNull(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Equal(tc.b); got != tc.want {
				t.Errorf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResultsValue_Type(t *testing.T) {
	ctx := context.Background()
	v := task_runs.NewResultsValueNull()
	if _, ok := v.Type(ctx).(task_runs.ResultsType); !ok {
		t.Errorf("Type() should return ResultsType, got %T", v.Type(ctx))
	}
}
