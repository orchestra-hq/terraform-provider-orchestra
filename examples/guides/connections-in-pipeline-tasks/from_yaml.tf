# Keep the pipeline in YAML and let Terraform set each task's connection by
# its integration. A task whose integration is not listed keeps the
# connection in the YAML, or the integration's default when it names none.

locals {
  orders = provider::orchestra::pipeline_yaml(file("${path.module}/orders.yml"))

  connections = {
    FIVETRAN  = local.fivetran_connection_id
    SNOWFLAKE = orchestra_integration_connection.snowflake.id
  }
}

resource "orchestra_pipeline" "orders" {
  alias     = "orders"
  published = true

  # orders.yml has only top-level tasks. A task in a group is under
  # task_groups.<group>.tasks instead.
  data = merge(local.orders, {
    tasks = {
      for id, task in local.orders.tasks : id => merge(task, {
        connection = lookup(local.connections, task.integration, try(task.connection, null))
      })
    }
  })
}
