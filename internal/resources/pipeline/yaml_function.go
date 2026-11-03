package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"gopkg.in/yaml.v3"
)

// YAMLFunction is provider::orchestra::pipeline_yaml: an Orchestra pipeline
// YAML document reshaped into the value orchestra_pipeline.data takes.
//
// The YAML and `data` use the same snake_case keys and differ in two ways. The
// YAML keeps tasks and task groups together under `pipeline`, which has no
// Terraform equivalent; `data` takes them as `tasks` and `task_groups`, as the
// resource already does when it reads a pipeline back. And `data` takes every
// free-form field as a JSON string. Which fields those are is read from the
// `data` schema itself, so a free-form field added there is covered here too.
type YAMLFunction struct{}

var _ function.Function = &YAMLFunction{}

func NewYAMLFunction() function.Function { return &YAMLFunction{} }

func (f *YAMLFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "pipeline_yaml"
}

func (f *YAMLFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary: "Decode an Orchestra pipeline YAML document into orchestra_pipeline data.",
		MarkdownDescription: "Decodes an Orchestra pipeline YAML document into the value `orchestra_pipeline`'s `data` takes. " +
			"Pass it `file(...)` or `templatefile(...)`. Entries under `pipeline` with a `tasks` key become `task_groups` " +
			"and the rest `tasks`, and free-form fields such as `parameters` are JSON-encoded. Requires Terraform 1.8 or later.",
		Parameters: []function.Parameter{
			function.StringParameter{
				Name:                "yaml",
				MarkdownDescription: "The pipeline YAML document.",
			},
		},
		Return: function.DynamicReturn{},
	}
}

func (f *YAMLFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var doc string
	resp.Error = req.Arguments.Get(ctx, &doc)
	if resp.Error != nil {
		return
	}

	var parsed any
	if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
		resp.Error = function.NewArgumentFuncError(0, "Invalid pipeline YAML: "+err.Error())
		return
	}
	def, ok := parsed.(map[string]any)
	if !ok {
		resp.Error = function.NewArgumentFuncError(0, "Invalid pipeline YAML: the document must be a mapping with keys such as name and pipeline.")
		return
	}

	data, err := yamlToData(def)
	if err != nil {
		resp.Error = function.NewArgumentFuncError(0, "Invalid pipeline YAML: "+err.Error())
		return
	}
	tf, err := toTerraform(data)
	if err != nil {
		resp.Error = function.NewArgumentFuncError(0, "Invalid pipeline YAML: "+err.Error())
		return
	}
	value, err := basetypes.DynamicType{}.ValueFromTerraform(ctx, tf)
	if err != nil {
		resp.Error = function.NewFuncError(err.Error())
		return
	}
	resp.Error = resp.Result.Set(ctx, value)
}

// yamlToData splits `pipeline` into tasks and task groups, then encodes the
// free-form fields against the `data` schema.
func yamlToData(def map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(def)+1)
	for k, v := range def {
		if k != "pipeline" {
			out[k] = v
		}
	}
	if raw, present := def["pipeline"]; present && raw != nil {
		entries, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("pipeline: must be a mapping of task and task group IDs")
		}
		tasks, groups := map[string]any{}, map[string]any{}
		for id, entry := range entries {
			if m, ok := entry.(map[string]any); ok && m["tasks"] != nil {
				groups[id] = entry
			} else {
				tasks[id] = entry
			}
		}
		// The API returns no map rather than an empty one, so an empty one
		// here would plan a diff that never settles.
		if len(tasks) > 0 {
			out["tasks"] = tasks
		}
		if len(groups) > 0 {
			out["task_groups"] = groups
		}
	}

	attr, _ := definitionAttribute().(schema.SingleNestedAttribute)
	return encodeObject(out, attr.Attributes, "")
}

