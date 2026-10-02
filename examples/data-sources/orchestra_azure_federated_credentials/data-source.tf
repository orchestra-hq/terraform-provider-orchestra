# The subject and audience to trust when configuring an Azure federated
# identity credential for Orchestra.
data "orchestra_azure_federated_credentials" "this" {}

output "azure_federated_subject" {
  value = data.orchestra_azure_federated_credentials.this.subject_id
}
