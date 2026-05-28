locals {
  # flatten nested dns_records map to "zone:key" composite keys
  zone_dns_records = {
    for entry in flatten([
      for zone, records in var.dns_records : [
        for key, r in records : {
          composite_key = "${zone}:${key}"
          zone          = zone
          type          = r.type
          name          = r.name
          content       = r.content
          ttl           = r.ttl
          proxied       = r.proxied
          comment       = r.comment
          priority      = r.priority
        }
      ]
    ]) : entry.composite_key => entry
  }

  # flatten pages custom domains
  pages_domains = {
    for entry in flatten([
      for project, config in var.pages_projects : [
        for domain in config.custom_domains : {
          composite_key = "${project}:${domain}"
          project       = project
          domain        = domain
        }
      ]
    ]) : entry.composite_key => entry
  }
}

resource "cloudflare_zone" "zones" {
  for_each = var.zones

  account = {
    id = var.account_id
  }
  name = each.key
  type = each.value.type

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_dns_record" "records" {
  for_each = local.zone_dns_records

  zone_id  = cloudflare_zone.zones[each.value.zone].id
  type     = each.value.type
  name     = each.value.name
  content  = each.value.content
  ttl      = each.value.ttl
  proxied  = each.value.proxied
  comment  = each.value.comment
  priority = each.value.priority
}

resource "cloudflare_account_member" "members" {
  for_each = var.members

  account_id = var.account_id
  email      = each.key
  roles      = each.value.roles
  status     = "accepted"
}

resource "cloudflare_workers_kv_namespace" "kv_namespaces" {
  for_each = var.kv_namespaces

  account_id = var.account_id
  title      = each.value.title
}

resource "cloudflare_pages_project" "pages_projects" {
  for_each = var.pages_projects

  account_id        = var.account_id
  name              = each.key
  production_branch = each.value.production_branch

  build_config = each.value.build_config != null ? {
    build_command   = each.value.build_config.build_command
    destination_dir = each.value.build_config.destination_dir
    root_dir        = each.value.build_config.root_dir
  } : null

  deployment_configs = (
    length(each.value.kv_bindings) > 0 ||
    each.value.compatibility_date != null ||
    length(each.value.compatibility_flags) > 0
    ) ? {
    preview = {
      compatibility_date  = each.value.compatibility_date
      compatibility_flags = each.value.compatibility_flags
      kv_namespaces = {
        for binding, namespace_key in each.value.kv_bindings :
        binding => {
          namespace_id = cloudflare_workers_kv_namespace.kv_namespaces[namespace_key].id
        }
      }
    }
    production = {
      compatibility_date  = each.value.compatibility_date
      compatibility_flags = each.value.compatibility_flags
      kv_namespaces = {
        for binding, namespace_key in each.value.kv_bindings :
        binding => {
          namespace_id = cloudflare_workers_kv_namespace.kv_namespaces[namespace_key].id
        }
      }
    }
  } : null

  lifecycle {
    ignore_changes = [
      build_config,
      deployment_configs,
    ]
  }
}

resource "cloudflare_pages_domain" "pages_domains" {
  for_each = local.pages_domains

  account_id   = var.account_id
  project_name = cloudflare_pages_project.pages_projects[each.value.project].name
  name         = each.value.domain
}
