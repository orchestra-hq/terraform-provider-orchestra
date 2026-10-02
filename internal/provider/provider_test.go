package provider_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/provider"
)

// TestProviderSchema exercises GetProviderSchema, which is what Terraform calls
// first. It runs the framework's own schema validation over every resource and
// data source, so a malformed attribute, a duplicate type name, or an invalid
// nested object fails here rather than at `terraform plan`.
func TestProviderSchema(t *testing.T) {
	ctx := context.Background()
	server := providerserver.NewProtocol6(provider.New("test")())()

	resp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema: %v", err)
	}

	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("schema error: %s: %s", d.Summary, d.Detail)
		}
	}

	wantResources := []string{
		"orchestra_environment",
		"orchestra_pipeline",
	}
	for _, name := range wantResources {
		if _, ok := resp.ResourceSchemas[name]; !ok {
			t.Errorf("resource %s is not registered", name)
		}
	}

	wantDataSources := []string{
		"orchestra_assets",
		"orchestra_azure_federated_credentials",
		"orchestra_environment",
		"orchestra_environments",
		"orchestra_integration_connections",
		"orchestra_pipeline",
		"orchestra_pipelines",
	}
	for _, name := range wantDataSources {
		if _, ok := resp.DataSourceSchemas[name]; !ok {
			t.Errorf("data source %s is not registered", name)
		}
	}
}

// TestProviderConfigureRequiresAPIKey checks the Configure-time error, which
// previously surfaced only when the first API call was made.
func TestProviderConfigureRequiresAPIKey(t *testing.T) {
	t.Setenv("ORCHESTRA_API_KEY", "")

	ctx := context.Background()
	server := providerserver.NewProtocol6(provider.New("test")())()

	schemaResp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema: %v", err)
	}

	config, err := tfprotov6.NewDynamicValue(
		schemaResp.Provider.ValueType(),
		nullProviderConfig(schemaResp),
	)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	resp, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &config})
	if err != nil {
		t.Fatalf("ConfigureProvider: %v", err)
	}

	var found bool
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError && d.Summary == "Missing Orchestra API key" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a missing-API-key error, got %#v", resp.Diagnostics)
	}
}
