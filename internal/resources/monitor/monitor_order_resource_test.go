package monitor

import (
	"slices"
	"testing"
)

func TestHead(t *testing.T) {
	for _, tc := range []struct {
		name           string
		order, managed []string
		want           []string
	}{
		{"managed monitors first, in order", []string{"a", "b", "c"}, []string{"a", "b"}, []string{"a", "b"}},
		{"reordered among themselves", []string{"b", "a", "c"}, []string{"a", "b"}, []string{"b", "a"}},
		{"another monitor moved in front", []string{"c", "a", "b"}, []string{"a", "b"}, []string{"c", "a", "b"}},
		{"one deleted outside Terraform", []string{"a", "c"}, []string{"a", "b"}, []string{"a"}},
		{"after an import, everything", []string{"a", "b", "c"}, nil, []string{"a", "b", "c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := head(tc.order, tc.managed); !slices.Equal(got, tc.want) {
				t.Errorf("head(%v, %v) = %v, want %v", tc.order, tc.managed, got, tc.want)
			}
		})
	}
}
