variable "availability_domain" {
  description = "Tenancy-prefixed availability domain name for the OCI compute instance, for example tjxx:eu-amsterdam-1-AD-1."
  type        = string

  validation {
    condition     = length(regexall("^[^:]+:.+-AD-[0-9]+$", var.availability_domain)) > 0
    error_message = "Availability domain must be the exact tenancy-prefixed OCI name, for example tjxx:eu-amsterdam-1-AD-1."
  }
}

variable "compartment_id" {
  description = "OCI compartment OCID that owns the network and compute resources."
  type        = string
}

variable "image_id" {
  description = "Optional custom OCI image OCID. When null, the latest Ubuntu 24.04 image for the selected shape is used."
  type        = string
  default     = null
  nullable    = true
}

variable "ssh_authorized_keys" {
  description = "SSH authorized keys content injected into the OCI instance metadata."
  type        = string

  validation {
    condition     = length(trimspace(var.ssh_authorized_keys)) > 0
    error_message = "SSH authorized keys must not be empty."
  }
}

variable "ssh_ingress_cidr" {
  description = "CIDR block allowed to reach the instance over SSH."
  type        = string

  validation {
    condition     = can(cidrhost(var.ssh_ingress_cidr, 0))
    error_message = "SSH ingress CIDR must be a valid IPv4 CIDR block."
  }
}
