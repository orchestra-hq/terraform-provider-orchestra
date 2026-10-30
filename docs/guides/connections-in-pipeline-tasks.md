---
page_title: "Setting connections on pipeline tasks"
subcategory: ""
description: |-
  Choose the integration connection each pipeline task runs with, whether Terraform creates the connection or it was made in the Orchestra UI.
---

# Setting connections on pipeline tasks

A task's `connection` is the ID of the [connection](https://docs.getorchestra.io/docs/core-concepts/tasks/connections)
it runs with, such as `snowflake_production_48213`. A task that sets none uses its integration's default connection.

This guide covers the ways to set it:

| Where the connection comes from | Set `connection` to |
|---|---|
| An `orchestra_integration_connection` in the same configuration | `orchestra_integration_connection.<name>.id` |
| A connection made in the Orchestra UI | its ID, looked up with `orchestra_integration_connections` |
| The environment the pipeline runs in | `"$${{ ENV.<VARIABLE> }}"` |
| The integration's default connection | nothing |

The same choices apply to a pipeline written in YAML, where Terraform can fill in each task's connection by its
integration.

## Connections in the pipeline definition

```terraform
# Set the connection each pipeline task runs with.

variable "snowflake_password" {
  type      = string
  sensitive = true
}

# A connection Terraform creates.
resource "orchestra_integration_connection" "snowflake" {
  integration = "SNOWFLAKE"
  name        = "Snowflake production"

  parameters = jsonencode({
    snowflake_auth_type = "USERNAME_PASSWORD"
    snowflake_user      = "ORCHESTRA_SVC"
    snowflake_password  = var.snowflake_password
    snowflake_account   = "xy12345.eu-west-1"
    snowflake_role      = "TRANSFORMER"
    snowflake_warehouse = "TRANSFORMING"
    snowflake_database  = "ANALYTICS"
    snowflake_schema    = "PUBLIC"
  })
}

# A connection made in the Orchestra UI, looked up by name.
data "orchestra_integration_connections" "fivetran" {
  integration = "FIVETRAN"
}

locals {
  # Fails the plan if no connection, or more than one, has this name.
  fivetran_connection_id = {
    for c in data.orchestra_integration_connections.fivetran.connections :
    c.connection_name => c.connection_id
  }["Fivetran production"]
}

resource "orchestra_pipeline" "daily_sales" {
  alias     = "daily_sales"
  published = true

  data = {
    name = "Daily sales"

    tasks = {
      ingest = {
        integration     = "FIVETRAN"
        integration_job = "FIVETRAN_SYNC_ALL"
        connection      = local.fivetran_connection_id
        parameters      = jsonencode({ connector_id = "warehouse_orders" })
      }
      check_orders = {
        depends_on      = ["ingest"]
        integration     = "SNOWFLAKE"
        integration_job = "SNOWFLAKE_RUN_QUERY"
        connection      = orchestra_integration_connection.snowflake.id
        parameters      = jsonencode({ statement = "SELECT COUNT(*) FROM raw.orders" })
      }
    }
  }
}
```

- **`check_orders`** references a connection that Terraform creates. Terraform creates the connection first, and a
  change that replaces it updates the task too. Until the connection exists, Orchestra's check during the plan cannot
  see it, so the first plan may warn that the task has no connection.
- **`ingest`** uses a connection made in the Orchestra UI. Integrations that sign in through a browser (OAuth) can only
  be connected there. `orchestra_integration_connections` lists the connections for an integration, and the map in
  `locals` picks one by its name. The plan fails if no connection has that name, or if more than one does, so a
  renamed connection cannot leave the task silently using the default.

## Connections from the environment

