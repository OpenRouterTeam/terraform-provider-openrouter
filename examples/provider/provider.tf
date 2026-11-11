terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.242"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}