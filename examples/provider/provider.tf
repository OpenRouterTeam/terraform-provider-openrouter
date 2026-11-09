terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.201"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}