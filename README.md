# Terraform Provider for Orchestra

Manage [Orchestra](https://www.getorchestra.io/) pipelines, environments and assets as code.

Built on the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework) against the
[Orchestra REST API](https://docs.getorchestra.io/api/introduction) (protocol version 6, Terraform 1.0+).

> The Orchestra API is available to enterprise customers. See the
> [API introduction](https://docs.getorchestra.io/api/introduction) for details.

## Usage

```hcl
terraform {
  required_providers {
    orchestra = {
      source = "orchestra-hq/orchestra"
    }
  }
}

provider "orchestra" {
  # Falls back to ORCHESTRA_API_KEY when omitted.
  api_key = var.orchestra_api_key
}

resource "orchestra_pipeline" "daily_sales" {
  alias     = "daily_sales"
  published = true

  data = {
    name = "Daily sales"

    tasks = {
      ingest = {
        integration     = "FIVETRAN"
        integration_job = "FIVETRAN_SYNC_ALL"
        parameters      = jsonencode({ connector_id = "warehouse_orders" })
      }
    }

    schedule = [{
      cron     = "cron(0 2 * * ? *)"
      timezone = "Europe/London"
    }]
  }
}
```

Full reference documentation, including every attribute and worked examples, lives in [`docs/`](docs/) and is
rendered on the Terraform Registry.

## Provider configuration

| Attribute | Environment variable | Description |
|---|---|---|
| `api_key` | `ORCHESTRA_API_KEY` | Workspace API key, from [Settings → API Keys](https://app.getorchestra.io/settings/workspace). Required. |
| `base_url` | `ORCHESTRA_BASE_URL` | API base URL. Defaults to `https://app.getorchestra.io/api/engine/public`. |
| `account_id` | `ORCHESTRA_ACCOUNT_ID` | Sent as `X-Orchestra-Account-Id` to act on a different account. Optional. |

The provider fails at configure time when no key resolves, rather than on the first API call.

## What it manages

### Resources

| Resource | Endpoints |
|---|---|
| `orchestra_pipeline` | `POST/PUT/DELETE /pipelines`, `GET /pipeline`, `GET /pipelines/data`, `POST /pipelines/schema` |
| `orchestra_environment` | `POST/GET/PATCH/DELETE /environments[/{id}]` |

### Data sources

`orchestra_pipeline`, `orchestra_pipelines`, `orchestra_assets`, `orchestra_environment`,
`orchestra_environments`, `orchestra_integration_connections`, `orchestra_azure_federated_credentials`.

Run telemetry — pipeline runs, task runs, operations, incidents, logs and artifacts — is deliberately not exposed.
It changes on every run, so putting it in Terraform state would produce permanent diffs. Export it with
[dlt or the Snowflake app](https://docs.getorchestra.io/docs/guides/export-metadata) instead.

## Modelling notes

Three parts of the API do not map onto Terraform's type system directly.

**Free-form objects.** The API declares several fields as `additionalProperties: true`, because their shape depends
on another field — a task's `parameters` depends on its `integration_job`, a sensor check's on its `sensor_type`.
Those are normalized JSON strings in Terraform; write them with `jsonencode({...})`. The same applies to `meta`,
`run_inputs`, `matrix.inputs`, an input's `default`, and a webhook destination's `body`.

**The pipeline union.** `PipelineModel.pipeline` is a map of `TaskGroupModel | TaskModel`, and Terraform has no
union type. The provider splits it into `data.task_groups` and `data.tasks`, merges them into one object on the
wire, and splits them back on read by the presence of a `tasks` key. IDs must be unique across both maps; the
provider rejects a collision at plan time.

**Integer-or-template values.** `maxLiveAlerts` and `concurrency.maxActive` accept either an integer or a
`${{ }}` template. Both are written as strings (`"3"` or `"${{ ENV.MAX }}"`) and encoded to the right JSON type.

## What the provider deliberately does not manage

**Git-backed pipelines.** A pipeline whose definition lives in a repository is owned by Orchestra's git sync.
Terraform would be a second writer to the same file, so `orchestra_pipeline` manages Orchestra-backed pipelines
only. Import an existing git-backed pipeline through Orchestra, not here.

**Assets and lineage edges.** Assets are discovered from integrations rather than declared, and the API has no
endpoint to read or delete an individual lineage edge — so Terraform could create edges but never detect drift on
them or remove them. Use the API directly for manual asset registration.

**Integration and sensor-check catalogues.** These are static reference data; the provider generates the same
values into `internal/enums` from the spec, so a data source would only restate them over the network.

**Run telemetry.** See above.

## Development

```bash
make build   # compile
make vet     # go vet
make test    # unit tests
make docs    # regenerate docs/ with tfplugindocs
make format  # gofmt
```

`make docs` needs the Terraform CLI on your `PATH`. On macOS it also needs a short `TMPDIR`: the plugin handshake
uses a unix socket, and paths over 104 characters fail with `bind: invalid argument`.

```bash
TMPDIR=/tmp/tfdocs make docs
```

### Tests

| Package | Covers |
|---|---|
| `internal/apicontract` | Every `(method, path)` and query parameter in the provider's source exists in `openapi.yaml`, and the generated enums match the spec. Both directions are checked: call sites are discovered by scanning `internal/`, so calling something unlisted fails the build. |
| `internal/orchestra` | Auth and account headers, 404 detection, retry and backoff, pagination across both list shapes. |
| `internal/provider` | Provider schema validity, resource and data source registration, configure-time errors. |
| `internal/resources/pipeline` | Definition round-trip through the wire format, the task-group union split, and JSON encoding edge cases. |
| `internal/resources/rsapply` | Drives resources through plan and apply against a stub API. Asserts the requests sent, that no unknown survives apply, that every known planned value equals the applied one, and that an in-place update never plans a replacement. |
| `internal/datasources/dsread` | Drives every data source through the provider server against a stub API. |
| `internal/acctest` | Real plan/apply cycles against a live workspace. Skipped without a key. |
| `internal/examplecheck` | Every example parses, names a registered type and uses only attributes that exist; every type has one; and `terraform validate` passes against a locally built provider. Needs the Terraform CLI, which it downloads if absent. No API key. |

The contract tests exist because the provider previously called `PUT /pipelines/{alias}`,
`DELETE /pipelines/{alias}` and `GET /pipelines?alias=…`, none of which exist. The first two failed loudly; the
third silently returned whichever pipeline sorted first.

The planned-vs-applied assertion in `rsapply` is what catches the other recurring class of bug: the API fills in
declared defaults on the way out (`webhook` becomes `{"enabled": false}`, `dependsOn` becomes `[]`, a task's
`name` becomes `""`), so an Optional attribute left null in config comes back populated and Terraform aborts the
apply. Stubs must apply those defaults too, or they hide the problem — see `applyServerDefaults`.

Acceptance tests run real plan/apply cycles and need `TF_ACC=1` and a valid `ORCHESTRA_API_KEY`. They create and
destroy real pipelines, so point them at a non-production workspace:

```bash
TF_ACC=1 ORCHESTRA_API_KEY=... go test ./... -run '^TestAcc' -v -timeout 30m
```

## Code generation

Only one thing is generated: `internal/enums/enums_gen.go`, the validator lists for the spec's enums.

```bash
make openapi   # refresh openapi.yaml from the docs site, then regenerate the enums
make docs      # regenerate docs/ from the schemas and examples/
```

`TestGeneratedEnumsMatchSpec` fails if the committed file drifts from the spec, so a refresh that skips the
regeneration is caught in CI.

Everything else is hand-written. The provider previously ran a four-stage pipeline built on
`tfplugingen-openapi` and `tfplugingen-framework`, but that toolchain renders a union and an
`additionalProperties: true` object as an *empty* object — which is how `orchestra_pipeline.data` shipped as an
attribute that could never hold a value. Once the pipeline, assets and pipelines schemas had to be written by
hand, the generator produced a single two-attribute data source with no descriptions, and the hand-written
wrapper around it was larger than its output. It was retired rather than left as scaffolding that implies the
schemas are safe to regenerate.


## Releasing

Tag a release and [GoReleaser](.goreleaser.yml) builds the per-platform archives, checksums and GPG signature the
[Terraform Registry requires](https://developer.hashicorp.com/terraform/registry/providers/publishing).

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The release workflow needs the `GPG_PRIVATE_KEY` and `PASSPHRASE` repository secrets, and publishes a draft release
for review.

## License

[MPL-2.0](LICENSE)
