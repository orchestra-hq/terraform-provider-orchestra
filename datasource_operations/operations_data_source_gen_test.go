package datasource_operations_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/datasource_operations"
)

func TestOperationsDataSourceSchema_FilterAttributes(t *testing.T) {
	ctx := context.Background()
	s := datasource_operations.OperationsDataSourceSchema(ctx)

	filters := []string{"external_id", "integration", "operation_type"}
	for _, name := range filters {
		a, ok := s.Attributes[name]
		if !ok {
			t.Errorf("expected filter attribute %q to exist", name)
			continue
		}
		sa, ok := a.(dschema.StringAttribute)
		if !ok {
			t.Errorf("expected %q to be StringAttribute", name)
			continue
		}
		if !sa.Optional {
			t.Errorf("expected filter attribute %q to be Optional", name)
		}
		if sa.Required || sa.Computed {
			t.Errorf("filter attribute %q should only be Optional", name)
		}
	}
}

func TestOperationsDataSourceSchema_ResultsAttribute(t *testing.T) {
	ctx := context.Background()
	s := datasource_operations.OperationsDataSourceSchema(ctx)

	a, ok := s.Attributes["results"]
	if !ok {
		t.Fatal("expected results attribute")
	}
	setAttr, ok := a.(dschema.SetNestedAttribute)
	if !ok {
		t.Fatal("results should be SetNestedAttribute")
	}
	if !setAttr.Computed {
		t.Error("results should be Computed")
	}
}

func TestOperationsDataSourceSchema_ResultsNestedFields(t *testing.T) {
	ctx := context.Background()
	s := datasource_operations.OperationsDataSourceSchema(ctx)

	setAttr := s.Attributes["results"].(dschema.SetNestedAttribute)
	nested := setAttr.NestedObject.Attributes

	expected := []string{
		"id", "account_id", "external_id", "external_status",
		"external_detail", "integration", "completed_at",
		"inserted_at", "dependencies",
	}
	for _, field := range expected {
		if _, ok := nested[field]; !ok {
			t.Errorf("expected nested field %q in results", field)
		}
	}
}

func TestOperationsDataSourceSchema_DependenciesIsListAttribute(t *testing.T) {
	ctx := context.Background()
	s := datasource_operations.OperationsDataSourceSchema(ctx)

	nested := s.Attributes["results"].(dschema.SetNestedAttribute).NestedObject.Attributes
	a, ok := nested["dependencies"]
	if !ok {
		t.Fatal("expected dependencies field in results")
	}
	listAttr, ok := a.(dschema.ListAttribute)
	if !ok {
		t.Fatal("dependencies should be a ListAttribute")
	}
	if !listAttr.Computed {
		t.Error("dependencies should be Computed")
	}
}

// ResultsType tests

func TestResultsType_String(t *testing.T) {
	rt := datasource_operations.ResultsType{}
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
			other: datasource_operations.ResultsType{ObjectType: emptyObjectType},
			want:  true,
		},
		{
			name:  "string type",
			other: types.StringType,
			want:  false,
		},
		{
			name:  "bool type",
			other: types.BoolType,
			want:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := datasource_operations.ResultsType{ObjectType: emptyObjectType}
			if got := rt.Equal(tc.other); got != tc.want {
				t.Errorf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResultsType_ValueFromTerraform(t *testing.T) {
	ctx := context.Background()
	rt := datasource_operations.ResultsType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
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
	v := datasource_operations.NewResultsValueNull()
	if !v.IsNull() {
		t.Error("IsNull() should be true")
	}
	if v.IsUnknown() {
		t.Error("IsUnknown() should be false")
	}
}

func TestNewResultsValueUnknown(t *testing.T) {
	v := datasource_operations.NewResultsValueUnknown()
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
			_, diags := datasource_operations.NewResultsValue(tc.attrTypes, tc.attrs)
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
	datasource_operations.NewResultsValueMust(
		map[string]attr.Type{},
		map[string]attr.Value{"extra": types.StringValue("x")},
	)
}

// ResultsValue method tests

func TestResultsValue_ToObjectValue_States(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		v           datasource_operations.ResultsValue
		wantNull    bool
		wantUnknown bool
	}{
		{"null → null ObjectValue", datasource_operations.NewResultsValueNull(), true, false},
		{"unknown → unknown ObjectValue", datasource_operations.NewResultsValueUnknown(), false, true},
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
	null1 := datasource_operations.NewResultsValueNull()
	null2 := datasource_operations.NewResultsValueNull()
	unknown := datasource_operations.NewResultsValueUnknown()

	tests := []struct {
		name string
		a    datasource_operations.ResultsValue
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
	v := datasource_operations.NewResultsValueNull()
	if _, ok := v.Type(ctx).(datasource_operations.ResultsType); !ok {
		t.Errorf("Type() should return ResultsType, got %T", v.Type(ctx))
	}
}
