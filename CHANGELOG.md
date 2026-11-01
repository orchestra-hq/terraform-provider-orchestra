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
- An `orchestra_monitor` declared without an `action` block aborted the apply: the API returns a default action
  for every monitor, so the plan's null never matched.
- `orchestra_pipeline` accepted an import of a git-backed pipeline, after which every apply wrote to a definition
  Orchestra's git sync owns. Importing or refreshing one now fails and points at the `orchestra_pipeline` data
  source.

### Added

- `orchestra_environment` and `orchestra_monitor` resources, and `orchestra_pipeline`, `orchestra_environment`,
  `orchestra_environments` and `orchestra_integration_connections` data sources.
- Plan-time validation of two limits the API now enforces: `configuration.timeout` must be 0 to 259200 seconds
  (72 hours) and a monitor's `name` at most 100 characters. Previously either surfaced as a 422 in the middle of
  an apply.
- `orchestra_monitor.group_by_labels` groups incidents on a value in the event body — one incident per customer,
  say — alongside the built-in dimensions in `group_by`. The API carries both in one list; Terraform has no union
  type, so they are separate attributes merged on the wire. A monitor using label grouping previously could not
  be read at all.
- `orchestra_monitor` manages incident grouping and alerting: `match` picks the events a monitor claims,
  `group_by` and `group_by_labels` decide what makes two of them one incident, `trigger` makes it a threshold monitor, and `action`
  says what happens when one opens. `order` is reported but not settable — evaluation order is set across the
  whole workspace by `POST /monitors/reorder`, so a per-resource attribute would have monitors fighting each
  other on every apply. `orchestra_monitor_order` sets it instead.
- `orchestra_monitor_order` puts the monitors it lists first, in that order, with every other monitor following
  in the order it already had. The API requires a reorder to name every monitor, but a configuration that had to
  as well could not delete a monitor: Terraform updates the order before destroying the monitor it no longer
  lists. A monitor moved in front of the listed ones outside Terraform shows up as drift, and one created during
  the apply, by Terraform or anyone else, is picked up rather than failing the reorder.
- `orchestra_monitors` lists the workspace's monitors in evaluation order, for importing or ordering them.
- `orchestra_monitor.action.subscribers` and `match.operation_failures_only`. Left out of the configuration they
  are `[]` and `false`. A monitor update replaces the whole definition, so a value set in the Orchestra UI shows
  as drift and the next apply resets it.
- `orchestra_pipeline.paused` is settable. Pausing has its own endpoint, so changing only `paused` does not save
  a new version of the definition. Left unset, it is left alone, so a pause made in the Orchestra UI survives the
  next apply.
- `orchestra_pipeline.folder` files a pipeline in a folder such as `finance/daily`, or at the root with `""`.
  Moving it has its own endpoint, so changing only `folder` does not save a new version of the definition. Left
  unset, it is left alone, so a pipeline filed in the Orchestra UI is not moved by the next apply.
- `orchestra_pipeline.data.configuration.concurrency.on_limit`: skip a run that would exceed `max_active`, queue
  it, or queue only the latest.
- `orchestra_pipeline.data.inputs.<name>.options`: the fixed list of values a string input may take. A pipeline
  update replaces the whole definition, so before this existed an apply cleared any set in the Orchestra UI.
- `orchestra_incident_settings` manages the workspace's incident settings: whether incidents are raised, which
  failures can raise one, and the alert budget. It is a singleton, so settings left unset keep their current value
  and destroying it only removes it from state.
- `orchestra_integration_connection` manages an integration's credentials, which tasks reference by ID. Secrets
  come back masked, so `parameters` is written but never refreshed. Integrations that sign in through a browser
  (OAuth) are rejected with a pointer to the Orchestra UI.
- `orchestra_agent` and `orchestra_skill` manage AI agents and the skills they use, through the Agents API
  (`/api/ai/v1` beside the Engine API). A skill is markdown held in Terraform, or a file in a connected repository
  that Orchestra keeps in sync. An agent's `secrets` are written but never read back. `orchestra_skills` lists
  skills, including the ones Orchestra publishes, so an agent can use them by ID.
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
