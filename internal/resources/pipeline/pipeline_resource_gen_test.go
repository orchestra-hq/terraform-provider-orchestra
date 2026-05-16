package pipeline_test
import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/resources/pipeline"
)

// Schema tests

func TestPipelineResourceSchema_RequiredAttributes(t *testing.T) {
	ctx := context.Background()
	s := pipeline.PipelineResourceSchema(ctx)

	for _, name := range []string{"alias", "published", "data"} {
		a, ok := s.Attributes[name]
		if !ok {
			t.Errorf("attribute %q not found in schema", name)
			continue
		}
		if !isRequired(a) {
			t.Errorf("attribute %q should be Required", name)
		}
	}
}

func TestPipelineResourceSchema_ComputedAttributes(t *testing.T) {
	ctx := context.Background()
	s := pipeline.PipelineResourceSchema(ctx)

	for _, name := range []string{"id", "name", "created_at", "updated_at", "num_tasks", "paused", "yaml_path"} {
		a, ok := s.Attributes[name]
		if !ok {
			t.Errorf("attribute %q not found in schema", name)
			continue
		}
		if !isComputed(a) {
			t.Errorf("attribute %q should be Computed", name)
		}
	}
}

func TestPipelineResourceSchema_AttributeTypes(t *testing.T) {
	ctx := context.Background()
	s := pipeline.PipelineResourceSchema(ctx)

	tests := []struct {
		name     string
		wantType string
	}{
		{"alias", "string"},
		{"published", "bool"},
		{"num_tasks", "int64"},
		{"paused", "bool"},
		{"data", "singleNested"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := s.Attributes[tc.name]
			if !ok {
				t.Fatalf("attribute %q not found", tc.name)
			}
			switch tc.wantType {
			case "string":
				if _, ok := a.(rschema.StringAttribute); !ok {
					t.Errorf("expected StringAttribute, got %T", a)
				}
			case "bool":
				if _, ok := a.(rschema.BoolAttribute); !ok {
					t.Errorf("expected BoolAttribute, got %T", a)
				}
			case "int64":
				if _, ok := a.(rschema.Int64Attribute); !ok {
					t.Errorf("expected Int64Attribute, got %T", a)
				}
			case "singleNested":
				if _, ok := a.(rschema.SingleNestedAttribute); !ok {
					t.Errorf("expected SingleNestedAttribute, got %T", a)
				}
			}
		})
	}
}

func TestPipelineResourceSchema_StorageProviderDefault(t *testing.T) {
	ctx := context.Background()
	s := pipeline.PipelineResourceSchema(ctx)

	a, ok := s.Attributes["storage_provider"]
	if !ok {
		t.Fatal("storage_provider not found")
	}
	sa, ok := a.(rschema.StringAttribute)
	if !ok {
		t.Fatal("storage_provider is not a StringAttribute")
	}
	if !sa.Optional || !sa.Computed {
		t.Error("storage_provider should be Optional and Computed")
	}
	if sa.Default == nil {
		t.Error("storage_provider should have a default value")
	}
}

// DataType tests

func TestDataType_String(t *testing.T) {
	if got := (pipeline.DataType{}).String(); got != "DataType" {
		t.Errorf("String() = %q, want DataType", got)
	}
}

func TestDataType_Equal(t *testing.T) {
	emptyObjectType := types.ObjectType{AttrTypes: map[string]attr.Type{}}
	tests := []struct {
		name  string
		other attr.Type
		want  bool
	}{
		{
			name:  "same type",
			other: pipeline.DataType{ObjectType: emptyObjectType},
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
			dt := pipeline.DataType{ObjectType: emptyObjectType}
			if got := dt.Equal(tc.other); got != tc.want {
				t.Errorf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDataType_ValueFromObject(t *testing.T) {
	ctx := context.Background()
	dt := pipeline.DataType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
	objVal, diags := types.ObjectValue(map[string]attr.Type{}, map[string]attr.Value{})
	if diags.HasError() {
		t.Fatalf("setup: %v", diags)
	}
	val, diags := dt.ValueFromObject(ctx, objVal)
	if diags.HasError() {
		t.Fatalf("ValueFromObject: %v", diags)
	}
	if val.IsNull() || val.IsUnknown() {
		t.Error("expected known value")
	}
}

func TestDataType_ValueFromTerraform(t *testing.T) {
	ctx := context.Background()
	dt := pipeline.DataType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
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
		{
			name: "known empty object → known",
			in:   tftypes.NewValue(objType, map[string]tftypes.Value{}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, err := dt.ValueFromTerraform(ctx, tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNull && !val.IsNull() {
				t.Error("expected null")
			}
			if tc.wantUnknown && !val.IsUnknown() {
				t.Error("expected unknown")
			}
			if !tc.wantNull && !tc.wantUnknown && (val.IsNull() || val.IsUnknown()) {
				t.Error("expected known")
			}
		})
	}
}

// DataValue constructor tests

func TestNewDataValueNull(t *testing.T) {
	v := pipeline.NewDataValueNull()
	if !v.IsNull() {
		t.Error("IsNull() should be true")
	}
	if v.IsUnknown() {
		t.Error("IsUnknown() should be false")
	}
}

func TestNewDataValueUnknown(t *testing.T) {
	v := pipeline.NewDataValueUnknown()
	if !v.IsUnknown() {
		t.Error("IsUnknown() should be true")
	}
	if v.IsNull() {
		t.Error("IsNull() should be false")
	}
}

func TestNewDataValue(t *testing.T) {
	tests := []struct {
		name      string
		attrTypes map[string]attr.Type
		attrs     map[string]attr.Value
		wantError bool
	}{
		{
			name:      "empty maps succeed",
			attrTypes: map[string]attr.Type{},
			attrs:     map[string]attr.Value{},
		},
		{
			name:      "extra attribute returns error",
			attrTypes: map[string]attr.Type{},
			attrs:     map[string]attr.Value{"extra": types.StringValue("x")},
			wantError: true,
		},
		{
			name:      "missing attribute returns error",
			attrTypes: map[string]attr.Type{"required": types.StringType},
			attrs:     map[string]attr.Value{},
			wantError: true,
		},
		{
			name:      "type mismatch returns error",
			attrTypes: map[string]attr.Type{"field": types.StringType},
			attrs:     map[string]attr.Value{"field": types.BoolValue(true)},
			wantError: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := pipeline.NewDataValue(tc.attrTypes, tc.attrs)
			if tc.wantError && !diags.HasError() {
				t.Error("expected error, got none")
			}
			if !tc.wantError && diags.HasError() {
				t.Errorf("unexpected error: %v", diags)
			}
		})
	}
}

func TestNewDataValueMust_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from NewDataValueMust on invalid input")
		}
	}()
	pipeline.NewDataValueMust(
		map[string]attr.Type{},
		map[string]attr.Value{"extra": types.StringValue("x")},
	)
}

