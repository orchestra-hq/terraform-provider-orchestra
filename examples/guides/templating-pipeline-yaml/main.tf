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
