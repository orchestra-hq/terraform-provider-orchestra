# Pick the connection when the pipeline runs, from the environment it runs in.
# Give every environment a SNOWFLAKE_CONNECTION value naming its own
# connection.
resource "orchestra_environment" "production" {
  name = "production"

  values = {
    SNOWFLAKE_CONNECTION = {
      type  = "integration_credential"
      value = orchestra_integration_connection.snowflake.id
    }
  }
}

resource "orchestra_pipeline" "orders_check" {
  alias     = "orders_check"
  published = true

  data = {
    name = "Orders check"

    tasks = {
      check_orders = {
        integration     = "SNOWFLAKE"
        integration_job = "SNOWFLAKE_RUN_QUERY"
        # $${ is how HCL writes a literal ${, so Orchestra receives
        # ${{ ENV.SNOWFLAKE_CONNECTION }}.
        connection = "$${{ ENV.SNOWFLAKE_CONNECTION }}"
        parameters = jsonencode({ statement = "SELECT COUNT(*) FROM raw.orders" })
      }
    }
  }
}
