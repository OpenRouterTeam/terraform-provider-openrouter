terraform {
  required_providers {
    openrouter = {
      source  = "speakeasy/openrouter"
      version = "0.0.7"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}