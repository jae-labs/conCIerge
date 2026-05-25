resource "cloudflare_workers_script" "media_proxy" {
  account_id     = local.account_id
  script_name    = "media-proxy"
  content_file   = "${path.module}/worker.js"
  content_sha256 = filesha256("${path.module}/worker.js")
  main_module    = "worker.js"

  compatibility_date = "2026-05-25"
}

resource "cloudflare_workers_custom_domain" "media_domain" {
  account_id = local.account_id
  zone_id    = module.cloudflare.zone_ids["justanother.engineer"]
  hostname   = "media.justanother.engineer"
  service    = cloudflare_workers_script.media_proxy.script_name
}
