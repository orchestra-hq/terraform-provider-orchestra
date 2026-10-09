# A skill held in Terraform. The frontmatter's name and description tell an
# agent when to use it. Usually: markdown = file("${path.module}/skills/triage.md")
resource "orchestra_skill" "triage" {
  markdown = <<-EOT
    ---
    name: triage-pipeline-failures
    description: Find why a pipeline failed and who owns the fix. Use when asked about a failed run.
    ---

    # Triage a pipeline failure

    1. Find the failed run with `list_pipeline_runs`.
    2. Read the failing task's logs.
    3. Say what broke, why, and which team owns it.
  EOT
}

# A skill synced from a file in a connected repository. Orchestra keeps it up to
# date with the branch; Terraform only registers it.
resource "orchestra_skill" "dbt_conventions" {
  git = {
    repository     = "acme/analytics"
    default_branch = "main"
    path           = "skills/dbt-conventions.md"
  }
}
