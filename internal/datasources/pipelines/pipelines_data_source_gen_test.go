package pipelines_test
import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/datasources/pipelines"
)

func TestPipelinesDataSourceSchema_TopLevelAttribute(t *testing.T) {
	ctx := context.Background()
	s := pipelines.PipelinesDataSourceSchema(ctx)

	a, ok := s.Attributes["pipelines"]
	if !ok {
		t.Fatal("expected pipelines attribute at top level")
	}
	setAttr, ok := a.(dschema.SetNestedAttribute)
	if !ok {
		t.Fatal("pipelines should be a SetNestedAttribute")
	}
	if !setAttr.Computed {
		t.Error("pipelines should be Computed")
	}
}

func TestPipelinesDataSourceSchema_NestedStringFields(t *testing.T) {
	ctx := context.Background()
	s := pipelines.PipelinesDataSourceSchema(ctx)
	nested := s.Attributes["pipelines"].(dschema.SetNestedAttribute).NestedObject.Attributes

	fields := []string{
		"alias", "id", "name", "created_at", "updated_at",
		"storage_provider", "default_branch", "repository",
		"schedule", "trigger_events", "yaml_path",
		"latest_run_id", "latest_run_status", "latest_run_created_at",
		"latest_run_completed_at", "latest_run_message", "latest_run_started_at",
	}
	for _, name := range fields {
		a, ok := nested[name]
		if !ok {
			t.Errorf("expected nested string field %q", name)
			continue
		}
		if _, ok := a.(dschema.StringAttribute); !ok {
			t.Errorf("field %q should be StringAttribute", name)
		}
	}
}

func TestPipelinesDataSourceSchema_NestedInt64Fields(t *testing.T) {
	ctx := context.Background()
	nested := pipelines.PipelinesDataSourceSchema(ctx).
		Attributes["pipelines"].(dschema.SetNestedAttribute).NestedObject.Attributes

	for _, name := range []string{"num_tasks", "latest_version_number", "published_version_number"} {
		a, ok := nested[name]
		if !ok {
			t.Errorf("expected nested int64 field %q", name)
			continue
		}
		if _, ok := a.(dschema.Int64Attribute); !ok {
			t.Errorf("field %q should be Int64Attribute", name)
		}
	}
}

func TestPipelinesDataSourceSchema_NestedBoolFields(t *testing.T) {
	ctx := context.Background()
	nested := pipelines.PipelinesDataSourceSchema(ctx).
		Attributes["pipelines"].(dschema.SetNestedAttribute).NestedObject.Attributes

	a, ok := nested["paused"]
	if !ok {
		t.Fatal("expected paused field")
	}
	if _, ok := a.(dschema.BoolAttribute); !ok {
		t.Error("paused should be BoolAttribute")
	}
}

func TestPipelinesDataSourceSchema_InputsIsSingleNested(t *testing.T) {
	ctx := context.Background()
	nested := pipelines.PipelinesDataSourceSchema(ctx).
		Attributes["pipelines"].(dschema.SetNestedAttribute).NestedObject.Attributes

	a, ok := nested["inputs"]
	if !ok {
		t.Fatal("expected inputs field")
	}
	if _, ok := a.(dschema.SingleNestedAttribute); !ok {
		t.Error("inputs should be SingleNestedAttribute")
	}
}

// InputsType tests

func TestInputsType_String(t *testing.T) {
	it := pipelines.InputsType{}
	if it.String() != "InputsType" {
		t.Errorf("expected InputsType, got %s", it.String())
	}
}

