terraform {
  required_providers {
    orchestra = {
      source = "orchestra-hq/orchestra"
    }
  }
}

# The API key can also come from the ORCHESTRA_API_KEY environment variable,
# which is the usual choice in CI.
provider "orchestra" {
  api_key = var.orchestra_api_key
}

variable "orchestra_api_key" {
  description = "Orchestra workspace API key, from Settings -> API Keys."
  type        = string
  sensitive   = true
}
