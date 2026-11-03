---
page_title: "Templating pipeline YAML"
subcategory: ""
description: |-
  Fill placeholders in an Orchestra pipeline YAML file from Terraform, and render several pipelines from one template.
---

# Templating pipeline YAML

Terraform's [`templatefile`](https://developer.hashicorp.com/terraform/language/functions/templatefile) function fills
placeholders in a file before it is read. Applied to an Orchestra pipeline YAML file, it lets one file describe
several similar pipelines, or take values such as connection IDs and schedules from Terraform.

This guide renders one pipeline per region from a single template. It builds on
[Creating a pipeline from a YAML file](pipeline-from-yaml), which explains how the YAML maps to `data`.

## The template

```yaml
version: v1
name: ${jsonencode(name)}

schedule:
  - cron: ${jsonencode(cron)}
    timezone: Europe/London

pipeline:
  ingest:
    name: Ingest
    tasks:
%{ for connector in connectors ~}
      sync_${connector}:
        integration: FIVETRAN
        integration_job: FIVETRAN_SYNC_ALL
        connection: ${fivetran_connection}
        parameters:
          connector_id: ${connector}
%{ endfor ~}

  transform:
    name: Transform
    depends_on: [ingest]
    tasks:
      dbt_build:
        integration: DBT_CORE
        integration_job: DBT_CORE_EXECUTE
        parameters:
          commands: dbt build --select tag:${region} --target $${{ ENV.DBT_TARGET }}
```

## Configuration

```terraform
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
    for region, r in local.regions : region => provider::orchestra::pipeline_yaml(templatefile("${path.module}/sales.yml.tftpl", {
      name                = "Sales (${upper(region)})"
      region              = region
      cron                = r.cron
      fivetran_connection = r.fivetran_connection
      connectors          = r.connectors
    }))
  }
}

resource "orchestra_pipeline" "sales" {
  for_each = local.definitions

  alias     = "sales_${each.key}"
  published = true
  data      = each.value
}
```

`templatefile` renders the template once for each region, `yamldecode` parses the result, and `for_each` creates a
pipeline from each, with an alias of `sales_eu` or `sales_us`. Adding a region to `local.regions` adds a pipeline.

Replace the connection IDs and connectors in `local.regions` with your own. The plan has Orchestra check each
rendered pipeline, and warns about a connection that does not exist in the workspace.

## Template syntax

- **`${name}`** inserts a value passed to `templatefile`. Any Terraform expression works, such as `${upper(region)}`.
- **`%{ for ... }` ... `%{ endfor }`** repeats lines, here one task per Fivetran connector. The `~` trims the newline
  after the directive, so the YAML keeps its indentation. Task IDs built from values, such as `sync_${connector}`,
  must come out unique.
- **`${jsonencode(value)}`** inserts a value as JSON, which YAML reads as the same value. Use it for a string that
  could contain characters YAML treats specially, such as `: ` or ` #`, and for lists and objects:
  `depends_on: ${jsonencode(upstream_ids)}`.

## Orchestra expressions

Orchestra's own expressions, such as `${{ ENV.DBT_TARGET }}` and `${{ inputs.run_date }}`, start
with `${` too, so `templatefile` would try to evaluate them. Write them as `$${{ ... }}` in the template:
`templatefile` turns `$${` into `${`, and the expression reaches Orchestra intact, to be resolved when the pipeline
runs. The template above uses both kinds on one line:

```yaml
commands: dbt build --select tag:${region} --target $${{ ENV.DBT_TARGET }}
```

renders, for the `eu` pipeline, as:

```yaml
commands: dbt build --select tag:eu --target ${{ ENV.DBT_TARGET }}
```

Use a Terraform placeholder for what differs between the pipelines Terraform creates, and an Orchestra expression
for what differs between runs of one pipeline, such as the environment it runs in.

## Values known only after apply

If any value passed to `templatefile` is only known after apply, such as the ID of a connection created in the same
apply, the whole rendered file is unknown until then. The plan shows `data = (known after apply)` instead of the
pipeline's tasks, and Orchestra cannot check the definition during the plan.

Pass such values in after decoding instead, as
[Setting connections on pipeline tasks](connections-in-pipeline-tasks) does for connections, so only the attributes
that depend on them are unknown. Values from data sources, variables and locals are known during the plan and are
safe to template.

## Notes

- Without placeholders, read the file with `file()` rather than `templatefile`, and no escaping is needed.
- A rendered file that is not valid YAML fails in `yamldecode`, with the line of the rendered output. Run the
  `templatefile` call in `terraform console` to see the rendered YAML.
- For logic beyond a loop or two, consider building `data` in HCL instead, as in the
  [`orchestra_pipeline` example](../resources/pipeline).
