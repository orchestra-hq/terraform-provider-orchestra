# Look a pipeline up by alias or ID.
data "orchestra_pipeline" "daily_sales" {
  alias = "daily_sales"
}

output "daily_sales_last_run" {
  value = data.orchestra_pipeline.daily_sales.latest_run_status
}