func TestInputsType_Equal(t *testing.T) {
	tests := []struct {
		name  string
		other attr.Type
		want  bool
	}{
		{
			name:  "same type",
			other: pipelines.InputsType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}},
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
			it := pipelines.InputsType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
			if got := it.Equal(tc.other); got != tc.want {
				t.Errorf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInputsType_ValueFromTerraform(t *testing.T) {
	ctx := context.Background()
	it := pipelines.InputsType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}

	tests := []struct {
		name        string
		in          tftypes.Value
		wantNull    bool
		wantUnknown bool
	}{
		{
			name:     "nil type returns null",
			in:       tftypes.Value{},
			wantNull: true,
		},
		{
			name:        "unknown value",
			in:          tftypes.NewValue(objType, tftypes.UnknownValue),
			wantUnknown: true,
		},
		{
			name:     "null value",
			in:       tftypes.NewValue(objType, nil),
			wantNull: true,
		},
		{
			name: "known empty object",
			in:   tftypes.NewValue(objType, map[string]tftypes.Value{}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, err := it.ValueFromTerraform(ctx, tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNull && !val.IsNull() {
				t.Error("expected null value")
			}
			if tc.wantUnknown && !val.IsUnknown() {
				t.Error("expected unknown value")
			}
			if !tc.wantNull && !tc.wantUnknown && (val.IsNull() || val.IsUnknown()) {
				t.Error("expected known value")
			}
		})
	}
}

// InputsValue tests

func TestNewInputsValueNull(t *testing.T) {
	v := pipelines.NewInputsValueNull()
	if !v.IsNull() {
		t.Error("expected IsNull() == true")
	}
	if v.IsUnknown() {
		t.Error("expected IsUnknown() == false")
	}
}

func TestNewInputsValueUnknown(t *testing.T) {
	v := pipelines.NewInputsValueUnknown()
	if !v.IsUnknown() {
		t.Error("expected IsUnknown() == true")
	}
	if v.IsNull() {
		t.Error("expected IsNull() == false")
	}
}

func TestNewInputsValue(t *testing.T) {
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := pipelines.NewInputsValue(tc.attrTypes, tc.attrs)
			if tc.wantError && !diags.HasError() {
				t.Error("expected error, got none")
			}
			if !tc.wantError && diags.HasError() {
				t.Errorf("unexpected error: %v", diags)
			}
		})
	}
}

func TestInputsValue_Equal(t *testing.T) {
	tests := []struct {
		name string
		a    pipelines.InputsValue
		b    attr.Value
		want bool
	}{
		{
			name: "null equals null",
			a:    pipelines.NewInputsValueNull(),
			b:    pipelines.NewInputsValueNull(),
			want: true,
		},
		{
			name: "unknown equals unknown",
			a:    pipelines.NewInputsValueUnknown(),
			b:    pipelines.NewInputsValueUnknown(),
			want: true,
		},
		{
			name: "null does not equal unknown",
			a:    pipelines.NewInputsValueNull(),
			b:    pipelines.NewInputsValueUnknown(),
			want: false,
		},
		{
			name: "different type returns false",
			a:    pipelines.NewInputsValueNull(),
			b:    types.StringNull(),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Equal(tc.b); got != tc.want {
				t.Errorf("Equal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInputsValue_ToObjectValue(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		v           pipelines.InputsValue
		wantNull    bool
		wantUnknown bool
	}{
		{
			name:     "null input",
			v:        pipelines.NewInputsValueNull(),
			wantNull: true,
		},
		{
			name:        "unknown input",
			v:           pipelines.NewInputsValueUnknown(),
			wantUnknown: true,
		},
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

// PipelinesType tests

func TestPipelinesType_String(t *testing.T) {
	pt := pipelines.PipelinesType{}
	if pt.String() != "PipelinesType" {
		t.Errorf("expected PipelinesType, got %s", pt.String())
	}
}

func TestPipelinesType_Equal(t *testing.T) {
	pt := pipelines.PipelinesType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}
	same := pipelines.PipelinesType{ObjectType: types.ObjectType{AttrTypes: map[string]attr.Type{}}}

	if !pt.Equal(same) {
		t.Error("equal PipelinesTypes should be equal")
	}
	if pt.Equal(types.StringType) {
		t.Error("PipelinesType should not equal StringType")
	}
}
