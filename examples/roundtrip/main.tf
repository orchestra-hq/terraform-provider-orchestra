# Every resource the provider has, wired together, for a round-trip check
# against a real workspace:
#
#   terraform apply      # creates everything
#   terraform plan       # must say "No changes."
#   terraform apply      # must change nothing
#   terraform destroy
#
# A diff on the second plan, or an update on the second apply, is a bug: a
# computed value the provider does not settle, or an attribute it reads back
# differently from how it was written.
#
# Use a scratch workspace: orchestra_incident_settings changes workspace-wide
# settings.

variable "environment_id" {
  description = "An existing environment to use instead of creating one, for a workspace at its environment limit."
  type        = string
  default     = null
}

variable "alert_email" {
  description = "Where alerts go. Email needs no Slack workspace connected."
  type        = string
}

locals {
  prefix         = "tf_roundtrip"
  environment_id = coalesce(var.environment_id, one(orchestra_environment.this[*].id))
}

resource "orchestra_environment" "this" {
  count = var.environment_id == null ? 1 : 0
  name  = "${local.prefix}_env"

  values = {
    WAREHOUSE   = { type = "string", value = "COMPUTE_WH" }
    BATCH_SIZE  = { type = "int", value = "500" }
    STRICT_MODE = { type = "bool", value = "true" }
  }
}

# Fake credentials: the connection is created whatever auth_status comes back.
resource "orchestra_integration_connection" "snowflake" {
  integration = "SNOWFLAKE"
  name        = "${local.prefix} Snowflake"

  parameters_wo_version = 1
  parameters_wo = jsonencode({
    snowflake_auth_type = "USERNAME_PASSWORD"
    snowflake_user      = "ROUNDTRIP"
    snowflake_password  = "not-a-real-password"
    snowflake_account   = "xy12345.eu-west-1"
    snowflake_role      = "TRANSFORMER"
    snowflake_warehouse = "COMPUTE_WH"
    snowflake_database  = "ANALYTICS"
    snowflake_schema    = "PUBLIC"
  })
}

resource "orchestra_pipeline" "this" {
  alias     = "${local.prefix}_pipeline"
  published = true
  folder    = "terraform/roundtrip"

  data = {
    name = "${local.prefix} pipeline"
    meta = jsonencode({ owner = "terraform" })

    inputs = {
      run_date = {
        type    = "string"
        default = jsonencode("today")
      }
    }

    tasks = {
      ping = {
        integration     = "SNOWFLAKE"
        integration_job = "SNOWFLAKE_RUN_QUERY"
        connection      = orchestra_integration_connection.snowflake.id
        name            = "Ping"
        tags            = ["roundtrip"]
        parameters = jsonencode({
          statement = "SELECT 1"
        })
      }
    }

    task_groups = {
      warehouse = {
        depends_on = ["ping"]
        name       = "Warehouse"

        tasks = {
          query = {
            integration     = "SNOWFLAKE"
            integration_job = "SNOWFLAKE_RUN_QUERY"
            connection      = orchestra_integration_connection.snowflake.id
            parameters = jsonencode({
              statement = "SELECT 1"
            })

            configuration = {
              retries     = "2"
              retry_delay = 60
              timeout     = 600
            }

            alerts = [{
              name     = "query_failed"
              statuses = ["FAILED"]
              destinations = [{
                integration = "EMAIL"
                destination = var.alert_email
              }]
            }]
          }
        }
      }
    }

    schedule = [{
      name        = "nightly"
      cron        = "0 2 * * ? *"
      timezone    = "Europe/London"
      environment = local.environment_id
    }]

    configuration = {
      timeout = 3600
      concurrency = {
        max_active = "1"
        on_limit   = "QUEUE"
      }
    }

    alerts = [{
      name     = "pipeline_failed"
      statuses = ["FAILED"]
      destinations = [{
        integration = "EMAIL"
        destination = var.alert_email
      }]
    }]

    anomalies = [{
      type = "pipeline_duration_above_baseline"
      destinations = [{
        integration = "EMAIL"
        destination = var.alert_email
      }]
    }]
  }
}

resource "orchestra_monitor" "failures" {
  name        = "${local.prefix} failures"
  description = "Snowflake failures on the round-trip pipeline"

  match = {
    pipelines    = [orchestra_pipeline.this.id]
    integrations = ["SNOWFLAKE"]
  }

  group_by = ["pipeline"]

  action = {
    severity = "HIGH"
    alerts = {
      destinations = [{
        integration = "EMAIL"
        destination = var.alert_email
      }]
    }
  }
}

resource "orchestra_monitor" "threshold" {
  name = "${local.prefix} threshold"

  match = {
    connections             = [orchestra_integration_connection.snowflake.id]
    operation_failures_only = true
  }

  # The API requires a threshold monitor to group by monitor.
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

resource "orchestra_monitor" "labels" {
  name = "${local.prefix} per customer"

  match = {
    integrations = ["SNOWFLAKE"]
  }

  group_by_labels = [{
    label = "customer"
    path  = "$.meta.customer"
  }]
}

resource "orchestra_monitor" "muted" {
  name     = "${local.prefix} muted"
  enabled  = false
  match    = { integrations = ["DBT"] }
  group_by = ["pipeline"]
  action   = { mute = true }
}

resource "orchestra_monitor_order" "this" {
  monitor_ids = [
    orchestra_monitor.muted.id,
    orchestra_monitor.failures.id,
    orchestra_monitor.threshold.id,
    orchestra_monitor.labels.id,
  ]
}

resource "orchestra_incident_settings" "this" {
  incidents_enabled       = true
  auto_resolve_on_success = true
  environment_ids         = [local.environment_id]

  max_alerts_per_incident     = 5
  alert_budget_refill_cadence = "daily"
}

resource "orchestra_skill" "this" {
  markdown = <<-EOT
    ---
    name: ${replace(local.prefix, "_", "-")}-skill
    description: Round-trip test skill. Use when asked about the round-trip pipeline.
    ---

    # Round-trip skill

    1. Find the failed run with `list_pipeline_runs`.
    2. Say what broke.
  EOT
}

data "orchestra_skills" "dbt" {
  search            = "dbt"
  orchestra_managed = true
}

resource "orchestra_agent" "this" {
  name            = "${local.prefix} agent"
  description     = "Round-trip test agent"
  system_prompt   = "You are a test agent."
  response_prompt = "Answer in one sentence."
  sample_prompts  = ["Why did the round-trip pipeline fail?"]

  skill_ids = concat(
    [orchestra_skill.this.id],
    [for s in data.orchestra_skills.dbt.skills : s.id],
  )
  default_effort = "high"

  secrets_wo_version = 1
  secrets_wo = {
    ROUNDTRIP_TOKEN = "not-a-real-token"
  }
}
