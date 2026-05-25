locals {
  oci_stack = {
    boot_volume_size_gbs   = 200
    data_volume_size_gbs   = 0
    environment            = "prod"
    instance_count         = 1
    instance_memory_in_gbs = 1
    instance_ocpus         = 1
    instance_shape         = "VM.Standard.E2.1.Micro"
    project_name           = "oci"
    public_subnet_cidr     = "10.80.0.0/24"
    vcn_cidr_block         = "10.80.0.0/16"
  }

  common_freeform_tags = {
    Environment = local.oci_stack.environment
    ManagedBy   = "Terraform"
    Project     = local.oci_stack.project_name
  }

  internet_cidr_block = "0.0.0.0/0"
  instance_private_ips = [
    for idx in range(local.oci_stack.instance_count) :
    cidrhost(local.oci_stack.public_subnet_cidr, idx + 10)
  ]
  resource_name = "${local.oci_stack.project_name}-${local.oci_stack.environment}"
}
