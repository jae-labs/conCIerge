locals {
  members = {
    "luiz1361"        = { role = "admin", full_name = "Luiz" }
    "abubakar-abiona" = { role = "member", full_name = "Abubakar" }
    "jane-doe"        = { role = "member", full_name = "Jane" }
  }

  teams = {
    "Maintainers" = {
      description = "Maintainers"
      privacy     = "closed"
      members     = []
      maintainers = ["luiz1361"]
      org_roles = {
        all_repo_admin = 8136
        ci_cd_admin    = 26237
      }
    }
    "Collaborators" = {
      description = "Collaborators"
      privacy     = "closed"
      members     = ["abubakar-abiona", "jane-doe"]
      maintainers = ["luiz1361"]
    }
  }
}
