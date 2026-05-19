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
| `internal/datasources/pipelines` | top-level attrs, nested fields | `InputsType`, `InputsValue`, `PipelinesType` |
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

## Regenerating generated code

Provider schemas and `*_gen.go` files are produced by HashiCorp’s OpenAPI codegen toolchain. The pipeline has four stages:

| Stage | Input | Tool | Output |
|---|---|---|---|
| 1 | `openapi.yaml` | `scripts/generate_generator_config.py` | `generator_config.yml` |
| 2 | `generator_config.yml` + `openapi.yaml` | `tfplugingen-openapi` | `provider_code_spec.json` |
| 3 | `provider_code_spec.json` | `tfplugingen-framework` | `*_gen.go` (in a temp dir) |
| 4 | temp dir | `scripts/sync_codegen_internal_layout.py` | `internal/datasources/*`, `internal/resources/*` |

`generator_config.yml` maps OpenAPI paths and methods to Terraform resources and data sources (including `schema.ignores` and attribute aliases). It does not generate Go code by itself — run stages 2–4 after editing it. Mapping rules are defined in HashiCorp’s [OpenAPI → provider code spec design](https://github.com/hashicorp/terraform-plugin-codegen-openapi/blob/main/DESIGN.md#mapping-oas-to-provider-code-specification). Stage 2 output (`provider_code_spec.json`) follows the [Terraform Provider Code Specification](https://github.com/hashicorp/terraform-plugin-codegen-spec) ([field reference](https://developer.hashicorp.com/terraform/plugin/code-generation/specification)).

Handwritten files (`*_data_source.go`, `*_resource.go`) are **not** overwritten by codegen. Regeneration updates `*_gen.go`, `*_gen_test.go`, `provider_code_spec.json`, and provider registration in generated output.

### Prerequisites

- Go (with `$(go env GOPATH)/bin` on your `PATH`)
- Python 3 with PyYAML (for stage 1 only): `pip install pyyaml`
- `openapi.yaml` in the repo root (required by `tfplugingen-openapi` even when you only change `generator_config.yml`)

Install the HashiCorp codegen CLIs once per machine:

```bash
go install github.com/hashicorp/terraform-plugin-codegen-openapi/cmd/tfplugingen-openapi@v0.3.0
go install github.com/hashicorp/terraform-plugin-codegen-framework/cmd/tfplugingen-framework@latest
```

### From an existing `generator_config.yml`

Use this when you have already updated `generator_config.yml` (or pulled a change that only touches that file) and want to refresh generated Go and `provider_code_spec.json`:

```bash
# Stage 2: generator_config.yml → provider_code_spec.json
$(go env GOPATH)/bin/tfplugingen-openapi generate \
  --config generator_config.yml \
  --output provider_code_spec.json \
  openapi.yaml

# Stage 3: provider_code_spec.json → Go (temporary output)
rm -rf .tmp-tfplugingen-framework-go
mkdir -p .tmp-tfplugingen-framework-go
$(go env GOPATH)/bin/tfplugingen-framework generate all \
  --input provider_code_spec.json \
  --output .tmp-tfplugingen-framework-go

# Stage 4: sync into internal/ layout
python3 scripts/sync_codegen_internal_layout.py \
  --src .tmp-tfplugingen-framework-go \
  --repo-root .

# Verify
go test ./...
```

Optional environment variables:

- `OPENAPI_PATH` — path to the OpenAPI spec (default: `openapi.yaml`) when using the full pipeline below
- `GO_CODEGEN_TMP_DIR` — framework output directory (default: `.tmp-tfplugingen-framework-go`)

### Full pipeline from `openapi.yaml`

Use this when the OpenAPI spec changed and you need to regenerate `generator_config.yml` as well:

```bash
python3 scripts/generate_generator_config.py \
  --openapi "${OPENAPI_PATH:-openapi.yaml}" \
  --out generator_config.yml
```

To generate `generator_config.yml` for only specific Terraform resources or data sources, pass optional filters (comma-separated names are allowed):

```bash
# Only the pipeline resource and assets / pipelines data sources
python3 scripts/generate_generator_config.py \
  --openapi openapi.yaml \
  --out generator_config.yml \
  --resources pipeline \
  --data-sources assets,pipelines
```

Omit `--resources` or `--data-sources` to include every entry the script knows about for that section. Unknown names fail fast with a list of available values.

$(go env GOPATH)/bin/tfplugingen-openapi generate \
  --config generator_config.yml \
  --output provider_code_spec.json \
  "${OPENAPI_PATH:-openapi.yaml}"

rm -rf "${GO_CODEGEN_TMP_DIR:-.tmp-tfplugingen-framework-go}"
mkdir -p "${GO_CODEGEN_TMP_DIR:-.tmp-tfplugingen-framework-go}"
$(go env GOPATH)/bin/tfplugingen-framework generate all \
  --input provider_code_spec.json \
  --output "${GO_CODEGEN_TMP_DIR:-.tmp-tfplugingen-framework-go}"

python3 scripts/sync_codegen_internal_layout.py \
  --src "${GO_CODEGEN_TMP_DIR:-.tmp-tfplugingen-framework-go}" \
  --repo-root .

go test ./...
```

### References

| Artifact | HashiCorp reference |
|---|---|
| `generator_config.yml` | [Mapping OAS to provider code specification](https://github.com/hashicorp/terraform-plugin-codegen-openapi/blob/main/DESIGN.md#mapping-oas-to-provider-code-specification) (`terraform-plugin-codegen-openapi`) |
| `provider_code_spec.json` | [Terraform Provider Code Specification](https://github.com/hashicorp/terraform-plugin-codegen-spec) ([schema docs](https://developer.hashicorp.com/terraform/plugin/code-generation/specification)) |

### CI

The workflow [`.github/workflows/generate-generator-config.yml`](.github/workflows/generate-generator-config.yml) runs the full pipeline on `workflow_dispatch` and opens a PR when generated artifacts differ from `main`.

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
