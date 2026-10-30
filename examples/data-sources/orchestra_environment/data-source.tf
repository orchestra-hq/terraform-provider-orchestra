# Read one environment, including its values. The list data source
# (orchestra_environments) deliberately omits values; only this one returns them.
data "orchestra_environment" "production" {
  id = "11111111-2222-3333-4444-555555555555"
}

output "production_values" {
  value = data.orchestra_environment.production.values
}
