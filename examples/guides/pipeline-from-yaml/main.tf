# Create a pipeline from an Orchestra pipeline YAML file.

resource "orchestra_pipeline" "daily_sales" {
  alias     = "daily_sales"
  published = true
  data      = provider::orchestra::pipeline_yaml(file("${path.module}/daily_sales.yml"))
}
