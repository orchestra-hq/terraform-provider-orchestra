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
