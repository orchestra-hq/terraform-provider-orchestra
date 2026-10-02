package rsapply_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// buildValue makes a tftypes.Value of the given type from a plain Go literal,
// filling every attribute the literal omits with null. Writing these values
// out by hand is unmanageable for a schema as nested as orchestra_pipeline's.
//
// Supported literals: map[string]any for objects and maps, []any for lists and
// sets, string, bool, int, float64, and nil for null.
func buildValue(t *testing.T, typ tftypes.Type, literal any) tftypes.Value {
	t.Helper()

	if literal == nil {
		return tftypes.NewValue(typ, nil)
	}

	switch target := typ.(type) {
	case tftypes.Object:
		fields, ok := literal.(map[string]any)
		if !ok {
			t.Fatalf("object %s needs a map[string]any, got %T", typ, literal)
		}
		for name := range fields {
			if _, declared := target.AttributeTypes[name]; !declared {
				t.Fatalf("%q is not an attribute of %s", name, typ)
			}
		}
		values := make(map[string]tftypes.Value, len(target.AttributeTypes))
		for name, attrType := range target.AttributeTypes {
			values[name] = buildValue(t, attrType, fields[name])
		}
		return tftypes.NewValue(target, values)

	case tftypes.Map:
		entries, ok := literal.(map[string]any)
		if !ok {
			t.Fatalf("map %s needs a map[string]any, got %T", typ, literal)
		}
		values := make(map[string]tftypes.Value, len(entries))
		for key, entry := range entries {
			values[key] = buildValue(t, target.ElementType, entry)
		}
		return tftypes.NewValue(target, values)

	case tftypes.List:
		items, ok := literal.([]any)
		if !ok {
			t.Fatalf("list %s needs a []any, got %T", typ, literal)
		}
		values := make([]tftypes.Value, 0, len(items))
		for _, item := range items {
			values = append(values, buildValue(t, target.ElementType, item))
		}
		return tftypes.NewValue(target, values)

	case tftypes.Set:
		items, ok := literal.([]any)
		if !ok {
			t.Fatalf("set %s needs a []any, got %T", typ, literal)
		}
		values := make([]tftypes.Value, 0, len(items))
		for _, item := range items {
			values = append(values, buildValue(t, target.ElementType, item))
		}
		return tftypes.NewValue(target, values)
	}

	switch {
	case typ.Is(tftypes.String):
		return tftypes.NewValue(typ, fmt.Sprint(literal))
	case typ.Is(tftypes.Bool):
		b, ok := literal.(bool)
		if !ok {
			t.Fatalf("bool needs a bool, got %T", literal)
		}
		return tftypes.NewValue(typ, b)
	case typ.Is(tftypes.Number):
		switch n := literal.(type) {
		case int:
			return tftypes.NewValue(typ, big.NewFloat(float64(n)))
		case float64:
			return tftypes.NewValue(typ, big.NewFloat(n))
		default:
			t.Fatalf("number needs an int or float64, got %T", literal)
		}
	}

	t.Fatalf("unsupported type %s", typ)
	return tftypes.Value{}
}
