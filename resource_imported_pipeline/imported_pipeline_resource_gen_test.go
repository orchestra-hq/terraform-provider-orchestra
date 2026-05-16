package resource_imported_pipeline_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/resource_imported_pipeline"
)

func TestImportedPipelineResourceSchema_RequiredAttributes(t *testing.T) {
	ctx := context.Background()
	s := resource_imported_pipeline.ImportedPipelineResourceSchema(ctx)

	required := []string{"repository", "default_branch", "storage_provider"}
	for _, name := range required {
		a, ok := s.Attributes[name]
		if !ok {
			t.Errorf("expected attribute %q to exist", name)
			continue
		}
		sa, ok := a.(rschema.StringAttribute)
		if !ok {
			t.Errorf("expected %q to be a StringAttribute", name)
			continue
		}
		if !sa.Required {
			t.Errorf("expected attribute %q to be Required", name)
		}
	}
}

func TestImportedPipelineResourceSchema_OptionalAttributes(t *testing.T) {
	ctx := context.Background()
	s := resource_imported_pipeline.ImportedPipelineResourceSchema(ctx)

	optional := []string{"alias", "working_branch"}
	for _, name := range optional {
		a, ok := s.Attributes[name]
		if !ok {
			t.Errorf("expected attribute %q to exist", name)
			continue
		}
		sa, ok := a.(rschema.StringAttribute)
		if !ok {
			t.Errorf("expected %q to be a StringAttribute", name)
			continue
		}
		if !sa.Optional {
			t.Errorf("expected attribute %q to be Optional", name)
		}
	}
}

func TestImportedPipelineResourceSchema_ComputedAttributes(t *testing.T) {
	ctx := context.Background()
	s := resource_imported_pipeline.ImportedPipelineResourceSchema(ctx)

	computed := []string{"id", "name", "created_at", "updated_at", "num_tasks", "paused", "data"}
	for _, name := range computed {
		a, ok := s.Attributes[name]
		if !ok {
			t.Errorf("expected attribute %q to exist", name)
			continue
		}
		if !isComputedAttr(a) {
			t.Errorf("expected attribute %q to be Computed", name)
		}
	}
}

func TestImportedPipelineResourceSchema_DataAttribute(t *testing.T) {
	ctx := context.Background()
	s := resource_imported_pipeline.ImportedPipelineResourceSchema(ctx)

	a, ok := s.Attributes["data"]
	if !ok {
		t.Fatal("expected data attribute to exist")
	}
	nested, ok := a.(rschema.SingleNestedAttribute)
	if !ok {
		t.Fatal("data should be a SingleNestedAttribute")
	}
	if !nested.Computed {
		t.Error("data attribute should be Computed")
	}
}

// DataType tests for imported_pipeline package

func TestImportedPipelineDataType_String(t *testing.T) {
	dt := resource_imported_pipeline.DataType{}
	if dt.String() != "DataType" {
		t.Errorf("expected DataType, got %s", dt.String())
	}
}

func TestImportedPipelineDataType_Equal_Same(t *testing.T) {
	dt := resource_imported_pipeline.DataType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
	other := resource_imported_pipeline.DataType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
	if !dt.Equal(other) {
		t.Error("expected equal DataTypes to be equal")
	}
}

func TestImportedPipelineDataType_Equal_WrongType(t *testing.T) {
	dt := resource_imported_pipeline.DataType{}
	if dt.Equal(types.StringType) {
		t.Error("DataType should not equal StringType")
	}
}

func TestImportedPipelineDataType_ValueFromTerraform_NilType(t *testing.T) {
	ctx := context.Background()
	dt := resource_imported_pipeline.DataType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
	val, err := dt.ValueFromTerraform(ctx, tftypes.Value{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val.IsNull() {
		t.Error("expected null value for nil-type input")
	}
}

func TestImportedPipelineDataType_ValueFromTerraform_Unknown(t *testing.T) {
	ctx := context.Background()
	dt := resource_imported_pipeline.DataType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}
	val, err := dt.ValueFromTerraform(ctx, tftypes.NewValue(objType, tftypes.UnknownValue))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val.IsUnknown() {
		t.Error("expected unknown value")
	}
}

func TestImportedPipelineDataType_ValueFromTerraform_Null(t *testing.T) {
	ctx := context.Background()
	dt := resource_imported_pipeline.DataType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}
	val, err := dt.ValueFromTerraform(ctx, tftypes.NewValue(objType, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val.IsNull() {
		t.Error("expected null value")
	}
}

// DataValue tests for imported_pipeline package

func TestImportedPipelineNewDataValueNull(t *testing.T) {
	v := resource_imported_pipeline.NewDataValueNull()
	if !v.IsNull() {
		t.Error("expected IsNull() == true")
	}
}

func TestImportedPipelineNewDataValueUnknown(t *testing.T) {
	v := resource_imported_pipeline.NewDataValueUnknown()
	if !v.IsUnknown() {
		t.Error("expected IsUnknown() == true")
	}
}

func TestImportedPipelineNewDataValue_Empty(t *testing.T) {
	v, diags := resource_imported_pipeline.NewDataValue(map[string]attr.Type{}, map[string]attr.Value{})
	if diags.HasError() {
		t.Fatalf("unexpected error: %v", diags)
	}
	if v.IsNull() || v.IsUnknown() {
		t.Error("expected known DataValue")
	}
}

func TestImportedPipelineNewDataValue_ExtraAttribute(t *testing.T) {
	_, diags := resource_imported_pipeline.NewDataValue(
		map[string]attr.Type{},
		map[string]attr.Value{"extra": types.StringValue("x")},
	)
	if !diags.HasError() {
		t.Error("expected error for extra attribute")
	}
}

func TestImportedPipelineNewDataValue_MissingAttribute(t *testing.T) {
	_, diags := resource_imported_pipeline.NewDataValue(
		map[string]attr.Type{"required": types.StringType},
		map[string]attr.Value{},
	)
	if !diags.HasError() {
		t.Error("expected error for missing attribute")
	}
}

func TestImportedPipelineDataValue_ToObjectValue_States(t *testing.T) {
	ctx := context.Background()

	nullVal := resource_imported_pipeline.NewDataValueNull()
	obj, diags := nullVal.ToObjectValue(ctx)
	if diags.HasError() {
		t.Fatalf("unexpected error: %v", diags)
	}
	if !obj.IsNull() {
		t.Error("expected null ObjectValue from null DataValue")
	}

	unknownVal := resource_imported_pipeline.NewDataValueUnknown()
	obj, diags = unknownVal.ToObjectValue(ctx)
	if diags.HasError() {
		t.Fatalf("unexpected error: %v", diags)
	}
	if !obj.IsUnknown() {
		t.Error("expected unknown ObjectValue from unknown DataValue")
	}
}

func TestImportedPipelineDataValue_Equal(t *testing.T) {
	null1 := resource_imported_pipeline.NewDataValueNull()
	null2 := resource_imported_pipeline.NewDataValueNull()
	if !null1.Equal(null2) {
		t.Error("two null DataValues should be equal")
	}

	unknown := resource_imported_pipeline.NewDataValueUnknown()
	if null1.Equal(unknown) {
		t.Error("null and unknown DataValues should not be equal")
	}
}

func isComputedAttr(a rschema.Attribute) bool {
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
