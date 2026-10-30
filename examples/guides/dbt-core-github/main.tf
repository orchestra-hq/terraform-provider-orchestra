# Connect a dbt Core project in GitHub to Orchestra.

variable "snowflake_password" {
  type      = string
  sensitive = true
}

resource "orchestra_integration_connection" "dbt" {
  integration = "DBT_CORE"
  name        = "dbt (GitHub)"

  # Write-only: sent to Orchestra, never stored in state. Bump the version to
  # send changed values.
  parameters_wo_version = 1
  parameters_wo         = jsonencode({
    repo_source = "GITHUB"
    repo_path   = "my-org/dbt-project"

    # Uses the workspace's Git connection to GitHub. To use a GitHub token
    # instead, set auth_method = "TOKEN" and add token.
    auth_method = "GIT_CONNECTION"

    # The top-level key must match `profile` in dbt_project.yml.
    profiles_yaml = <<-YAML
      dbt_project:
        target: prod
        outputs:
          prod:
            type: snowflake
            account: xy12345.eu-west-1
            user: DBT_SVC
            password: "${var.snowflake_password}"
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
