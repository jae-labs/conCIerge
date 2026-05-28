module "cloudflare" {
  source = "../modules/cloudflare"

  account_id     = local.account_id
  zones          = local.zones
  dns_records    = local.dns_records
  kv_namespaces  = local.kv_namespaces
  members        = local.members
  pages_projects = local.pages_projects
}
