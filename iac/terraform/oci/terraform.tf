terraform {
  required_version = ">= 1.5"

  required_providers {
    oci = {
      source  = "oracle/oci"
      version = "~> 7.26"
    }
  }

  backend "gcs" {
    bucket = "gh-jae-labs-terraform"
    prefix = "oci"
  }
}
