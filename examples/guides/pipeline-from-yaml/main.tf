# Create a pipeline from an Orchestra pipeline YAML file.

locals {
  definition = yamldecode(file("${path.module}/daily_sales.yml"))

  # The YAML keeps tasks and task groups together under `pipeline`; the
  # provider takes them separately, and tells a group by its `tasks` key.
  # `parameters` is free-form, so the provider takes it as JSON.
  task_groups = {
    for id, group in local.definition.pipeline : id => merge(group, {
      tasks = {
        for task_id, task in group.tasks :
        task_id => merge(task, { parameters = jsonencode(task.parameters) })
      }
    }) if can(group.tasks)
  }
  tasks = {
    for id, task in local.definition.pipeline :
    id => merge(task, { parameters = jsonencode(task.parameters) }) if !can(task.tasks)
  }
}

resource "orchestra_pipeline" "daily_sales" {
  alias     = "daily_sales"
  published = true

  data = merge(
    { for key, value in local.definition : key => value if key != "pipeline" },
    {
      # Orchestra returns no map rather than an empty one, so send none.
      task_groups = length(local.task_groups) > 0 ? local.task_groups : null
      tasks       = length(local.tasks) > 0 ? local.tasks : null
    },
  )
}
