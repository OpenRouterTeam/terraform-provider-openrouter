terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.52"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}