terraform {
  required_providers {
    openrouter = {
      source  = "OpenRouterTeam/openrouter"
      version = "0.3.228"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}