data "oci_core_images" "ubuntu_24" {
  count = var.image_id == null ? 1 : 0

  compartment_id           = var.OCI_COMPARTMENT_OCID
  operating_system         = "Canonical Ubuntu"
  operating_system_version = "24.04"
  shape                    = local.oci_stack.instance_shape
  sort_by                  = "TIMECREATED"
  sort_order               = "DESC"
}

resource "oci_core_vcn" "main" {
  compartment_id = var.OCI_COMPARTMENT_OCID
  cidr_block     = local.oci_stack.vcn_cidr_block
  display_name   = "${local.resource_name}-vcn"
  freeform_tags  = local.common_freeform_tags
}

resource "oci_core_internet_gateway" "main" {
  compartment_id = var.OCI_COMPARTMENT_OCID
  display_name   = "${local.resource_name}-igw"
  enabled        = true
  freeform_tags  = local.common_freeform_tags
  vcn_id         = oci_core_vcn.main.id
}

resource "oci_core_route_table" "public" {
  compartment_id = var.OCI_COMPARTMENT_OCID
  display_name   = "${local.resource_name}-public-rt"
  freeform_tags  = local.common_freeform_tags
  vcn_id         = oci_core_vcn.main.id

  route_rules {
    destination       = local.internet_cidr_block
    destination_type  = "CIDR_BLOCK"
    network_entity_id = oci_core_internet_gateway.main.id
  }
}

resource "oci_core_security_list" "public" {
  compartment_id = var.OCI_COMPARTMENT_OCID
  display_name   = "${local.resource_name}-public-sl"
  freeform_tags  = local.common_freeform_tags
  vcn_id         = oci_core_vcn.main.id

  ingress_security_rules {
    protocol    = "6"
    source      = var.SSH_INGRESS_CIDR
    source_type = "CIDR_BLOCK"
    stateless   = false

    tcp_options {
      max = 22
      min = 22
    }
  }

  ingress_security_rules {
    protocol    = "6"
    source      = local.internet_cidr_block
    source_type = "CIDR_BLOCK"
    stateless   = false

    tcp_options {
      max = 80
      min = 80
    }
  }

  ingress_security_rules {
    protocol    = "6"
    source      = local.internet_cidr_block
    source_type = "CIDR_BLOCK"
    stateless   = false

    tcp_options {
      max = 443
      min = 443
    }
  }

  egress_security_rules {
    destination      = local.internet_cidr_block
    destination_type = "CIDR_BLOCK"
    protocol         = "all"
    stateless        = false
  }
}

resource "oci_core_subnet" "public" {
  cidr_block                 = local.oci_stack.public_subnet_cidr
  compartment_id             = var.OCI_COMPARTMENT_OCID
  display_name               = "${local.resource_name}-public-subnet"
  freeform_tags              = local.common_freeform_tags
  prohibit_public_ip_on_vnic = false
  route_table_id             = oci_core_route_table.public.id
  security_list_ids          = [oci_core_security_list.public.id]
  vcn_id                     = oci_core_vcn.main.id
}

resource "oci_core_network_security_group" "main" {
  compartment_id = var.OCI_COMPARTMENT_OCID
  display_name   = "${local.resource_name}-nsg"
  freeform_tags  = local.common_freeform_tags
  vcn_id         = oci_core_vcn.main.id
}

resource "oci_core_network_security_group_security_rule" "egress_all" {
  destination               = local.internet_cidr_block
  destination_type          = "CIDR_BLOCK"
  direction                 = "EGRESS"
  network_security_group_id = oci_core_network_security_group.main.id
  protocol                  = "all"
  stateless                 = false
}

resource "oci_core_network_security_group_security_rule" "http_ingress" {
  direction                 = "INGRESS"
  network_security_group_id = oci_core_network_security_group.main.id
  protocol                  = "6"
  source                    = local.internet_cidr_block
  source_type               = "CIDR_BLOCK"
  stateless                 = false

  tcp_options {
    destination_port_range {
      max = 80
      min = 80
    }
  }
}

