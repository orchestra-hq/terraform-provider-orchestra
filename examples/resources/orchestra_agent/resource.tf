# Orchestra's own skills can be attached by ID alongside your own.
data "orchestra_skills" "dbt" {
  search            = "dbt"
  orchestra_managed = true
}

resource "orchestra_agent" "triage" {
  name          = "Pipeline triage"
  description   = "Looks into failed runs and says who owns the fix"
  system_prompt = "You are the data platform team's on-call assistant."

  sample_prompts = ["Why did the nightly load fail?"]

  skill_ids = concat(
    ["11111111-2222-3333-4444-555555555555"], # orchestra_skill.triage.id
    [for s in data.orchestra_skills.dbt.skills : s.id],
  )
  integrations = ["SNOWFLAKE"]

  default_model_id = "claude-opus-5"
  default_effort   = "high"

  # Write-only: sent to Orchestra, never stored in state. Bump the version to
  # send changed values.
  secrets_wo_version = 1
  secrets_wo         = {
    PAGERDUTY_TOKEN = var.pagerduty_token
  }
}

variable "pagerduty_token" {
  type      = string
  sensitive = true
}
