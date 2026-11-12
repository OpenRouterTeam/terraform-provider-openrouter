terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.247"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}