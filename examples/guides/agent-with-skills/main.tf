# An agent with two skills: one written in this configuration, one synced from
# a repository.

# Held in Terraform: changing the file and applying updates the skill.
resource "orchestra_skill" "triage" {
  markdown = file("${path.module}/triage.md")
}

# Synced from Orchestra's public skills repository: Orchestra keeps it up to
# date with the branch, so a merge changes what the agent knows without an apply.
resource "orchestra_skill" "fix_dbt_task" {
  git = {
    repository     = "orchestra-hq/orchestra-skills"
    default_branch = "main"
    path           = "skills/orchestra/skills/fix-pipeline-dbt-task/SKILL.md"
  }
}

resource "orchestra_agent" "triage" {
  name          = "Pipeline triage"
  description   = "Looks into failed runs and says who owns the fix"
  system_prompt = "You are the data platform team's on-call assistant."

  sample_prompts = ["Why did the nightly load fail?"]

  skill_ids = [
    orchestra_skill.triage.id,
    orchestra_skill.fix_dbt_task.id,
  ]
}
