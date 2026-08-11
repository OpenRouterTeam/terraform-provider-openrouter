terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.2.20"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}