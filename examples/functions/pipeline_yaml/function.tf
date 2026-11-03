# Usually the YAML is in its own file: pipeline_yaml(file("${path.module}/daily_sales.yml")).
resource "orchestra_pipeline" "daily_sales" {
  alias     = "daily_sales"
  published = true

  data = provider::orchestra::pipeline_yaml(<<-YAML
    version: v1
    name: Daily sales
    schedule:
      - name: nightly
        cron: "0 2 * * ? *"
        timezone: Europe/London
    pipeline:
      ingest:
        integration: FIVETRAN
        integration_job: FIVETRAN_SYNC_ALL
        parameters:
          connector_id: warehouse_orders
  YAML
  )
}
