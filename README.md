# terraform-provider-orchestra — schema & type tests

Generated schema definitions and unit tests for the Orchestra Terraform provider.

## Running the tests

No environment variables are required. All tests are unit tests that run locally with no network access.

```bash
go test ./...
```

To run a single package:

```bash
go test ./internal/datasources/pipelines/...
```

To run with verbose output:

```bash
go test -v ./...
```

## What the tests cover

These are **unit tests** in the sense described by the [Terraform plugin testing documentation](https://developer.hashicorp.com/terraform/plugin/testing/unit-testing): isolated, no network connections, no real Terraform binary required.

Each package tests:

| Package | Schema tests | Custom type tests |
|---|---|---|
| `internal/datasources/assets` | filter attrs, nested fields (string/int64/float64/list) | `ResultsType`, `ResultsValue` |
| `internal/datasources/azure_federated_credentials` | computed attrs | — |
| `internal/datasources/operations` | filter attrs, nested fields, dependencies list | `ResultsType`, `ResultsValue` |
| `internal/datasources/pipeline_runs` | filter attrs, nested fields | `ResultsType`, `ResultsValue` |
| `internal/datasources/pipelines` | top-level attrs, nested fields | `InputsType`, `InputsValue`, `PipelinesType` |
| `internal/datasources/task_runs` | filter attrs, nested fields | `ResultsType`, `ResultsValue` |
| `internal/provider` | schema is empty | — |
| `internal/resources/pipeline` | api+git-import attrs (single resource), storage_provider default | `DataType`, `DataValue` |

Custom type tests cover `String()`, `Equal()`, `ValueFromTerraform()` (nil/unknown/null inputs), constructor null/unknown/error paths, `NewXxxValueMust` panic behaviour, `ToObjectValue()`, and `Equal()` across value states.

## Code generation layout

Most datasource/resource packages are split into three layers:

1. Handwritten implementation: `*_data_source.go` (datasources) and `*_resource.go` (resources)
2. Generated schema + custom types: `*_data_source_gen.go` / `*_resource_gen.go`
3. Generated schema/type unit tests: `*_data_source_gen_test.go` / `*_resource_gen_test.go`

Concretely:

* `*_data_source.go` / `*_resource.go`
  * Implements the Terraform plugin framework interface (`datasource.DataSource` / `resource.Resource`).
  * Owns the API-facing logic: `Configure(...)`, `Read(...)` for datasources; `Create/Read/Update/Delete` for resources.
  * Converts Orchestra API responses (JSON structs) into Terraform framework values (`types.String`, `types.Set`, nested custom values, etc.).
  * Defines small helpers/models used only by the handwritten implementation (for example, building query parameters and mapping API fields into the `ResultsValue`/`DataValue` structs).

* `*_data_source_gen.go` / `*_resource_gen.go`
  * Generated from the provider spec (see the `// Code generated ... DO NOT EDIT` header).
  * Provides the full Terraform schema via functions like `AssetsDataSourceSchema(ctx)` / `PipelineResourceSchema(ctx)` (all attribute flags: `Optional`, `Computed`, defaults, validators, nested attributes).
  * Defines custom framework types used by nested/complex attributes (for example, `ResultsType`/`ResultsValue` in datasources, `DataType`/`DataValue` in the `pipeline` resource).
  * Implements the Terraform type conversion plumbing for those custom types, including `ValueFromTerraform(...)`, `ToTerraformValue(...)`, `ToObjectValue(...)`, plus constructors for null/unknown states.

* `*_data_source_gen_test.go` / `*_resource_gen_test.go`
  * Verifies that the generated schema matches expectations (attribute existence, nested field types, `Optional`/`Computed`, and defaults).
  * Verifies custom type behaviour across Terraform value states (null/unknown/known) and conversion helpers, so generator changes don't silently break type semantics.

## Authentication

The provider authenticates with the Orchestra API using a bearer token:

```
Authorization: Bearer <ORCHESTRA_API_KEY>
```

The provider schema has no attributes — the token is read from the `ORCHESTRA_API_KEY` environment variable at runtime. Obtain your key from [Settings → API Key](https://app.getorchestra.io/settings/workspace/api-key) in the Orchestra UI.

```hcl
terraform {
  required_providers {
    orchestra = {
      source = "registry.terraform.io/orchestra-hq/orchestra"
    }
  }
}

provider "orchestra" {}
```

```bash
export ORCHESTRA_API_KEY=your-api-key-here
terraform plan
```

## Acceptance tests

Acceptance tests (which run real Terraform plan/apply operations against the Orchestra API) are not yet implemented in this module. When added, they will:

- Require `TF_ACC=1` and `ORCHESTRA_API_KEY` to be set
- Use a `PreCheck` function to fail fast if credentials are missing:
  ```go
  PreCheck: func() {
      if os.Getenv("ORCHESTRA_API_KEY") == "" {
          t.Fatal("ORCHESTRA_API_KEY must be set for acceptance tests")
      }
  },
  ```
- Follow the `TestAcc<Resource>_<scenario>` naming convention

See the [HashiCorp acceptance testing docs](https://developer.hashicorp.com/terraform/plugin/testing/acceptance-tests) for the full setup.
