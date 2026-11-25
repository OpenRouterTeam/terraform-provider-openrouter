terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.297"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}