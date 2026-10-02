package provider_test

import (
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// nullProviderConfig builds an all-null value matching the provider schema, the
// shape Terraform sends for an empty `provider "orchestra" {}` block.
func nullProviderConfig(schema *tfprotov6.GetProviderSchemaResponse) tftypes.Value {
	objType, ok := schema.Provider.ValueType().(tftypes.Object)
	if !ok {
		return tftypes.NewValue(schema.Provider.ValueType(), nil)
	}

	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, attrType := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	return tftypes.NewValue(objType, values)
}
