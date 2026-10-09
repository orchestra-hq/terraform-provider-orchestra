# The skills Orchestra publishes, by name.
data "orchestra_skills" "published" {
  orchestra_managed = true
}

output "published_skill_ids" {
  value = { for s in data.orchestra_skills.published.skills : s.name => s.id }
}
