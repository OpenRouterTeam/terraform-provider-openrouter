terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.261"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}