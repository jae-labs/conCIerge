locals {
  # Explicit list of secrets sync configurations due to licensing limits
  sync_targets = {
    "concierge:dev" = {
      project_key      = "concierge"
      config           = "dev"
      repo_name        = "conCIerge"
      environment_name = "development"
    }
    "concierge:prd" = {
      project_key      = "concierge"
      config           = "prd"
      repo_name        = "conCIerge"
      environment_name = "production"
    }
    "flashcards:dev" = {
      project_key      = "flashcards"
      config           = "dev"
      repo_name        = "flashcards"
      environment_name = "development"
    }
    "flashcards:prd" = {
      project_key      = "flashcards"
      config           = "prd"
      repo_name        = "flashcards"
      environment_name = "production"
    }
  }
}

resource "doppler_secrets_sync_github_actions" "github_sync" {
  for_each = local.sync_targets

  project          = module.doppler.project_names[each.value.project_key]
  config           = each.value.config
  integration      = "0e4c99e3-d0ef-4e3d-ad67-d3fad271c510"
  sync_target      = "repo"
  repo_name        = each.value.repo_name
  environment_name = each.value.environment_name

  depends_on = [module.doppler]
}


