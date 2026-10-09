data "orchestra_monitors" "all" {}

# Monitor IDs by name, for `terraform import` or an orchestra_monitor_order.
output "monitor_ids" {
  value = { for m in data.orchestra_monitors.all.monitors : m.name => m.id }
}
