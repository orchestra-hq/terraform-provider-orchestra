# Resolve the connection ID a pipeline task should reference.
data "orchestra_integration_connections" "snowflake" {
  integration = "SNOWFLAKE"
  auth_status = "SUCCEEDED"
}

output "default_snowflake_connection" {
  value = one([
    for c in data.orchestra_integration_connections.snowflake.connections :
    c.connection_id if c.is_default
  ])
}
