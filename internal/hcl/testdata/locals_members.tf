locals {
  members = {
    "lula"     = { role = "admin", full_name = "Lula" }
    "jane-doe" = { role = "member", full_name = "Jane" }
  }

  teams = {
    "Maintainers" = {
      description = "Maintainers"
      privacy     = "closed"
      members     = []
      maintainers = ["lula"]
      org_roles = {
        all_repo_admin = 8136
        ci_cd_admin    = 26237
      }
    }
    "Collaborators" = {
      description = "Collaborators"
      privacy     = "closed"
      members     = ["jane-doe"]
      maintainers = ["lula"]
    }
  }
}
