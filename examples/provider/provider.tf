terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.1.29"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}