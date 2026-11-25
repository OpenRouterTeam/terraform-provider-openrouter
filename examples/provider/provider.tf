terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.300"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}