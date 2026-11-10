terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.216"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}