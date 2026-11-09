terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.204"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}