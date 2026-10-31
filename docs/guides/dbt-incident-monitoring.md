---
page_title: "Incident monitoring for dbt"
subcategory: ""
description: |-
  Raise incidents for failing dbt models and tests, grouped by lineage, and alert the people who own them.
---

# Incident monitoring for dbt

This guide turns on [incidents](https://docs.getorchestra.io/docs/core-concepts/incidents) for the workspace and sets
up the [monitor Orchestra recommends for dbt](https://docs.getorchestra.io/docs/core-concepts/incidents/monitors#recommended-monitor-for-dbt-users),
plus a second monitor for dbt runs that fail before any model runs.

## Configuration

```terraform
# Raise incidents for dbt failures, one per broken model and what it breaks
# downstream, and alert the analytics engineers.

variable "production_environment_id" {
  type        = string
  description = "Only failures in this environment raise incidents."
}

# One per workspace. Settings left out keep their current value.
resource "orchestra_incident_settings" "this" {
  incidents_enabled       = true
  auto_resolve_on_success = true
  auto_diagnose_incidents = true

  environment_ids = [var.production_environment_id]

  # Runs on a feature branch shouldn't page anyone.
  require_default_branch_or_published_version = true

  max_alerts_per_incident     = 5
  alert_budget_refill_cadence = "daily"
}

# Orchestra's recommended dbt monitor: a failing model and the failures up to
# two levels downstream of it become one incident; unrelated models that fail in
# the same run get their own.
resource "orchestra_monitor" "dbt_models" {
  name        = "dbt model and test failures"
  description = "dbt Core and dbt Cloud failures, grouped by lineage"

  match = {
    integrations = ["DBT_CORE", "DBT"]

    # Only failing models and tests. Failures of the dbt task itself, such as a
    # parse error, fall through to the next monitor.
    operation_failures_only = true
  }

  group_by = ["downstream_assets"]

  action = {
    severity = "HIGH"

    alerts = {
      destinations = [{
        integration = "SLACK"
        destination = "#analytics-engineering"
      }]
    }
  }
}

# Catches the dbt failures the lineage monitor leaves: compilation and parse
# errors, a bad profile, a missing package.
resource "orchestra_monitor" "dbt_tasks" {
  name        = "dbt task failures"
  description = "dbt runs that failed before any model ran"

  match = {
    integrations = ["DBT_CORE", "DBT"]
  }

  group_by = ["task"]

  action = {
    severity = "MEDIUM"
    alerts = {
      also_send_to_pipeline_destinations = true
    }
  }
}

# The first matching monitor claims an event, so the lineage monitor must be
# checked before the catch-all.
resource "orchestra_monitor_order" "this" {
  monitor_ids = [
    orchestra_monitor.dbt_models.id,
    orchestra_monitor.dbt_tasks.id,
  ]
}
```

Set `production_environment_id` to your production environment's ID (the `orchestra_environments` data source lists
them), change the Slack channel, then run `terraform apply`.

## How it fits together

- **`orchestra_incident_settings`** controls whether failures raise incidents at all. Here only production runs on a
  pipeline's default branch or published version can, an incident resolves itself once everything it covers succeeds
  again, and each Slack channel gets at most five alerts per incident a day.
- **`dbt_models`** claims failing dbt models and tests from dbt Core (`DBT_CORE`) and dbt Cloud (`DBT`).
  `operation_failures_only` limits it to failures with a model or test behind them, and grouping by
  `downstream_assets` ([Downstream dbt models](https://docs.getorchestra.io/docs/core-concepts/incidents/monitors#group-by-downstream-dbt-models)
  in the UI) puts a failing model and the failures up to two levels downstream of it in one incident.
- **`dbt_tasks`** claims the rest: compilation errors, a bad profile, a missing package. It groups by task and alerts
  the pipeline's own destinations.
- **`orchestra_monitor_order`** makes the lineage monitor the first to see each event. The first monitor that matches
  an event claims it, so in the other order the catch-all would claim everything.

## Notes

- Downstream grouping uses lineage from previous runs, so a new model or a changed dependency may not group
  accurately until it has run.
- `orchestra_incident_settings` is workspace-wide and there is one per workspace. Declare it in one configuration only.
- Monitors not in `orchestra_monitor_order` are checked after the ones it lists, in the order they already had.
