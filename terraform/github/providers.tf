variable "GH_PAT" {
  type        = string
  default     = ""
  sensitive   = true
  description = "GitHub Personal Access Token"
}

provider "github" {
  owner = local.org
  token = var.GH_PAT != "" ? var.GH_PAT : null
}
