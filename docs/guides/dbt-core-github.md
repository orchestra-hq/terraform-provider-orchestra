---
page_title: "Connecting dbt Core in GitHub"
subcategory: ""
description: |-
  Connect a dbt Core project in a GitHub repository to Orchestra.
---

# Connecting dbt Core in GitHub

This guide creates a dbt Core connection to a project stored in GitHub. Pipelines then run the project through that
connection.

## Before you start

- A dbt project in a GitHub repository, with a `requirements.txt` or `pyproject.toml` at its root listing what to
  install, such as `dbt-snowflake`.
- A [fine-grained GitHub token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)
  for that repository with read-only **Contents** and **Metadata** permissions. Orchestra checks the token when the
  connection is created.
- Credentials for the warehouse dbt runs against. The example uses a Snowflake user.

## Configuration

```terraform
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
```

Replace `my-org/dbt-project` with your repository, the warehouse details in `profiles_yaml` with your own, and
`dbt_project` with the `profile` name in your `dbt_project.yml`. Then supply the secrets and apply:

```shell
export TF_VAR_github_token=...
export TF_VAR_snowflake_password=...
terraform apply
```

## Keeping secrets out of `profiles.yml`

Orchestra stores `profiles.yml` encrypted, but the example still keeps the password out of it. Values under `secrets`
are injected into every task that uses the connection as environment variables, and `profiles.yml` reads them with
dbt's `env_var()`. Secret values are redacted in task logs.

## Using the connection

A [Run dbt Command](https://docs.getorchestra.io/docs/integrations/dbt_core/dbt_core_execute) task uses the connection
by setting its `connection` to the `dbt_connection_id` output. That page lists the task's parameters, such as
`commands`, `branch`, and `project_dir` for a dbt project that isn't at the root of the repository.

## Notes

- Orchestra masks secrets when it returns a connection, so Terraform never reads `parameters` back. A change made to a
  connection in the Orchestra UI is not detected.
- If your workspace already has a GitHub connection set up in the Orchestra UI, set `auth_method = "GIT_CONNECTION"`
  and remove `token`.
