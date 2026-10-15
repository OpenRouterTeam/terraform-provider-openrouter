terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.81"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}