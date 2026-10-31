---
page_title: "Creating an agent with skills"
subcategory: ""
description: |-
  Create an Orchestra agent and give it two skills: one written in the Terraform configuration and one synced from a Git repository.
---

# Creating an agent with skills

A skill is a Markdown document that tells an agent how to do one job. Its frontmatter's `name` and `description` tell
the agent when to use it. This guide creates two skills, one from each source Orchestra supports, and an agent that
uses both.

## Before you start

- For the Git-backed skill, a [Git connection](https://docs.getorchestra.io/docs/git-control-and-ci-cd/) that can
  read [orchestra-hq/orchestra-skills](https://github.com/orchestra-hq/orchestra-skills), or your own repository
  holding the skill.

## Configuration

`main.tf`:

```terraform
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
```

`triage.md`, next to it:

```markdown
---
name: triage-pipeline-failures
description: Find why a pipeline failed and who owns the fix. Use when asked about a failed run.
---

# Triage a pipeline failure

1. Find the failed run with `list_pipeline_runs`.
2. Read the failing task's logs.
3. Say what broke, why, and which team owns it.
```

The Git-backed skill is `fix-pipeline-dbt-task` from Orchestra's public skills repository. To use your own, point
`repository` and `path` at it. Then run `terraform apply`.

## How it fits together

- **`orchestra_skill.triage`** holds its document in the configuration, read from `triage.md`. Edit the file and apply
  to update it.
- **`orchestra_skill.fix_dbt_task`** is synced from a file in the repository. Orchestra keeps it up to date with
  the branch, so merging a change to the file updates the skill without an apply. Changing `git` replaces the skill.
- **`orchestra_agent.triage`** lists both in `skill_ids`. Orchestra's own skills can be added alongside them by ID,
  using the `orchestra_skills` data source.

## Notes

- Leaving `skill_ids` unset leaves the agent's skills as they are, so skills attached in the Orchestra UI are kept.
  Once set, Terraform manages the whole list.
