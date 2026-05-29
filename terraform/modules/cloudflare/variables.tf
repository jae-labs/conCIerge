variable "account_id" {
  description = "Cloudflare account ID"
  type        = string
}

variable "zones" {
  description = "map of domain names to zone config"
  type = map(object({
    type = optional(string, "full")
  }))
  default = {}
}

variable "dns_records" {
  description = "map of zone names to a map of DNS record configs; outer key = zone, inner key = logical name"
  type = map(map(object({
    type     = string
    name     = string
    content  = string
    ttl      = optional(number, 1) # 1 = automatic (required for proxied records)
    proxied  = optional(bool, false)
    comment  = optional(string)
    priority = optional(number)
  })))
  default = {}
}

variable "members" {
  description = "map of email addresses to account member config with role IDs"
  type = map(object({
    roles = list(string)
  }))
  default = {}
}

variable "kv_namespaces" {
  description = "map of Workers KV namespaces to create"
  type = map(object({
    title = string
  }))
  default = {}
}

variable "pages_projects" {
  description = "Map of Cloudflare Pages projects to create"
  type = map(object({
    production_branch = optional(string, "main")
    build_config = optional(object({
      build_command   = optional(string)
      destination_dir = optional(string)
      root_dir        = optional(string)
    }))
    kv_bindings         = optional(map(string), {})
    custom_domains      = optional(list(string), [])
    compatibility_date  = optional(string)
    compatibility_flags = optional(list(string), [])
  }))
  default = {}
}
