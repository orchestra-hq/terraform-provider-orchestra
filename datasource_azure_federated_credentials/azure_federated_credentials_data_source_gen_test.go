package datasource_azure_federated_credentials_test

import (
	"context"
	"testing"

	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/orchestra-hq/terraform-provider-orchestra/datasource_azure_federated_credentials"
)

func TestAzureFederatedCredentialsDataSourceSchema_ComputedAttributes(t *testing.T) {
	ctx := context.Background()
	s := datasource_azure_federated_credentials.AzureFederatedCredentialsDataSourceSchema(ctx)

	tests := []struct {
		name string
	}{
		{"audience"},
		{"subject_id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := s.Attributes[tc.name]
			if !ok {
				t.Fatalf("expected attribute %q to exist", tc.name)
			}
			sa, ok := a.(dschema.StringAttribute)
			if !ok {
				t.Fatalf("expected %q to be StringAttribute, got %T", tc.name, a)
			}
			if !sa.Computed {
				t.Errorf("attribute %q should be Computed", tc.name)
			}
			if sa.Required || sa.Optional {
				t.Errorf("attribute %q should only be Computed", tc.name)
			}
		})
	}
}

func TestAzureFederatedCredentialsDataSourceSchema_AttributeCount(t *testing.T) {
	ctx := context.Background()
	s := datasource_azure_federated_credentials.AzureFederatedCredentialsDataSourceSchema(ctx)

	if got := len(s.Attributes); got != 2 {
		t.Errorf("expected 2 attributes, got %d", got)
	}
}

func TestAzureFederatedCredentialsModel_CanInstantiate(t *testing.T) {
	m := datasource_azure_federated_credentials.AzureFederatedCredentialsModel{}
	_ = m
}
