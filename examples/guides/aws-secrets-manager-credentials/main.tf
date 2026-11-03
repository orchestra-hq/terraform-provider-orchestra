# Keep a Snowflake connection's credentials in AWS Secrets Manager, where
# Orchestra reads them each time it uses the connection.

variable "orchestra_role_arn" {
  type        = string
  description = "IAM role Orchestra assumes; it needs secretsmanager:GetSecretValue on the secret."
}

variable "snowflake_secret_arn" {
  type        = string
  description = "ARN of the secret holding the Snowflake connection's parameters as JSON."
}

# The connection to Secrets Manager itself. An IAM role means no AWS secret ever
# passes through Terraform.
resource "orchestra_integration_connection" "secrets_manager" {
  integration = "AWS_SECRETS_MANAGER"
  name        = "AWS Secrets Manager"

  parameters = jsonencode({
    aws_auth_type = "IAM_ROLE"
    aws_role_arn  = var.orchestra_role_arn
    aws_region    = "eu-west-1"
  })
}

# Snowflake, with its credentials read from the secret. The parameters say where
# the secret is, not what is in it, so they hold nothing sensitive.
resource "orchestra_integration_connection" "snowflake" {
  integration      = "SNOWFLAKE"
  name             = "Snowflake production"
  credential_store = "AWS_SECRETS_MANAGER"

  parameters = jsonencode({
    connection_id = orchestra_integration_connection.secrets_manager.id
    secret_arn    = var.snowflake_secret_arn
  })
}