A task can name an environment variable instead of a connection. Orchestra resolves it when the pipeline runs, from
the [environment](https://docs.getorchestra.io/docs/core-concepts/environments) the run is in, so the same pipeline
runs against staging credentials in staging and production credentials in production.

```terraform
# Pick the connection when the pipeline runs, from the environment it runs in.
# Give every environment a SNOWFLAKE_CONNECTION value naming its own
# connection.
resource "orchestra_environment" "production" {
  name = "production"

  values = {
    SNOWFLAKE_CONNECTION = {
      type  = "integration_credential"
      value = orchestra_integration_connection.snowflake.id
    }
  }
}

resource "orchestra_pipeline" "orders_check" {
  alias     = "orders_check"
  published = true

  data = {
    name = "Orders check"

    tasks = {
      check_orders = {
        integration     = "SNOWFLAKE"
        integration_job = "SNOWFLAKE_RUN_QUERY"
        # $${ is how HCL writes a literal ${, so Orchestra receives
        # ${{ ENV.SNOWFLAKE_CONNECTION }}.
        connection = "$${{ ENV.SNOWFLAKE_CONNECTION }}"
        parameters = jsonencode({ statement = "SELECT COUNT(*) FROM raw.orders" })
      }
    }
  }
}
```

Add the same variable to every environment, each naming its own connection. Terraform reads `${` as the start of an
interpolation, so write Orchestra's `${{ ... }}` expressions as `$${{ ... }}` in HCL.

## Connections for a pipeline written in YAML

When the pipeline definition lives in a YAML file, as in
[Creating a pipeline from a YAML file](pipeline-from-yaml), leave connections out of the YAML and have Terraform set
them while it converts the file:

```yaml
version: v1
name: Orders

pipeline:
  ingest:
    integration: FIVETRAN
    integration_job: FIVETRAN_SYNC_ALL
    parameters:
      connector_id: warehouse_orders

  check_orders:
    depends_on: [ingest]
    integration: SNOWFLAKE
    integration_job: SNOWFLAKE_RUN_QUERY
    parameters:
      statement: SELECT COUNT(*) FROM raw.orders
```

```terraform
# Keep the pipeline in YAML and let Terraform set each task's connection by
# its integration. A task whose integration is not listed keeps the
# connection in the YAML, or the integration's default when it names none.

locals {
  orders = yamldecode(file("${path.module}/orders.yml"))

  connections = {
    FIVETRAN  = local.fivetran_connection_id
    SNOWFLAKE = orchestra_integration_connection.snowflake.id
  }

  orders_task_groups = {
    for id, group in local.orders.pipeline : id => merge(group, {
      tasks = {
        for task_id, task in group.tasks : task_id => merge(task, {
          parameters = jsonencode(task.parameters)
          connection = lookup(local.connections, task.integration, try(task.connection, null))
        })
      }
    }) if can(group.tasks)
  }
  orders_tasks = {
    for id, task in local.orders.pipeline : id => merge(task, {
      parameters = jsonencode(task.parameters)
      connection = lookup(local.connections, task.integration, try(task.connection, null))
    }) if !can(task.tasks)
  }
}

resource "orchestra_pipeline" "orders" {
  alias     = "orders"
  published = true

  data = merge(
    { for key, value in local.orders : key => value if key != "pipeline" },
    {
      task_groups = length(local.orders_task_groups) > 0 ? local.orders_task_groups : null
      tasks       = length(local.orders_tasks) > 0 ? local.orders_tasks : null
    },
  )
}
```

`local.connections` maps an integration to the connection its tasks use, so the YAML holds no workspace-specific IDs
and the same file works in any workspace. To set connections task by task instead, put placeholders in the YAML; see
[Templating pipeline YAML](templating-pipeline-yaml).

## The default connection

Set `is_default = true` on an `orchestra_integration_connection` and leave `connection` out of the tasks. Every task
for that integration that names no connection uses it. This suits a workspace with one connection per integration,
but it is implicit: making another connection the default changes which one those tasks use, without a Terraform plan.

## Changing the connection of a pipeline Terraform does not manage

There is no way to change only the connection of one task in a pipeline whose definition Terraform does not manage.
Orchestra's update endpoint replaces the whole definition, so whatever sends the new connection sends every other
task too, and the `orchestra_pipeline` data source returns a summary of the pipeline rather than its tasks.

To manage such a pipeline from Terraform, import it, and let Terraform write its configuration:

```terraform
import {
  to = orchestra_pipeline.daily_sales
  id = "daily_sales" # the pipeline's alias or ID
}
```

```shell
terraform plan -generate-config-out=daily_sales.tf
```

Review `daily_sales.tf`, change the task's `connection` to reference a connection as above, and run `terraform apply`
to import the pipeline and update it in one step. The `import` block can be removed after that. The generated
`published` is `true` when the pipeline's latest version is its published one. Leave it `true` for the change to take
effect on schedule: with `false`, the apply saves the change as a draft and the published version keeps running.

A pipeline whose definition lives in a git repository is managed by Orchestra's git sync. Change its connections in
the YAML in the repository, using `${{ ENV.<VARIABLE> }}` to vary them by environment.
