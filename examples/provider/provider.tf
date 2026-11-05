terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.180"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}