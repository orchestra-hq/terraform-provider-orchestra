# A workspace has one monitor order, imported by this fixed ID. The import
# lists every monitor; trim monitor_ids to the ones Terraform should put first.
terraform import orchestra_monitor_order.this monitor_order
