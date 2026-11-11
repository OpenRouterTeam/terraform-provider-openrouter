terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.240"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}