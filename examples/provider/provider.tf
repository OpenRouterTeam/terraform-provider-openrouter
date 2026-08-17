terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.2.34"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}