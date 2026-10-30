# Render one pipeline per region from a single YAML template.

locals {
  regions = {
    eu = {
      cron                = "0 2 * * ? *"
      fivetran_connection = "fivetran_eu_10001"
      connectors          = ["orders_eu", "customers_eu"]
    }
    us = {
      cron                = "0 7 * * ? *"
      fivetran_connection = "fivetran_us_10002"
      connectors          = ["orders_us", "customers_us"]
    }
  }

  definitions = {
    for region, r in local.regions : region => yamldecode(templatefile("${path.module}/sales.yml.tftpl", {
      name                = "Sales (${upper(region)})"
      region              = region
      cron                = r.cron
      fivetran_connection = r.fivetran_connection
      connectors          = r.connectors
    }))
  }

  # As in the "Creating a pipeline from a YAML file" guide: split `pipeline`
  # into tasks and task groups, and pass `parameters` as JSON.
  task_groups = {
    for region, d in local.definitions : region => {
      for id, group in d.pipeline : id => merge(group, {
        tasks = {
          for task_id, task in group.tasks :
          task_id => merge(task, { parameters = jsonencode(task.parameters) })
        }
      }) if can(group.tasks)
    }
  }
  tasks = {
    for region, d in local.definitions : region => {
      for id, task in d.pipeline :
      id => merge(task, { parameters = jsonencode(task.parameters) }) if !can(task.tasks)
    }
  }
}

resource "orchestra_pipeline" "sales" {
  for_each = local.definitions

  alias     = "sales_${each.key}"
  published = true

  data = merge(
    { for key, value in each.value : key => value if key != "pipeline" },
    {
      task_groups = length(local.task_groups[each.key]) > 0 ? local.task_groups[each.key] : null
      tasks       = length(local.tasks[each.key]) > 0 ? local.tasks[each.key] : null
    },
  )
}
