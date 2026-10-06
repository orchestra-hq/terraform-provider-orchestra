# Changelog

## Unreleased

First usable release. The provider was previously unpublished; everything below is relative to `main`, and
anyone building from source should expect to rewrite their configuration.

### Fixed

- `orchestra_pipeline` could not create, update or delete a pipeline with a definition. `data` was generated as
  an empty object with no attributes, so nothing could be written to it, and three of the five pipeline calls
  targeted endpoints the API does not have: `PUT /pipelines/{alias}` and `DELETE /pipelines/{alias}` failed
  loudly, while reads via `GET /pipelines?alias=…` silently returned whichever pipeline sorted first, because
  that endpoint ignores the parameter. Reads now use `GET /pipeline`; update and delete pass the selector in the
  body and query string respectively.
- Updating a pipeline destroyed and re-created it. `alias` and `yaml_path` carried `RequiresReplace` without
  `UseStateForUnknown`, so they went unknown whenever anything else changed and compared unequal to the prior
  value, making `Update` unreachable.
- Integers above 2^53 in `parameters`, `meta`, `run_inputs` and `matrix.inputs` were silently rounded — a
  warehouse job ID of `9007199254740993` reached the API as `…992`. These fields now pass through untouched.
- An explicitly empty list (`tags = []`, `depends_on = []`) was written to state as null, aborting the apply.
- Server-side defaults echoed by the API — `webhook`, anomaly thresholds, a task or group `name` — were written
  over planned nulls, aborting the apply for any configuration that omitted them.
- An Orchestra validation error could print the submitted request payload, including environment credentials,
  into Terraform diagnostics, which are never redacted.
- `orchestra_environment` overwrote the planned `default_env` and `values` with the API's response, so an
  environment the server promoted to default, or an integer written as `007`, aborted the apply.
- A paginated read could loop without bound against a server reporting a `total` it never delivered.

### Added

- `orchestra_environment` and `orchestra_monitor` resources, and `orchestra_pipeline`, `orchestra_environment`,
  `orchestra_environments` and `orchestra_integration_connections` data sources.
- `orchestra_monitor.group_by_labels` groups incidents on a value in the event body — one incident per customer,
  say — alongside the built-in dimensions in `group_by`. The API carries both in one list; Terraform has no union
  type, so they are separate attributes merged on the wire. A monitor using label grouping previously could not
  be read at all.
- `orchestra_monitor` manages incident grouping and alerting: `match` picks the events a monitor claims,
  `group_by` and `group_by_labels` decide what makes two of them one incident, `trigger` makes it a threshold monitor, and `action`
  says what happens when one opens. `order` is reported but not settable — evaluation order is set across the
  whole workspace by `POST /monitors/reorder`, so a per-resource attribute would have monitors fighting each
  other on every apply.
- `orchestra_pipeline.data` is now the full pipeline definition — tasks, task groups, schedules, sensors,
  trigger events, alerts, anomalies, inputs and configuration — validated at plan time against
  `POST /pipelines/schema`.
- Provider `api_key` and `account_id` attributes, with `ORCHESTRA_API_KEY` and `ORCHESTRA_ACCOUNT_ID` fallbacks.
  The provider now fails at configure time with a clear message when no key is available.
- Retries with backoff that honour `Retry-After`, and 404 detection that removes a deleted resource from state.
- Acceptance tests against a live workspace, and a contract test that scans the provider's own source so a call
  to an endpoint the spec does not declare fails the build.

### Changed (breaking)

- `orchestra_assets.results[].connection` was briefly renamed `asset_connection`; it is `connection` again.
- `orchestra_pipelines.pipelines` is a list rather than a set, so it can be indexed and ordered.
- `orchestra_pipelines.pipelines[].inputs` is a map of objects rather than an empty object.
- `alias`, `yaml_path` and `storage_provider` on `orchestra_pipeline` now force replacement, because the API has
  no in-place path for them.
- Enum and format validators were added to several attributes, so a configuration that was previously accepted
  and rejected by the API is now rejected at plan time.

### Removed

- Git-backed pipelines. A pipeline whose definition lives in a repository is owned by Orchestra's git sync;
  Terraform would be a second writer to the same file. `storage_provider`, `repository`, `default_branch` and
  `working_branch` are gone, along with `POST /pipelines/import` and `PATCH /pipelines/storage-settings`.
- The `orchestra_asset` and `orchestra_asset_dependency` resources. Assets are discovered from integrations
  rather than declared, and the API has no endpoint to read or delete an individual lineage edge, so Terraform
  could create edges but never detect drift on them or remove them.
- The `orchestra_integrations`, `orchestra_integration_jobs`, `orchestra_sensor_checks` and singular
  `orchestra_asset` data sources. The first three return static catalogue data that `internal/enums` already
  generates from the spec.
- Schema code generation. `tfplugingen-openapi` renders a union and an `additionalProperties: true` object as an
  empty object, which is how `orchestra_pipeline.data` shipped unusable. Only `internal/enums/enums_gen.go` is
  still generated.
