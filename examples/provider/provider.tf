terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.185"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}