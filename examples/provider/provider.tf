terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.133"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}