package monitor

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestGroupByDecodesBothForms covers the spec's
// anyOf[MonitorGroupByEnum, GroupByLabel]. Before groupBy was a union, the wire
// type was []string and a monitor grouping on a label could not be read at all:
//
//	json: cannot unmarshal object into Go struct field ... of type string
//
// which made such a monitor impossible to import or refresh.
func TestGroupByDecodesBothForms(t *testing.T) {
	const body = `{"groupBy":["pipeline",{"label":"customer","path":"$.meta.customer"},"task"]}`

	var d wireDefinition
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(d.GroupBy) != 3 {
		t.Fatalf("got %d entries, want 3", len(d.GroupBy))
	}

	var m monitorModel
	groupByFromWire(&m, d.GroupBy)

	if len(m.GroupBy) != 2 || m.GroupBy[0].ValueString() != "pipeline" || m.GroupBy[1].ValueString() != "task" {
		t.Errorf("group_by = %v, want [pipeline task]", m.GroupBy)
	}
	if len(m.GroupByLabels) != 1 {
		t.Fatalf("group_by_labels has %d entries, want 1", len(m.GroupByLabels))
	}
	if got := m.GroupByLabels[0].Label.ValueString(); got != "customer" {
		t.Errorf("label = %q, want customer", got)
	}
	if got := m.GroupByLabels[0].Path.ValueString(); got != "$.meta.customer" {
		t.Errorf("path = %q, want $.meta.customer", got)
	}
}

// TestGroupByRoundTrip checks the merge back onto the wire keeps both forms and
// their encoding: a name is a bare string, a label an object.
func TestGroupByRoundTrip(t *testing.T) {
	m := monitorModel{
		GroupBy: []types.String{types.StringValue("pipeline")},
		GroupByLabels: []groupByLabelTF{{
			Label: types.StringValue("customer"),
			Path:  types.StringValue("$.meta.customer"),
		}},
	}

	encoded, err := json.Marshal(groupByToWire(m))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	const want = `["pipeline",{"label":"customer","path":"$.meta.customer"}]`
	if string(encoded) != want {
		t.Errorf("encoded as %s, want %s", encoded, want)
	}

	// And back again, unchanged.
	var entries []wireGroupBy
	if err := json.Unmarshal(encoded, &entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var back monitorModel
	groupByFromWire(&back, entries)
	if len(back.GroupBy) != 1 || len(back.GroupByLabels) != 1 {
		t.Errorf("round trip lost an entry: %v / %v", back.GroupBy, back.GroupByLabels)
	}
}

// TestGroupByRejectsNonsense makes sure a shape the spec does not allow is
// reported rather than silently becoming an empty dimension.
func TestGroupByRejectsNonsense(t *testing.T) {
	var d wireDefinition
	err := json.Unmarshal([]byte(`{"groupBy":[42]}`), &d)
	if err == nil {
		t.Fatal("a numeric groupBy entry was accepted")
	}
}

// TestGroupByUnsetStaysNull keeps an attribute the API sent nothing for out of
// state as null rather than an empty list, which would abort the apply.
func TestGroupByUnsetStaysNull(t *testing.T) {
	var m monitorModel
	groupByFromWire(&m, []wireGroupBy{{Name: "pipeline"}})
	if m.GroupByLabels != nil {
		t.Errorf("group_by_labels = %v, want nil when the API sent no labels", m.GroupByLabels)
	}
}
