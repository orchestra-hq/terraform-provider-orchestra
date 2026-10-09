# Agents are imported by ID. Secrets are never read back, so set them in
# configuration and apply once after importing.
terraform import orchestra_agent.triage 11111111-2222-3333-4444-555555555555
