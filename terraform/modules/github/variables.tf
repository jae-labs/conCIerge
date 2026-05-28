variable "org" {
  description = "GitHub organization name"
  type        = string
}

variable "members" {
  description = "map of GitHub usernames to member config"
  type = map(object({
    role      = string
    full_name = string
  }))
  default = {}
}

variable "teams" {
  description = "map of team slugs to team config"
  type = map(object({
    description = string
    privacy     = string
    members     = list(string)
    maintainers = list(string)
    org_roles   = optional(map(number), {})
  }))
  default = {}
}

variable "repos" {
  description = "map of repository names to repository config"
  type = map(object({
    description                 = string
    visibility                  = string
    has_issues                  = bool
    has_wiki                    = optional(bool, true)
    has_projects                = optional(bool, true)
    has_discussions             = optional(bool, false)
    homepage_url                = optional(string)
    allow_auto_merge            = optional(bool, false)
    allow_merge_commit          = optional(bool, true)
    allow_rebase_merge          = optional(bool, true)
    allow_squash_merge          = optional(bool, true)
    allow_update_branch         = optional(bool, false)
    archived                    = optional(bool, false)
    dependabot_security_updates = optional(bool, true)
    delete_branch_on_merge      = optional(bool, false)
    is_template                 = optional(bool, false)
    merge_commit_title          = optional(string, "MERGE_MESSAGE")
    merge_commit_message        = optional(string, "PR_TITLE")
    squash_merge_commit_title   = optional(string, "COMMIT_OR_PR_TITLE")
    squash_merge_commit_message = optional(string, "COMMIT_MESSAGES")
    vulnerability_alerts        = optional(bool, true)
    web_commit_signoff_required = optional(bool, false)
    topics                      = optional(list(string), [])
    default_branch              = string
    team_access                 = map(string)
    security_and_analysis = optional(object({
      advanced_security = optional(object({
        status = string
      }))
      secret_scanning = optional(object({
        status = string
      }))
      secret_scanning_push_protection = optional(object({
        status = string
      }))
    }))
    pages = optional(object({
      build_type = optional(string, "workflow")
      cname      = optional(string)
      source = optional(object({
        branch = string
        path   = optional(string, "/")
      }))
    }))
    branch_protection = optional(object({
      required_reviews                = number
      dismiss_stale_reviews           = bool
      require_linear_history          = bool
      require_conversation_resolution = bool
      force_push_bypassers            = optional(list(string), [])
    }))
    environments = optional(map(object({
      wait_timer          = optional(number, 0)
      prevent_self_review = optional(bool, false)
      can_admins_bypass   = optional(bool, true)
      reviewers = optional(object({
        users = optional(list(number), [])
        teams = optional(list(number), [])
      }))
      deployment_branch_policy = optional(object({
        protected_branches     = bool
        custom_branch_policies = bool
      }))
    })), {})
  }))
  default = {}

  validation {
    condition = alltrue(flatten([
      for repo in values(var.repos) : [
        for status in values(merge(
          try(repo.security_and_analysis.advanced_security, null) != null ? { advanced_security = repo.security_and_analysis.advanced_security.status } : {},
          try(repo.security_and_analysis.secret_scanning, null) != null ? { secret_scanning = repo.security_and_analysis.secret_scanning.status } : {},
          try(repo.security_and_analysis.secret_scanning_push_protection, null) != null ? { secret_scanning_push_protection = repo.security_and_analysis.secret_scanning_push_protection.status } : {}
        )) : contains(["enabled", "disabled"], status)
      ]
    ]))
    error_message = "security_and_analysis statuses must be either \"enabled\" or \"disabled\"."
  }

  validation {
    condition = alltrue([
      for repo in values(var.repos) :
      repo.visibility != "public" || try(repo.security_and_analysis.advanced_security, null) == null
    ])
    error_message = "Public repositories must omit security_and_analysis.advanced_security."
  }
}

variable "org_settings" {
  description = "organization-level settings"
  type = object({
    name                                                         = optional(string)
    billing_email                                                = string
    blog                                                         = optional(string)
    email                                                        = optional(string)
    description                                                  = optional(string)
    location                                                     = optional(string)
    members_can_create_repositories                              = bool
    members_can_create_public_repositories                       = optional(bool, false)
    members_can_create_private_repositories                      = optional(bool, false)
    members_can_create_private_pages                             = optional(bool, false)
    default_repository_permission                                = string
    web_commit_signoff_required                                  = bool
    dependabot_alerts_enabled_for_new_repositories               = optional(bool, true)
    dependabot_security_updates_enabled_for_new_repositories     = optional(bool, true)
    dependency_graph_enabled_for_new_repositories                = optional(bool, true)
    advanced_security_enabled_for_new_repositories               = optional(bool, false)
    secret_scanning_enabled_for_new_repositories                 = optional(bool, false)
    secret_scanning_push_protection_enabled_for_new_repositories = optional(bool, false)
  })
}
