---
page_title: "Creating a pipeline from a YAML file"
subcategory: ""
description: |-
  Manage an Orchestra pipeline with Terraform while keeping its definition in Orchestra's pipeline YAML format.
---

# Creating a pipeline from a YAML file

Orchestra pipelines are written in a [YAML format](https://docs.getorchestra.io/docs/core-concepts/pipelines/schema).
This guide keeps a pipeline's definition in that format, in a file next to your Terraform configuration, and has
Terraform create and update the pipeline from it. You can copy the YAML from the Orchestra pipeline editor, or write
it by hand.

To have Orchestra read the YAML from a git repository instead, use
[git-backed pipelines](https://docs.getorchestra.io/docs/git-control-and-ci-cd/). Those are managed by Orchestra's git
sync, not by Terraform.

## The pipeline YAML

```yaml
version: v1
name: Daily sales

schedule:
  - name: nightly
    cron: "0 2 * * ? *"
    timezone: Europe/London

configuration:
  retries: 1

pipeline:
  ingest:
    integration: FIVETRAN
    integration_job: FIVETRAN_SYNC_ALL
    parameters:
      connector_id: warehouse_orders

  transform:
    name: Transform
    depends_on: [ingest]
    tasks:
      dbt_build:
        integration: DBT_CORE
        integration_job: DBT_CORE_EXECUTE
        parameters:
          commands: dbt build --select tag:daily
        configuration:
          retries: 2
          timeout: 3600
```

## Configuration

```terraform
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
```

Run `terraform apply`. From then on, a change to the YAML file shows up in `terraform plan` as a change to the
pipeline's `data`.

The plan also has Orchestra check the definition, and shows what it finds as warnings, such as a task whose
connection does not exist. The example's tasks set no `connection`, so they use the workspace's default Fivetran and
dbt Core connections, and Orchestra warns *No default connection was found* for an integration that has none.

## How the YAML maps to `data`

The YAML and `data` use the same field names, so most of the file passes through unchanged. The `locals` block
handles the two differences:

- **`pipeline` is split in two.** The YAML puts tasks and task groups together under `pipeline`. `data` takes
  top-level tasks in `tasks` and groups in `task_groups`. An entry with a `tasks` key is a group.
- **Free-form fields are JSON.** A task's `parameters` has a different shape for each `integration_job`, so `data`
  takes it as a JSON string, and the `locals` wrap it in `jsonencode`.

Other fields are JSON in `data` for the same reason: `meta`, an input's `default`, `run_inputs` on a schedule, sensor
or webhook, a group's `matrix.inputs`, and a webhook alert's `body`. If your YAML uses any of them, `jsonencode` them
the same way. Terraform reports *Inappropriate value for attribute* naming any you miss.

## Notes

- `file()` reads the YAML as it is, so Orchestra's `${{ ... }}` expressions, such as
  `${{ ENV.DBT_TARGET }}`, reach Orchestra unchanged. To fill in values from Terraform, see
  [Templating pipeline YAML](templating-pipeline-yaml).
- To manage a folder of pipelines, one per file, decode each file in
  `fileset(path.module, "pipelines/*.yml")`, key each expression in `locals` by file name, and set `for_each` on the
  resource.
- `alias`, `published`, `folder` and `paused` are not part of the YAML. Set them on the resource.
- YAML comments are not kept: Orchestra stores the parsed definition, not the file.
