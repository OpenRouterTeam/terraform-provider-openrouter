terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.0.15"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}