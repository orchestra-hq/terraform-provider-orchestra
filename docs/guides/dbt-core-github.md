---
page_title: "Running dbt Core from GitHub"
subcategory: ""
description: |-
  Connect a dbt Core project in a GitHub repository to Orchestra and run it on a schedule.
---

# Running dbt Core from GitHub

This guide connects a dbt Core project stored in GitHub to Orchestra and runs `dbt build` every night. It creates:

- a Snowflake connection that holds the warehouse credentials,
- a dbt Core connection to the GitHub repository, which links the Snowflake connection so `profiles.yml` holds no
  secrets,
- a pipeline that runs `dbt build` on a schedule.

## Before you start

- A dbt project in a GitHub repository, with a `requirements.txt` or `pyproject.toml` at its root listing what to
  install, such as `dbt-snowflake`.
- A [fine-grained GitHub token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)
  for that repository with read-only **Contents** and **Metadata** permissions. Orchestra checks the token when the
  connection is created.
- A Snowflake user for dbt to run as.

## Configuration

```terraform
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
```

Replace `my-org/dbt-project` with your repository, the Snowflake details with your own, and `dbt_project` in
`profiles_yaml` with the `profile` name in your `dbt_project.yml`. Then supply the secrets and apply:

```shell
export TF_VAR_github_token=...
export TF_VAR_snowflake_password=...
terraform apply
```

## How the credentials reach dbt

The dbt Core connection lists the Snowflake connection under `linked_connections`. When a task runs, Orchestra exports
each field of a linked connection as an environment variable named `<INTEGRATION>__<CONNECTION_ID>__<FIELD>`, such as
`SNOWFLAKE__SNOWFLAKE_PRODUCTION_48213__SNOWFLAKE_PASSWORD`. The connection ID is upper-cased, with each run of
characters other than letters and digits replaced by one underscore. The `snowflake_env` local builds that prefix from
the Snowflake connection's ID, and `profiles.yml` reads each variable with dbt's `env_var()`.

The password lives only in the Snowflake connection, so rotating it is one change. Linked connections are in beta; see
the [dbt Core integration docs](https://docs.getorchestra.io/docs/integrations/dbt_core#linked-connections).

## Notes

- Orchestra masks secrets when it returns a connection, so Terraform never reads `parameters` back. A change made to a
  connection in the Orchestra UI is not detected.
- If your workspace already has a GitHub connection set up in the Orchestra UI, set `auth_method = "GIT_CONNECTION"`
  and remove `token`.
- For a dbt project that isn't at the root of the repository, set `project_dir` in the task's `parameters`. The other
  task options, such as `branch`, are listed under
  [Run dbt Command](https://docs.getorchestra.io/docs/integrations/dbt_core/dbt_core_execute).
