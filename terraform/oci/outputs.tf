output "instance_ids" {
  description = "IDs of the OCI compute instances."
  value       = [for instance in oci_core_instance.main : instance.id]
}

output "instance_names" {
  description = "Display names of the OCI compute instances."
  value       = [for instance in oci_core_instance.main : instance.display_name]
}

output "instance_private_ips" {
  description = "Fixed private IP addresses assigned to the OCI compute instances."
  value       = local.instance_private_ips
}

output "instance_public_ips" {
  description = "Reserved public IP addresses assigned to the OCI compute instances."
  value       = [for public_ip in oci_core_public_ip.main : public_ip.ip_address]
}

output "data_volume_ids" {
  description = "IDs of the additional block volumes attached to the OCI compute instances."
  value       = [for volume in oci_core_volume.data : volume.id]
}

output "public_ip_ids" {
  description = "IDs of the reserved public IP resources attached to the OCI compute instances."
  value       = [for public_ip in oci_core_public_ip.main : public_ip.id]
}

output "network_security_group_id" {
  description = "ID of the OCI network security group attached to the instance."
  value       = oci_core_network_security_group.main.id
}

output "public_subnet_id" {
  description = "ID of the OCI public subnet."
  value       = oci_core_subnet.public.id
}

output "vcn_id" {
  description = "ID of the OCI virtual cloud network."
  value       = oci_core_vcn.main.id
}
