data "orchestra_environments" "all" {}

output "default_environment" {
  value = one([for e in data.orchestra_environments.all.environments : e.name if e.default_env])
}
