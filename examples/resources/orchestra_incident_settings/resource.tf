# One per workspace. Settings left out keep their current value.
resource "orchestra_incident_settings" "this" {
  incidents_enabled       = true
  auto_resolve_on_success = true

  # Only failures in the production environment raise incidents.
  environment_ids = ["11111111-2222-3333-4444-555555555555"]

  # At most five alerts per destination per incident each day.
  max_alerts_per_incident     = 5
  alert_budget_refill_cadence = "daily"
}
