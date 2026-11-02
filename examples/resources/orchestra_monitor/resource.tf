# Group every Snowflake and dbt failure on the warehouse pipelines into one
# incident per pipeline, and page on-call.
resource "orchestra_monitor" "warehouse_failures" {
  name        = "Warehouse failures"
  description = "Snowflake and dbt failures across the warehouse pipelines"

  match = {
    integrations = ["SNOWFLAKE", "DBT"]
  }

  # One incident per pipeline, rather than one per failing task.
  group_by = ["pipeline"]

  action = {
    severity = "HIGH"

    alerts = {
      destinations = [{
        integration = "SLACK"
        destination = "#data-oncall"
      }]
    }
  }
}

# A threshold monitor: a single blip is noise, five in half an hour is not.
resource "orchestra_monitor" "flaky_ingestion" {
  name = "Flaky ingestion"

  match = {
    integrations = ["FIVETRAN"]

    # Only failing syncs, not a task that could not start.
    operation_failures_only = true
  }

  # A threshold monitor counts events across the whole monitor, so the API
  # requires it to group by monitor.
  group_by = ["monitor"]

  trigger = {
    count  = 5
    within = "PT30M"
  }

  action = {
    severity = "MEDIUM"
    alerts = {
      also_send_to_pipeline_destinations = true
    }
  }
}

# Group on a value in the event body rather than a built-in dimension: one
# incident per customer, whatever pipeline the failure came from.
resource "orchestra_monitor" "per_customer" {
  name = "Per-customer failures"

  match = {
    integrations = ["SNOWFLAKE", "DBT"]
  }

  group_by_labels = [{
    label = "customer"
    path  = "$.meta.customer"
  }]

  action = {
    severity = "HIGH"
  }
}
