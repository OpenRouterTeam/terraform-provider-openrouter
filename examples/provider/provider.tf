terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.16"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}