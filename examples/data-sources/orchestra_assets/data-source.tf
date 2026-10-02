# Unhealthy Snowflake assets in one schema. All pages are fetched.
data "orchestra_assets" "unhealthy" {
  integration = "SNOWFLAKE"
  schema_name = "ANALYTICS"
  status      = "UNHEALTHY"

  sort_column    = "last_updated_in_integration"
  sort_direction = "desc"
}

output "unhealthy_asset_names" {
  value = [for a in data.orchestra_assets.unhealthy.results : a.asset_name]
}
