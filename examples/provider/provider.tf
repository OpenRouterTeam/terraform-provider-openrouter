terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.2.83"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}