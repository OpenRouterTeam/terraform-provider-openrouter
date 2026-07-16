terraform {
  required_providers {
    openrouter = {
      source  = "speakeasy/openrouter"
      version = "0.0.8"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}