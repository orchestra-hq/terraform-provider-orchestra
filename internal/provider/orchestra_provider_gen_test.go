package provider_test

import (
	"context"
	"testing"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/provider"
)

func TestOrchestraProviderSchema_IsEmpty(t *testing.T) {
	ctx := context.Background()
	s := provider.OrchestraProviderSchema(ctx)
	if len(s.Attributes) != 1 {
		t.Errorf("expected provider schema with 1 attribute, got %d attributes", len(s.Attributes))
	}
	if _, ok := s.Attributes["base_url"]; !ok {
		t.Errorf("expected provider schema to contain `base_url` attribute")
	}
}

func TestOrchestraModel_IsEmpty(t *testing.T) {
	m := provider.OrchestraModel{}
	_ = m // struct exists and can be instantiated
}
