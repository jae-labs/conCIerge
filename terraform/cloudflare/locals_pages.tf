locals {
  kv_namespaces = {
    "jae-pages-chat-rate-limit" = {
      title = "jae-pages-chat-rate-limit"
    }
  }

  pages_projects = {
    "jae-pages" = {
      production_branch   = "main"
      compatibility_date  = "2026-05-25"
      compatibility_flags = ["nodejs_compat"]
      build_config = {
        destination_dir = "dist"
      }
      kv_bindings = {
        RATE_LIMIT_KV = "jae-pages-chat-rate-limit"
      }
      custom_domains = [
        "justanother.engineer"
      ]
    }
  }
}
