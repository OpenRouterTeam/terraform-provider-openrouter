terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.2.69"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}