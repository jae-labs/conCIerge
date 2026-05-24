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
    "dotfiles" = {
      description  = "dotfiles for macOS."
      environments = local.default_envs
    }
    "flashcards" = {
      description  = "Transform notes into flashcards with local AI. Study offline, stay private, learn smarter—for free!"
      environments = local.default_envs
    }
    "pages" = {
      description  = "justanother.engineer website."
      environments = local.default_envs
    }
    "sandbox" = {
      description  = "Exploring ideas, testing concepts, and prototyping solutions."
      environments = local.default_envs
    }
    "homebrew-formulae" = {
      description  = "A Homebrew tap that provides formulae for installing my projects."
      environments = local.default_envs
    }
    "scripts" = {
      description  = "Collection of handy utility scripts."
      environments = local.default_envs
    }
  }
}

