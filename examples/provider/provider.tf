terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.288"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}