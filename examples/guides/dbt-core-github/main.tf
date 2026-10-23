# Connect a dbt Core project in GitHub to Orchestra.

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

    # Each task using this connection gets these as environment variables, so
    # the password stays out of profiles.yml.
    secrets = {
      DBT_SNOWFLAKE_PASSWORD = var.snowflake_password
    }

    # The top-level key must match `profile` in dbt_project.yml.
    profiles_yaml = <<-YAML
      dbt_project:
        target: prod
        outputs:
          prod:
            type: snowflake
            account: xy12345.eu-west-1
            user: DBT_SVC
            password: "{{ env_var('DBT_SNOWFLAKE_PASSWORD') }}"
            role: TRANSFORMER
            warehouse: TRANSFORMING
            database: ANALYTICS
            schema: DBT
            threads: 8
    YAML
  })
}

# A dbt Core task in a pipeline uses this connection by setting `connection`
# to this ID.
output "dbt_connection_id" {
  value = orchestra_integration_connection.dbt.id
}
