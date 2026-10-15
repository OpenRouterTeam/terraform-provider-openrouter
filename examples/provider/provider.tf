terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.87"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}