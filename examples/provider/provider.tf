terraform {
  required_providers {
    openrouter = {
      source  = "openrouter/openrouter"
      version = "0.0.9"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}