// encodeObject JSON-encodes every value whose attribute is free-form JSON and
// descends into nested attributes. An unknown key is an error: Terraform drops
// it silently on assignment to `data`, so a misspelt depends_on would quietly
// lose the dependency.
func encodeObject(obj map[string]any, attrs map[string]schema.Attribute, at string) (map[string]any, error) {
	if params, ok := obj["parameters"].(map[string]any); ok && attrs["parameters"] == nil && attrs["priority"] != nil {
		// An alert destination: `data` takes its parameters flattened into
		// it, as alerts.DestinationTF explains.
		flat := make(map[string]any, len(obj)+len(params))
		for k, v := range obj {
			if k != "parameters" {
				flat[k] = v
			}
		}
		for k, v := range params {
			flat[k] = v
		}
		obj = flat
	}
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		p := at + k
		if at == "" && (k == "tasks" || k == "task_groups") {
			// Named as the YAML names them.
			p = "pipeline"
		}
		a, known := attrs[k]
		if !known {
			return nil, fmt.Errorf("%s: not a field orchestra_pipeline supports", p)
		}
		if v == nil {
			out[k] = v
			continue
		}
		var err error
		switch a := a.(type) {
		case schema.StringAttribute:
			if _, isJSON := a.CustomType.(jsontypes.NormalizedType); isJSON {
				var b []byte
				if b, err = json.Marshal(v); err != nil {
					return nil, fmt.Errorf("%s: %w", p, err)
				}
				v = string(b)
			}
		case schema.SingleNestedAttribute:
			v, err = encodeNested(v, a.Attributes, p)
		case schema.ListNestedAttribute:
			v, err = encodeEach(v, a.NestedObject.Attributes, p, false)
		case schema.SetNestedAttribute:
			v, err = encodeEach(v, a.NestedObject.Attributes, p, false)
		case schema.MapNestedAttribute:
			v, err = encodeEach(v, a.NestedObject.Attributes, p, true)
		}
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

func encodeNested(v any, attrs map[string]schema.Attribute, p string) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be a mapping", p)
	}
	return encodeObject(m, attrs, p+".")
}

func encodeEach(v any, attrs map[string]schema.Attribute, p string, keyed bool) (any, error) {
	if keyed {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: must be a mapping", p)
		}
		out := make(map[string]any, len(m))
		for k, item := range m {
			enc, err := encodeNested(item, attrs, p+"."+k)
			if err != nil {
				return nil, err
			}
			out[k] = enc
		}
		return out, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be a list", p)
	}
	out := make([]any, len(list))
	for i, item := range list {
		enc, err := encodeNested(item, attrs, fmt.Sprintf("%s[%d]", p, i))
		if err != nil {
			return nil, err
		}
		out[i] = enc
	}
	return out, nil
}

// toTerraform types decoded YAML the way yamldecode does: mappings become
// objects and sequences tuples, which Terraform converts on assignment to
// `data`. A null mapping value is dropped, so the attribute is simply unset.
func toTerraform(v any) (tftypes.Value, error) {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		attrTypes := map[string]tftypes.Type{}
		vals := map[string]tftypes.Value{}
		for _, k := range keys {
			if v[k] == nil {
				continue
			}
			tv, err := toTerraform(v[k])
			if err != nil {
				return tftypes.Value{}, fmt.Errorf("%s: %w", k, err)
			}
			attrTypes[k], vals[k] = tv.Type(), tv
		}
		return tftypes.NewValue(tftypes.Object{AttributeTypes: attrTypes}, vals), nil
	case []any:
		elemTypes := make([]tftypes.Type, len(v))
		vals := make([]tftypes.Value, len(v))
		for i, item := range v {
			if item == nil {
				return tftypes.Value{}, fmt.Errorf("[%d]: a list item cannot be null", i)
			}
			tv, err := toTerraform(item)
			if err != nil {
				return tftypes.Value{}, fmt.Errorf("[%d]: %w", i, err)
			}
			elemTypes[i], vals[i] = tv.Type(), tv
		}
		return tftypes.NewValue(tftypes.Tuple{ElementTypes: elemTypes}, vals), nil
	case string:
		return tftypes.NewValue(tftypes.String, v), nil
	case bool:
		return tftypes.NewValue(tftypes.Bool, v), nil
	case int:
		return tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(int64(v))), nil
	case int64:
		return tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(v)), nil
	case uint64:
		return tftypes.NewValue(tftypes.Number, new(big.Float).SetUint64(v)), nil
	case float64:
		return tftypes.NewValue(tftypes.Number, big.NewFloat(v)), nil
	case map[any]any:
		return tftypes.Value{}, fmt.Errorf("mapping keys must be strings")
	default:
		return tftypes.Value{}, fmt.Errorf("unsupported YAML value %v (%T)", v, v)
	}
}
