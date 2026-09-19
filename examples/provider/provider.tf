terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.3"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}