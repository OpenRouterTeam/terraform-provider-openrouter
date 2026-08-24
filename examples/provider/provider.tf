terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.2.50"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}