// Package tfconv converts between Orchestra API JSON values and Terraform
// framework values. The API marks most response fields nullable, so almost
// everything arrives as a pointer.
package tfconv

import (
	"encoding/json"
	"math/big"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func String(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}

func Bool(v *bool) types.Bool {
	if v == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*v)
}

func Int64(v *int64) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*v)
}

func Number(v *float64) types.Number {
	if v == nil {
		return types.NumberNull()
	}
	return types.NumberValue(big.NewFloat(*v))
}

func Strings(vs []string) []types.String {
	if vs == nil {
		return nil
	}
	out := make([]types.String, 0, len(vs))
	for _, v := range vs {
		out = append(out, types.StringValue(v))
	}
	return out
}

// StringList is for Computed list attributes. A Go slice of types.String
// cannot hold an unknown value, which is what the framework hands a Computed
// attribute at plan time, so those must be modelled as types.List.
func StringList(vs []string) types.List {
	if vs == nil {
		return types.ListNull(types.StringType)
	}
	elems := make([]attr.Value, 0, len(vs))
	for _, v := range vs {
		elems = append(elems, types.StringValue(v))
	}
	return types.ListValueMust(types.StringType, elems)
}

func StringMap(m map[string]string) map[string]types.String {
	if m == nil {
		return nil
	}
	out := make(map[string]types.String, len(m))
	for k, v := range m {
		out[k] = types.StringValue(v)
	}
	return out
}

// JSON renders an arbitrary decoded value as a normalized JSON string, for the
// API's free-form (additionalProperties: true) fields.
func JSON(v any) jsontypes.Normalized {
	if v == nil {
		return jsontypes.NewNormalizedNull()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return jsontypes.NewNormalizedNull()
	}
	// A typed nil (a nil map inside an `any`) is not == nil and marshals to
	// the literal "null". Storing that as a string would read back as drift.
	if string(b) == "null" {
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(b))
}

// GoString returns the Go value of a string, treating null and unknown as "".
func GoString(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

func Int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

// GoStrings drops null and unknown elements. A non-nil empty input stays a
// non-nil empty slice: Terraform distinguishes [] from null, and collapsing the
// two produces "Provider produced inconsistent result after apply".
func GoStrings(vs []types.String) []string {
	if vs == nil {
		return nil
	}
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if v.IsNull() || v.IsUnknown() {
			continue
		}
		out = append(out, v.ValueString())
	}
	return out
}

// GoStringMap preserves an empty map for the same reason as GoStrings.
func GoStringMap(m map[string]types.String) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if v.IsNull() || v.IsUnknown() {
			continue
		}
		out[k] = v.ValueString()
	}
	return out
}
