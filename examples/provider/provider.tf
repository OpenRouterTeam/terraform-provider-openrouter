terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.2.24"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}