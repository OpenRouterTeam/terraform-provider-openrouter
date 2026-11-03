terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.139"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}