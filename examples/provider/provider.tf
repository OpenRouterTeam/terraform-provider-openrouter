terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.294"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}