resource "oci_core_network_security_group_security_rule" "https_ingress" {
  direction                 = "INGRESS"
  network_security_group_id = oci_core_network_security_group.main.id
  protocol                  = "6"
  source                    = local.internet_cidr_block
  source_type               = "CIDR_BLOCK"
  stateless                 = false

  tcp_options {
    destination_port_range {
      max = 443
      min = 443
    }
  }
}

resource "oci_core_network_security_group_security_rule" "ssh_ingress" {
  direction                 = "INGRESS"
  network_security_group_id = oci_core_network_security_group.main.id
  protocol                  = "6"
  source                    = var.SSH_INGRESS_CIDR
  source_type               = "CIDR_BLOCK"
  stateless                 = false

  tcp_options {
    destination_port_range {
      max = 22
      min = 22
    }
  }
}

resource "oci_core_instance" "main" {
  count = local.oci_stack.instance_count

  availability_domain = var.OCI_AVAILABILITY_DOMAIN
  compartment_id      = var.OCI_COMPARTMENT_OCID
  display_name        = "${local.resource_name}-${count.index + 1}"
  freeform_tags       = local.common_freeform_tags
  shape               = local.oci_stack.instance_shape

  create_vnic_details {
    assign_private_dns_record = true
    assign_public_ip          = false
    nsg_ids                   = [oci_core_network_security_group.main.id]
    private_ip                = local.instance_private_ips[count.index]
    subnet_id                 = oci_core_subnet.public.id
  }

  metadata = {
    ssh_authorized_keys = trimspace(var.OCI_SSH_AUTHORIZED_KEYS)
  }

  dynamic "shape_config" {
    for_each = length(regexall("\\.Flex$", local.oci_stack.instance_shape)) > 0 ? [1] : []

    content {
      memory_in_gbs = local.oci_stack.instance_memory_in_gbs
      ocpus         = local.oci_stack.instance_ocpus
    }
  }

  source_details {
    boot_volume_size_in_gbs = local.oci_stack.boot_volume_size_gbs
    source_id               = var.image_id != null ? var.image_id : data.oci_core_images.ubuntu_24[0].images[0].id
    source_type             = "IMAGE"
  }
}

data "oci_core_private_ips" "main" {
  count = local.oci_stack.instance_count

  ip_address = local.instance_private_ips[count.index]
  subnet_id  = oci_core_subnet.public.id

  depends_on = [oci_core_instance.main]
}

resource "oci_core_public_ip" "main" {
  count = local.oci_stack.instance_count

  compartment_id = var.OCI_COMPARTMENT_OCID
  display_name   = "${local.resource_name}-${count.index + 1}-public-ip"
  lifetime       = "RESERVED"
  private_ip_id  = data.oci_core_private_ips.main[count.index].private_ips[0].id
}

resource "oci_core_volume" "data" {
  count = local.oci_stack.data_volume_size_gbs > 0 ? local.oci_stack.instance_count : 0

  availability_domain = var.OCI_AVAILABILITY_DOMAIN
  compartment_id      = var.OCI_COMPARTMENT_OCID
  display_name        = "${local.resource_name}-${count.index + 1}-data"
  freeform_tags       = local.common_freeform_tags
  size_in_gbs         = local.oci_stack.data_volume_size_gbs
}

resource "oci_core_volume_attachment" "data" {
  count = local.oci_stack.data_volume_size_gbs > 0 ? local.oci_stack.instance_count : 0

  attachment_type = "paravirtualized"
  instance_id     = oci_core_instance.main[count.index].id
  volume_id       = oci_core_volume.data[count.index].id
}

data "oci_objectstorage_namespace" "main" {
  compartment_id = var.OCI_COMPARTMENT_OCID
}

resource "oci_objectstorage_bucket" "media" {
  compartment_id = var.OCI_COMPARTMENT_OCID
  name           = "${local.resource_name}-jae-pages-media"
  namespace      = data.oci_objectstorage_namespace.main.namespace
  access_type    = "ObjectReadWithoutList"
  storage_tier   = "Standard"
  freeform_tags  = local.common_freeform_tags
}

