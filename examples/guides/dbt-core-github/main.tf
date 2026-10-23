# Run a dbt Core project from GitHub every night, with the warehouse
# credentials held in one Snowflake connection.

variable "github_token" {
  description = "A fine-grained GitHub token with read-only Contents and Metadata access to the dbt repository."
  type        = string
  sensitive   = true
}

variable "snowflake_password" {
  description = "Password for the Snowflake user dbt runs as."
  type        = string
  sensitive   = true
}

# The warehouse credentials live here only. The dbt connection links to this
# one rather than repeating them in profiles.yml.
resource "orchestra_integration_connection" "snowflake" {
  integration = "SNOWFLAKE"
  name        = "Snowflake (dbt)"

  parameters = jsonencode({
    snowflake_auth_type = "USERNAME_PASSWORD"
    snowflake_user      = "DBT_SVC"
    snowflake_password  = var.snowflake_password
    snowflake_account   = "xy12345.eu-west-1"
    snowflake_role      = "TRANSFORMER"
    snowflake_warehouse = "TRANSFORMING"
    snowflake_database  = "ANALYTICS"
    snowflake_schema    = "DBT"
  })
}

locals {
  # A linked connection's fields reach the task as environment variables named
  # SNOWFLAKE__<CONNECTION_ID>__<FIELD>, with the connection ID upper-cased and
  # every run of other characters replaced by one underscore.
  snowflake_env = "SNOWFLAKE__${trim(upper(replace(orchestra_integration_connection.snowflake.id, "/[^A-Za-z0-9]+/", "_")), "_")}"
}

resource "orchestra_integration_connection" "dbt" {
  integration = "DBT_CORE"
  name        = "dbt (GitHub)"

  parameters = jsonencode({
    repo_source = "GITHUB"
    repo_path   = "my-org/dbt-project"

    # With a GitHub connection already set up in the Orchestra UI, use
    # auth_method = "GIT_CONNECTION" and drop the token.
    auth_method = "TOKEN"
    token       = var.github_token

    linked_connections = [orchestra_integration_connection.snowflake.id]

    # The top-level key must match `profile` in dbt_project.yml.
    profiles_yaml = <<-YAML
      dbt_project:
        target: prod
        outputs:
          prod:
            type: snowflake
            account: "{{ env_var('${local.snowflake_env}__SNOWFLAKE_ACCOUNT') }}"
            user: "{{ env_var('${local.snowflake_env}__SNOWFLAKE_USER') }}"
            password: "{{ env_var('${local.snowflake_env}__SNOWFLAKE_PASSWORD') }}"
            role: "{{ env_var('${local.snowflake_env}__SNOWFLAKE_ROLE') }}"
            warehouse: "{{ env_var('${local.snowflake_env}__SNOWFLAKE_WAREHOUSE') }}"
            database: "{{ env_var('${local.snowflake_env}__SNOWFLAKE_DATABASE') }}"
            schema: "{{ env_var('${local.snowflake_env}__SNOWFLAKE_SCHEMA') }}"
            threads: 8
    YAML
  })
}

resource "orchestra_pipeline" "dbt_nightly" {
  alias     = "dbt_nightly"
  published = true

  data = {
    name = "dbt nightly build"

    tasks = {
      dbt_build = {
        integration     = "DBT_CORE"
        integration_job = "DBT_CORE_EXECUTE"
        connection      = orchestra_integration_connection.dbt.id

        parameters = jsonencode({
          # Separate several commands with semicolons. Orchestra runs dbt deps itself.
          commands        = "dbt build"
          package_manager = "PIP"
          python_version  = "3.12"
        })
      }
    }

    schedule = [{
      name     = "nightly"
      cron     = "cron(0 2 * * ? *)"
      timezone = "Europe/London"
    }]
  }
}
