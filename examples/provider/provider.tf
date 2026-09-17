terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.2.133"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}