---
page_title: "Connecting a dbt Core project from GitHub"
subcategory: "Connections"
description: |-
  Connect a dbt Core project in a GitHub repository to Orchestra.
---

# Connecting a dbt Core project from GitHub

This guide creates a dbt Core connection to a project stored in GitHub. Pipelines then run the project through that
connection.

## Before you start

- A dbt project in a GitHub repository, with a `requirements.txt` or `pyproject.toml` at its root listing what to
  install, such as `dbt-snowflake`.
- Access to that repository, either:
  - a [Git connection](https://docs.getorchestra.io/docs/git-control-and-ci-cd/) to GitHub set up in Orchestra, which
    the example uses, or
  - a [GitHub token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)
    with read-only **Contents** and **Metadata** permissions on the repository. Orchestra checks the token when the
    connection is created.
- Credentials for the warehouse dbt runs against. The example uses a Snowflake user.

## Configuration

```terraform
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
```

Replace `my-org/dbt-project` with your repository, the warehouse details in `profiles_yaml` with your own, and
`dbt_project` with the `profile` name in your `dbt_project.yml`. Then run `terraform apply`.

## Credentials in `profiles.yml`

Orchestra stores `profiles.yml` encrypted and masks it when returning the connection, so credentials can go in it;
pass them in as sensitive variables rather than writing them into the configuration. `parameters_wo` is write-only
(Terraform 1.11 or later): Terraform sends it to Orchestra but keeps it out of plan output and the state file. Because
Terraform keeps no copy to compare, bump `parameters_wo_version` whenever `profiles.yml` changes so the new one is
sent. On older Terraform, use `parameters` instead: it is sensitive, so it stays out of plan output, but it is written
to the state.

## Using the connection

A [Run dbt Command](https://docs.getorchestra.io/docs/integrations/dbt_core/dbt_core_execute) task uses the connection
by setting its `connection` to the `dbt_connection_id` output. That page lists the task's parameters, such as
`commands`, `branch`, and `project_dir` for a dbt project that isn't at the root of the repository.

## Notes

- Terraform never reads the parameters back, because Orchestra masks secrets when it returns a connection. A change made
  to the connection in the Orchestra UI is not detected.
