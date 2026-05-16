package datasource_assets_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/datasource_assets"
)

// Schema tests

func TestAssetsDataSourceSchema_FilterAttributes(t *testing.T) {
	ctx := context.Background()
	s := datasource_assets.AssetsDataSourceSchema(ctx)

	for _, name := range []string{"asset_type", "integration"} {
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

func TestAssetsDataSourceSchema_ResultsAttribute(t *testing.T) {
	ctx := context.Background()
	s := datasource_assets.AssetsDataSourceSchema(ctx)

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

func TestAssetsDataSourceSchema_ResultsNestedStringFields(t *testing.T) {
	ctx := context.Background()
	s := datasource_assets.AssetsDataSourceSchema(ctx)
	nested := s.Attributes["results"].(dschema.SetNestedAttribute).NestedObject.Attributes

	stringFields := []string{
		"account_id", "asset_id", "asset_name", "asset_type",
		"connection", "connection_integration", "created_at",
		"created_in_integration", "database_name", "external_id",
		"integration", "integration_account_id", "integration_asset_type",
		"last_updated_in_integration", "owner", "schema_name",
		"status", "table_name", "updated_at",
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

func TestAssetsDataSourceSchema_ResultsNestedInt64Fields(t *testing.T) {
	ctx := context.Background()
	nested := datasource_assets.AssetsDataSourceSchema(ctx).
		Attributes["results"].(dschema.SetNestedAttribute).NestedObject.Attributes

	for _, name := range []string{"bytes", "row_count"} {
		t.Run(name, func(t *testing.T) {
			a, ok := nested[name]
			if !ok {
				t.Fatalf("expected nested field %q", name)
			}
			if _, ok := a.(dschema.Int64Attribute); !ok {
				t.Errorf("field %q should be Int64Attribute, got %T", name, a)
			}
		})
	}
}

func TestAssetsDataSourceSchema_ResultsNestedFloat64Field(t *testing.T) {
	ctx := context.Background()
	nested := datasource_assets.AssetsDataSourceSchema(ctx).
		Attributes["results"].(dschema.SetNestedAttribute).NestedObject.Attributes

	a, ok := nested["usage_percentage"]
	if !ok {
		t.Fatal("expected usage_percentage field")
	}
	if _, ok := a.(dschema.Float64Attribute); !ok {
		t.Errorf("usage_percentage should be Float64Attribute, got %T", a)
	}
}

func TestAssetsDataSourceSchema_ResultsNestedListFields(t *testing.T) {
	ctx := context.Background()
	nested := datasource_assets.AssetsDataSourceSchema(ctx).
		Attributes["results"].(dschema.SetNestedAttribute).NestedObject.Attributes

	for _, name := range []string{"downstream_dependencies", "owners", "upstream_dependencies"} {
		t.Run(name, func(t *testing.T) {
			a, ok := nested[name]
			if !ok {
				t.Fatalf("expected nested field %q", name)
			}
			la, ok := a.(dschema.ListAttribute)
			if !ok {
				t.Errorf("field %q should be ListAttribute, got %T", name, a)
				return
			}
			if !la.Computed {
				t.Errorf("field %q should be Computed", name)
			}
		})
	}
}

// ResultsType tests

func TestResultsType_String(t *testing.T) {
	rt := datasource_assets.ResultsType{}
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
			other: datasource_assets.ResultsType{ObjectType: emptyObjectType},
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
			rt := datasource_assets.ResultsType{ObjectType: emptyObjectType}
			if got := rt.Equal(tc.other); got != tc.want {
				t.Errorf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResultsType_ValueFromTerraform(t *testing.T) {
	ctx := context.Background()
	rt := datasource_assets.ResultsType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
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
	v := datasource_assets.NewResultsValueNull()
	if !v.IsNull() {
		t.Error("IsNull() should be true")
	}
	if v.IsUnknown() {
		t.Error("IsUnknown() should be false")
	}
}

func TestNewResultsValueUnknown(t *testing.T) {
	v := datasource_assets.NewResultsValueUnknown()
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
			_, diags := datasource_assets.NewResultsValue(tc.attrTypes, tc.attrs)
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
	datasource_assets.NewResultsValueMust(
		map[string]attr.Type{},
		map[string]attr.Value{"extra": types.StringValue("x")},
	)
}

// ResultsValue method tests

func TestResultsValue_ToObjectValue_States(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		v           datasource_assets.ResultsValue
		wantNull    bool
		wantUnknown bool
	}{
		{"null → null ObjectValue", datasource_assets.NewResultsValueNull(), true, false},
		{"unknown → unknown ObjectValue", datasource_assets.NewResultsValueUnknown(), false, true},
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
	null1 := datasource_assets.NewResultsValueNull()
	null2 := datasource_assets.NewResultsValueNull()
	unknown := datasource_assets.NewResultsValueUnknown()

	tests := []struct {
		name string
		a    datasource_assets.ResultsValue
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
	v := datasource_assets.NewResultsValueNull()
	if _, ok := v.Type(ctx).(datasource_assets.ResultsType); !ok {
		t.Errorf("Type() should return ResultsType, got %T", v.Type(ctx))
	}
}
