locals {
  projects = {
    "concierge" = {
      description = "A Slack Bot written in GoLang that provisions resources, manages access, and automates workflows across various platforms via Terraform."
      environments = {
        "dev" = { name = "Development", personal_configs = true }
        "prd" = { name = "Production" }
      }
    }
    "github" = {
      description = "Organization-wide shared templates."
      environments = {
        "dev" = { name = "Development", personal_configs = true }
        "prd" = { name = "Production" }
      }
    }
    "dotfiles" = {
      description = "dotfiles for macOS."
      environments = {
        "dev" = { name = "Development", personal_configs = true }
        "prd" = { name = "Production" }
      }
    }
    "flashcards" = {
      description = "Transform notes into flashcards with local AI. Study offline, stay private, learn smarter—for free!"
      environments = {
        "dev" = { name = "Development", personal_configs = true }
        "prd" = { name = "Production" }
      }
    }
    "pages" = {
      description = "justanother.engineer website."
      environments = {
        "rev" = { name = "Review", personal_configs = true }
        "prd" = { name = "Production" }
      }
    }
    "sandbox" = {
      description = "Exploring ideas, testing concepts, and prototyping solutions."
      environments = {
        "dev" = { name = "Development", personal_configs = true }
        "prd" = { name = "Production" }
      }
    }
    "homebrew-formulae" = {
      description = "A Homebrew tap that provides formulae for installing my projects."
      environments = {
        "dev" = { name = "Development", personal_configs = true }
        "prd" = { name = "Production" }
      }
    }
    "scripts" = {
      description = "Collection of handy utility scripts."
      environments = {
        "dev" = { name = "Development", personal_configs = true }
        "prd" = { name = "Production" }
      }
    }
  }
}
