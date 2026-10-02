# Every pipeline whose name contains "sales". All pages are fetched.
data "orchestra_pipelines" "sales" {
  name = "sales"
}

output "sales_pipeline_aliases" {
  value = [for p in data.orchestra_pipelines.sales.pipelines : p.alias]
}
