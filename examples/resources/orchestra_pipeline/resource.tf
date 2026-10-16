# An Orchestra-backed pipeline: Terraform owns the definition.
resource "orchestra_pipeline" "daily_sales" {
  alias     = "daily_sales"
  published = true

  data = {
    name = "Daily sales"

    # Task IDs and group IDs share one namespace, because the API merges
    # `tasks` and `task_groups` into a single object.
    tasks = {
      ingest = {
        integration     = "FIVETRAN"
        integration_job = "FIVETRAN_SYNC_ALL"

        # `parameters` is free-form in the API — its shape depends on
        # `integration_job` — so Terraform carries it as JSON.
        parameters = jsonencode({
          connector_id = "warehouse_orders"
        })
      }
    }

    task_groups = {
      transform = {
        depends_on = ["ingest"]
        name       = "Transform"

        tasks = {
          dbt_build = {
            integration     = "DBT_CORE"
            integration_job = "DBT_CORE_EXECUTE"
            connection      = "name_ABCD"

            parameters = jsonencode({
              commands = ["dbt build --select tag:daily"]
            })

            configuration = {
              retries     = 2
              retry_delay = 60
              timeout     = 3600
            }
          }
        }
      }
    }

    schedule = [{
      name     = "nightly"
      cron     = "cron(0 2 * * ? *)"
      timezone = "Europe/London"
    }]

    # One run at a time; a run that starts while one is in progress waits.
    configuration = {
      concurrency = {
        max_active = "1"
        on_limit   = "QUEUE"
      }
    }

    alerts = [{
      name     = "notify_on_failure"
      statuses = ["FAILED"]

      destinations = [{
        integration = "SLACK"
        destination = "#data-alerts"
      }]
    }]
  }
}
