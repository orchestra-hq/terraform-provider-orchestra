# Set the connection each pipeline task runs with.

variable "snowflake_password" {
  type      = string
  sensitive = true
}

# A connection Terraform creates.
resource "orchestra_integration_connection" "snowflake" {
  integration = "SNOWFLAKE"
  name        = "Snowflake production"

  parameters = jsonencode({
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

# A connection made in the Orchestra UI, looked up by name.
data "orchestra_integration_connections" "fivetran" {
  integration = "FIVETRAN"
}

locals {
  # Fails the plan if no connection, or more than one, has this name.
  fivetran_connection_id = {
    for c in data.orchestra_integration_connections.fivetran.connections :
    c.connection_name => c.connection_id
  }["Fivetran production"]
}

resource "orchestra_pipeline" "daily_sales" {
  alias     = "daily_sales"
  published = true

  data = {
    name = "Daily sales"

    tasks = {
      ingest = {
        integration     = "FIVETRAN"
        integration_job = "FIVETRAN_SYNC_ALL"
        connection      = local.fivetran_connection_id
        parameters      = jsonencode({ connector_id = "warehouse_orders" })
      }
      check_orders = {
        depends_on      = ["ingest"]
        integration     = "SNOWFLAKE"
        integration_job = "SNOWFLAKE_RUN_QUERY"
        connection      = orchestra_integration_connection.snowflake.id
        parameters      = jsonencode({ statement = "SELECT COUNT(*) FROM raw.orders" })
      }
    }
  }
}
