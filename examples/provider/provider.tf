terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.0.13"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}