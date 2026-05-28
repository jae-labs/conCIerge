# OCI Module

Manages a minimal OCI stack for `jae-labs`: one VCN, one public subnet, explicit security controls, one Intel E2.1.Micro compute instance by default, no attached data volumes by default, and fixed private/public IPs for the instance.

## Resources managed

| Resource | Key | Description |
|---|---|---|
| `oci_core_vcn` | `main` | Virtual cloud network for the OCI stack |
| `oci_core_internet_gateway` | `main` | Internet gateway for outbound internet access |
| `oci_core_route_table` | `public` | Public route table with a default route |
| `oci_core_security_list` | `public` | Subnet-level security list with SSH ingress from `ssh_ingress_cidr`, HTTP/HTTPS ingress from the internet, and outbound internet access |
| `oci_core_subnet` | `public` | Public subnet for the compute instances |
| `oci_core_network_security_group` | `main` | Network security group attached to the compute instances |
| `oci_core_network_security_group_security_rule` | `ssh_ingress`, `http_ingress`, `https_ingress`, `egress_all` | Explicit ingress and egress rules |
| `oci_core_instance` | `main[count]` | Compute instances running Ubuntu 24.04 on `VM.Standard.E2.1.Micro` by default |
| `oci_core_private_ips` | `main[count]` | Lookup of the fixed primary private IPs created for each instance |
| `oci_core_public_ip` | `main[count]` | Reserved public IPs attached to the fixed private IPs |
| `oci_core_volume` | `data[count]` | Optional additional block volumes attached when `data_volume_size_gbs` is greater than `0` |
| `oci_core_volume_attachment` | `data[count]` | Optional paravirtualized block volume attachments |
| `oci_core_images` | `ubuntu_24` | Image lookup for the latest Ubuntu 24.04 image matching the chosen shape |
| `oci_objectstorage_namespace` | `main` | Dynamic OCI Object Storage namespace lookup |
| `oci_objectstorage_bucket` | `media` | Object Storage bucket for public jae-pages media with no listing permission |

## Variables

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `availability_domain` | `string` | yes | - | Exact tenancy-prefixed OCI availability-domain name for the compute instances, for example `tjxx:eu-amsterdam-1-AD-1` |
| `compartment_id` | `string` | yes | - | OCI compartment OCID for the stack |
| `image_id` | `string` | no | `null` | Optional custom image OCID; defaults to latest Ubuntu 24.04 |
| `ssh_authorized_keys` | `string` | yes | - | SSH authorized keys content for the instances |
| `ssh_ingress_cidr` | `string` | yes | - | CIDR block allowed to reach port 22 |

## Locals

| Local | Purpose |
|---|---|
| `oci_stack` | Committed non-secret OCI topology defaults: project name, environment, network CIDRs, instance count, shape, OCPUs, memory, and boot/data volume sizes |
| `common_freeform_tags` | Shared freeform tags applied to OCI resources |
| `internet_cidr_block` | Default route and internet rule CIDR (`0.0.0.0/0`) |
| `instance_private_ips` | Fixed private IP addresses assigned to the instance VNICs |
| `resource_name` | Base resource name prefix derived from `project_name` and `environment` |

## Flattening

None. The OCI root module is intentionally flat and manages a single stack directly from `oci/`.

## Bot integration

**Status: Not integrated.**

The conCierge Slack bot does not read or write any OCI Terraform files.

## Host configuration

Post-provision host configuration for the OCI instance lives in the repository root `ansible/` folder.

Terraform remains the source of truth for OCI infrastructure resources and SSH key injection. Ansible handles in-instance configuration such as host firewall rules, nginx, and certbot after the instance is reachable.

## Auth

The OCI provider reads these environment variables automatically:

| Variable | Purpose |
|---|---|
| `OCI_TENANCY_OCID` | OCI tenancy OCID |
| `OCI_USER_OCID` | OCI user OCID |
| `OCI_FINGERPRINT` | API signing key fingerprint |
| `OCI_REGION` | Target OCI region |
| `OCI_PRIVATE_KEY_PATH` | Path to the OCI API private key PEM file |

Local stack inputs are typically exported as:

| Variable | Purpose |
|---|---|
| `TF_VAR_compartment_id` | Compartment OCID for the stack |
| `TF_VAR_availability_domain` | Exact tenancy-prefixed OCI availability-domain name |
| `TF_VAR_ssh_authorized_keys` | SSH authorized keys content |
| `TF_VAR_ssh_ingress_cidr` | CIDR allowed to reach SSH |

## Configuration examples

### Local environment exports

```bash
export OCI_TENANCY_OCID=ocid1.tenancy.oc1..example
export OCI_USER_OCID=ocid1.user.oc1..example
export OCI_FINGERPRINT=00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00
export OCI_REGION=eu-amsterdam-1
export OCI_PRIVATE_KEY_PATH=./oci-api-key.pem

export TF_VAR_compartment_id=ocid1.compartment.oc1..example
export TF_VAR_availability_domain=tjxx:eu-amsterdam-1-AD-1
export TF_VAR_ssh_authorized_keys="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexample user@example"
export TF_VAR_ssh_ingress_cidr=203.0.113.10/32
```

You can retrieve the correct availability-domain value with the OCI CLI:

```bash
oci iam availability-domain list --compartment-id "$TF_VAR_compartment_id"
```

Use the `name` field exactly as returned by OCI.

The committed defaults in `locals.tf` keep the stack simple for the current tenancy limits: `1` `VM.Standard.E2.1.Micro` instance, a `200` GB boot volume, no attached data volumes, one fixed private IP in the public subnet, and one reserved public IP.

### Apply the OCI stack

```bash
cd terraform/oci
terraform init
terraform plan
terraform apply
```

## Media Storage (Object Storage)

The flat stack manages a dedicated public media bucket: `${local.resource_name}-jae-pages-media` (e.g. `oci-prod-jae-pages-media`). It uses `ObjectReadWithoutList` access, meaning objects are fully downloadable/streamable directly by public clients, but their inventory cannot be listed, preventing public file enumeration.

### Outputs
* `bucket_name`: Name of the bucket.
* `bucket_namespace`: The Object Storage namespace of the tenancy.
* `media_base_url`: Helper URL template for building direct file access URLs.

### Manual Media Uploads
Because this is managed manually, you can upload video/media files via the OCI Console UI or OCI CLI:

#### Via OCI CLI
```bash
# Upload a large video file to the bucket
oci os object put \
  -ns <bucket_namespace> \
  -b <bucket_name> \
  --file /path/to/local/video.mp4 \
  --name video.mp4
```

#### File URL Assembly
Once uploaded, your file will be publicly accessible at:
```
https://objectstorage.<REGION>.oraclecloud.com/n/<bucket_namespace>/b/<bucket_name>/o/<file_name>
```
Substitute `<REGION>` with your active OCI region (e.g., `eu-amsterdam-1`), and use `<bucket_namespace>` and `<bucket_name>` from the Terraform outputs.

