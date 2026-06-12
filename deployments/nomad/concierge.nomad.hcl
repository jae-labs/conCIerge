variable "image_tag" {
  type        = string
  description = "Docker image tag for the concierge container"
  default     = "latest"
}

job "concierge" {
  datacenters = ["dc1"]
  type        = "service"

  update {
    max_parallel      = 1
    canary            = 1
    min_healthy_time  = "10s"
    healthy_deadline  = "60s"
    progress_deadline = "2m"
    auto_revert       = true
    auto_promote      = true
  }

  group "concierge" {
    count = 1

    restart {
      attempts = 2
      interval = "2m"
      delay    = "5s"
      mode     = "fail"
    }

    network {
      mode = "bridge"

      port "http" {
        to = 8080
      }

      port "metrics" {
        to = 9090
      }
    }

    service {
      name     = "concierge"
      port     = "http"
      provider = "nomad"

      tags = [
        "traefik.enable=true",
        "traefik.http.routers.concierge.rule=PathPrefix(`/`)",
        "traefik.http.routers.concierge.entrypoints=web",
        "traefik.http.middlewares.concierge-retry.retry.attempts=2",
        "traefik.http.middlewares.concierge-retry.retry.initialinterval=100ms",
        "traefik.http.routers.concierge.middlewares=concierge-retry",
      ]

      canary_tags = [
        "traefik.enable=true",
        "traefik.nomad.canary=true",
      ]

      check {
        type     = "http"
        path     = "/healthz"
        interval = "10s"
        timeout  = "2s"

        check_restart {
          limit = 3
          grace = "15s"
        }
      }
    }

    service {
      name     = "concierge-metrics"
      port     = "metrics"
      provider = "nomad"
    }

    task "concierge" {
      driver = "docker"

      config {
        image = "ghcr.io/jae-labs/concierge:${var.image_tag}"
        ports = ["http", "metrics"]
      }

      env {
        OTEL_ENVIRONMENT             = "production"
        OTEL_EXPORTER_OTLP_ENDPOINT = "${attr.unique.network.ip-address}:4319"
      }

      template {
        data        = file("/etc/concierge/concierge.env")
        destination = "secrets/env"
        env         = true
      }

      resources {
        cpu    = 500
        memory = 512
      }
    }
  }
}
