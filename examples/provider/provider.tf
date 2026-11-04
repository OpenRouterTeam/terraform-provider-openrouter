terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.148"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}