// DataValue method tests

func TestDataValue_ToTerraformValue(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		v           pipeline.DataValue
		wantNull    bool
		wantUnknown bool
	}{
		{
			name:     "null",
			v:        pipeline.NewDataValueNull(),
			wantNull: true,
		},
		{
			name:        "unknown",
			v:           pipeline.NewDataValueUnknown(),
			wantUnknown: true,
		},
		{
			name: "known",
			v:    func() pipeline.DataValue { v, _ := pipeline.NewDataValue(nil, nil); return v }(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tfv, err := tc.v.ToTerraformValue(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNull && !tfv.IsNull() {
				t.Error("expected null tftypes.Value")
			}
			if tc.wantUnknown && tfv.IsKnown() {
				t.Error("expected unknown tftypes.Value")
			}
			if !tc.wantNull && !tc.wantUnknown && (!tfv.IsKnown() || tfv.IsNull()) {
				t.Error("expected known non-null tftypes.Value")
			}
		})
	}
}

func TestDataValue_ToObjectValue(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		v           pipeline.DataValue
		wantNull    bool
		wantUnknown bool
	}{
		{"null → null ObjectValue", pipeline.NewDataValueNull(), true, false},
		{"unknown → unknown ObjectValue", pipeline.NewDataValueUnknown(), false, true},
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

func TestDataValue_Equal(t *testing.T) {
	knownVal, _ := pipeline.NewDataValue(map[string]attr.Type{}, map[string]attr.Value{})
	tests := []struct {
		name string
		a    pipeline.DataValue
		b    attr.Value
		want bool
	}{
		{"null == null", pipeline.NewDataValueNull(), pipeline.NewDataValueNull(), true},
		{"unknown == unknown", pipeline.NewDataValueUnknown(), pipeline.NewDataValueUnknown(), true},
		{"known == known", knownVal, knownVal, true},
		{"null != unknown", pipeline.NewDataValueNull(), pipeline.NewDataValueUnknown(), false},
		{"null != known", pipeline.NewDataValueNull(), knownVal, false},
		{"wrong type", pipeline.NewDataValueNull(), types.StringNull(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Equal(tc.b); got != tc.want {
				t.Errorf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDataValue_Type(t *testing.T) {
	ctx := context.Background()
	v := pipeline.NewDataValueNull()
	if _, ok := v.Type(ctx).(pipeline.DataType); !ok {
		t.Errorf("Type() should return DataType, got %T", v.Type(ctx))
	}
}

func TestDataValue_AttributeTypes_Empty(t *testing.T) {
	ctx := context.Background()
	if attrTypes := (pipeline.NewDataValueNull()).AttributeTypes(ctx); len(attrTypes) != 0 {
		t.Errorf("AttributeTypes() should be empty, got %d entries", len(attrTypes))
	}
}

// helpers

func isRequired(a rschema.Attribute) bool {
	switch v := a.(type) {
	case rschema.StringAttribute:
		return v.Required
	case rschema.BoolAttribute:
		return v.Required
	case rschema.Int64Attribute:
		return v.Required
	case rschema.SingleNestedAttribute:
		return v.Required
	}
	return false
}

func isComputed(a rschema.Attribute) bool {
	switch v := a.(type) {
	case rschema.StringAttribute:
		return v.Computed
	case rschema.BoolAttribute:
		return v.Computed
	case rschema.Int64Attribute:
		return v.Computed
	case rschema.SingleNestedAttribute:
		return v.Computed
	}
	return false
}
