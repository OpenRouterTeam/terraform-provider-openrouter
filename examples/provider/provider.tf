terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.1.24"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}