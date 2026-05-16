package provider_test
import (
	"context"
	"testing"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/provider"
)

func TestOrchestraProviderSchema_IsEmpty(t *testing.T) {
	ctx := context.Background()
	s := provider.OrchestraProviderSchema(ctx)
	if len(s.Attributes) != 0 {
		t.Errorf("expected empty provider schema, got %d attributes", len(s.Attributes))
	}
}

func TestOrchestraModel_IsEmpty(t *testing.T) {
	m := provider.OrchestraModel{}
	_ = m // struct exists and can be instantiated
}
