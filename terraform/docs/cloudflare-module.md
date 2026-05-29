# Cloudflare Module

Manages DNS records and account members for `jae-labs`.

## Resources managed

| Resource | Key | Description |
|---|---|---|
| `cloudflare_zone` | `zones[domain]` | DNS zones (`prevent_destroy` lifecycle) |
| `cloudflare_dns_record` | `records[zone:key]` | DNS records |
| `cloudflare_account_member` | `members[email]` | Account members |
| `cloudflare_workers_kv_namespace` | `kv_namespaces[name]` | Workers KV namespaces |
| `cloudflare_pages_project` | `pages_projects[name]` | Cloudflare Pages projects |
| `cloudflare_pages_domain` | `pages_domains[project:domain]` | Cloudflare Pages custom domains |

## Variables

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `account_id` | `string` | yes | - | Cloudflare account ID |
| `zones` | `map(object({ type? }))` | no | `{}` | Domain names to manage; type defaults to `"full"` |
| `dns_records` | `map(map(object({...})))` | no | `{}` | Nested map: zone -> record key -> record config |
| `members` | `map(object({ roles }))` | no | `{}` | Email to role ID list |
| `kv_namespaces` | `map(object({ title }))` | no | `{}` | Workers KV namespaces to create |
| `pages_projects` | `map(object({...}))` | no | `{}` | Cloudflare Pages projects to manage |

### `dns_records` object fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `type` | `string` | yes | - | Record type (A, CNAME, MX, TXT, etc.) |
| `name` | `string` | yes | - | DNS name |
| `content` | `string` | yes | - | Record value |
| `ttl` | `number` | no | `1` | TTL in seconds; 1 = automatic (required for proxied) |
| `proxied` | `bool` | no | `false` | Cloudflare proxy enabled |
| `comment` | `string` | no | `null` | Record comment |
| `priority` | `number` | no | `null` | Record priority (MX, SRV) |

### `pages_projects` object fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `production_branch` | `string` | no | `"main"` | Branch for production deployments |
| `build_config` | `object` | no | `null` | Pages build configuration block (build_command, destination_dir, root_dir) |
| `kv_bindings` | `map(string)` | no | `{}` | Pages Functions KV binding name to `kv_namespaces` key |
| `custom_domains` | `list(string)` | no | `[]` | List of custom domains to associate with the Pages project |
| `compatibility_date` | `string` | no | `null` | Compatibility date for Pages deployments |
| `compatibility_flags` | `list(string)` | no | `[]` | Compatibility flags for Pages deployments (e.g. `["nodejs_compat"]`) |

## Configuration

All metadata lives in split locals files under `cloudflare/`:

| File | Content |
|---|---|
| `locals_account.tf` | `account_id`, `zones` |
| `locals_dns.tf` | `dns_records` (nested: zone -> records) |
| `locals_members.tf` | `members` |
| `locals_pages.tf` | `kv_namespaces`, `pages_projects` |

## Flattening

`zone_dns_records` local flattens the nested `dns_records` map into `"zone:key"` composite keys for `for_each`. Each entry carries zone, type, name, content, ttl, proxied, comment, priority.

## Bot integration

**Status: Partially integrated (DNS records only)**

The conCierge bot reads and writes `locals_dns.tf`. Path constant in `handler.go`:

```go
pathCloudflareDNS = "terraform/cloudflare/locals_dns.tf"
```

| Operation | Supported |
|---|---|
| Add DNS record | yes |
| Delete DNS record | yes |
| Update DNS record | yes |
| Zone management | no |
| Account members | no |

## Auth

The Cloudflare provider reads `CLOUDFLARE_API_TOKEN` automatically. No variable needed.

### Required API token permissions

| Resource | Permission |
|---|---|
| Zone | DNS Edit |
| Zone | Zone Read |

## Configuration examples

### Adding a zone

```hcl
zones = {
  "example.com" = {}
}
```

### Adding an A record

```hcl
"my-a-record" = {
  type    = "A"
  name    = "app.example.com"
  content = "203.0.113.50"
  proxied = true
}
```

### Adding a CNAME record

```hcl
"www-cname" = {
  type    = "CNAME"
  name    = "www"
  content = "example.com"
  proxied = true
}
```

### Adding an MX record

```hcl
"mx-primary" = {
  type     = "MX"
  name     = "example.com"
  content  = "mx.mail.com"
  priority = 10
}
```

### Adding a TXT record

```hcl
"txt-spf" = {
  type    = "TXT"
  name    = "example.com"
  content = "v=spf1 include:_spf.mail.com ~all"
}
```

### Adding an account member

```hcl
"user@example.com" = {
  roles = ["role-id-1", "role-id-2"]
}
```

### Adding a Cloudflare Pages project

```hcl
"my-app" = {
  production_branch = "main"
  compatibility_date  = "2026-05-25"
  compatibility_flags = ["nodejs_compat"]
  build_config = {
    build_command   = "npm run build"
    destination_dir = "dist"
    root_dir        = ""
  }
  custom_domains = [
    "app.example.com"
  ]
}
```

### Adding a Pages KV binding

```hcl
kv_namespaces = {
  "my-app-rate-limit" = {
    title = "my-app-rate-limit"
  }
}

pages_projects = {
  "my-app" = {
    kv_bindings = {
      RATE_LIMIT_KV = "my-app-rate-limit"
    }
  }
}
```
