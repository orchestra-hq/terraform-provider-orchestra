resource "orchestra_environment" "production" {
  name        = "production"
  default_env = true

  # Values are always written as strings; `type` decides what is sent to the
  # API, so type = "int" with value = "8" sends the number 8.
  values = {
    WAREHOUSE = {
      type  = "string"
      value = "COMPUTE_WH"
    }
    BATCH_SIZE = {
      type  = "int"
      value = "500"
    }
    STRICT_MODE = {
      type  = "bool"
      value = "true"
    }
  }
}
