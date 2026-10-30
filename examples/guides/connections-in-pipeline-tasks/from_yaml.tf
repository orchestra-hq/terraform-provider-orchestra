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
