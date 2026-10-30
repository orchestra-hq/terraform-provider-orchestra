variable "snowflake_password" {
  type      = string
  sensitive = true
}

# Parameter names are the field IDs under "Authentication parameters" on the
# integration's documentation page.
resource "orchestra_integration_connection" "snowflake" {
  integration = "SNOWFLAKE"
  name        = "Snowflake production"
  is_default  = true

  # Write-only: sent to Orchestra, never stored in state. Bump the version to
  # send changed values.
  parameters_wo_version = 1
  parameters_wo         = jsonencode({
    snowflake_auth_type = "USERNAME_PASSWORD"
    snowflake_user      = "ORCHESTRA_SVC"
    snowflake_password  = var.snowflake_password
    snowflake_account   = "xy12345.eu-west-1"
    snowflake_role      = "TRANSFORMER"
    snowflake_warehouse = "TRANSFORMING"
    snowflake_database  = "ANALYTICS"
    snowflake_schema    = "PUBLIC"
  })
}

# Tasks reference it by ID, e.g. connection = orchestra_integration_connection.snowflake.id
