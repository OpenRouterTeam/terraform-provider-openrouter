terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.107"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}