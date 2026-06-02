locals {
  default_envs = {
    "dev" = { name = "Development", personal_configs = true }
    "prd" = { name = "Production" }
  }

  projects = {
    "concierge" = {
      description  = "A Slack Bot written in GoLang that provisions resources, manages access, and automates workflows across various platforms via Terraform."
      environments = local.default_envs
    }
    "github" = {
      description  = "Organization-wide shared templates."
      environments = local.default_envs
    }
  }
}
