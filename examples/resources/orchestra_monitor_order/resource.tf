resource "orchestra_monitor" "muted_sandbox" {
  name     = "Mute sandbox failures"
  match    = { integrations = ["DUCKDB"] }
  group_by = ["pipeline"]
  action   = { mute = true }
}

resource "orchestra_monitor" "warehouse" {
  name     = "Warehouse failures"
  match    = { integrations = ["SNOWFLAKE", "DBT", "DUCKDB"] }
  group_by = ["pipeline"]
}

# The mute rule is evaluated first, so it claims sandbox failures before the
# warehouse monitor can. Monitors not listed here are evaluated after these, in
# the order they already had.
resource "orchestra_monitor_order" "this" {
  monitor_ids = [
    orchestra_monitor.muted_sandbox.id,
    orchestra_monitor.warehouse.id,
  ]
}
