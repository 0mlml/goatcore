# host-node — variables.
#
# See main.tf for the module's purpose. Sizing rationale for the defaults lives
# in docs/design/zagros-cloud-host-site.md §"Sizing" — it is arithmetic on the
# measured figures in docs/design/capacity-and-austerity.md, and the peer axis
# is honestly flagged there as UNMEASURED.

variable "site" {
  description = "Site id this node belongs to (e.g. 'zagros-cloud'). Threads through every resource name."
  type        = string
}

variable "goatnet" {
  description = "Goatnet id this node serves (e.g. 'zagros'). A node on the wrong goatnet enrolls on the wrong mesh."
  type        = string
}

variable "role" {
  description = "Node role: 'core' (mgmt control plane) or 'obs' (observability + storage). Roles, not hardware — ADR 1141."
  type        = string

  validation {
    condition     = contains(["core", "obs"], var.role)
    error_message = "role must be 'core' or 'obs' (ADR 1141 names two roles per four-node site)."
  }
}

variable "index" {
  description = "Which half of the role pair: 'a' (active) or 'b' (warm standby). Host becomes mgmt-<site>-<role>-<index>."
  type        = string

  validation {
    condition     = contains(["a", "b"], var.index)
    error_message = "index must be 'a' (active) or 'b' (warm standby)."
  }
}

variable "az" {
  description = "Availability zone. The A and B halves of a pair MUST be in different AZs — an AZ is the cloud analogue of the site LAN whose loss on 2026-08-21 took two goatnets' control planes together."
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type. No default on purpose: the caller states the size, because a silent default is how one environment's number becomes every environment's number (doctrine P5)."
  type        = string
}

variable "root_volume_size" {
  description = "Root EBS size (GiB). OS + /var only — container and service state live on the data volume."
  type        = number
  default     = 100
}

variable "data_volume_size" {
  description = "Data EBS size (GiB), mounted at /opt/docker-data. This is docker's data-root and every service's state dir, matching the on-prem LUKS volume's mountpoint so no role needs a cloud special-case."
  type        = number
}

variable "data_volume_iops" {
  description = "gp3 provisioned IOPS for the data volume. 3000 is the gp3 baseline (free); the obs tier's audit write rate is the reason this is tunable."
  type        = number
  default     = 3000
}

variable "data_volume_throughput" {
  description = "gp3 provisioned throughput (MiB/s). 125 is the gp3 baseline (free)."
  type        = number
  default     = 125
}

variable "netbird_setup_key" {
  description = "Plaintext NetBird setup-key for mesh enrollment. Must be valid + non-exhausted at apply time."
  type        = string
  sensitive   = true
}

variable "netbird_mgmt_url" {
  description = "NetBird management URL for enrollment."
  type        = string
}

variable "netbird_enroll_enabled" {
  description = "Enroll this node on the goatnet mesh at boot. FALSE for a node that will itself RUN the management plane it would otherwise enrol against — that is a bootstrap circularity, not a config choice. See main.tf."
  type        = bool
  default     = true
}

variable "ssm_bucket_arn" {
  description = "ARN of the goatnet's SSM S3 bucket, so the ansible aws_ssm connection plugin can stage files to this host with port 22 closed."
  type        = string
}

# ---------------------------------------------------------------------------
# Public ingress. Default CLOSED — the no-public-exposure invariant. Only the
# node that carries the enrollment apex during a DNS failover opens anything,
# and that is a deliberate, operator-watched flip.
# ---------------------------------------------------------------------------
variable "public_enabled" {
  description = "Attach an Elastic IP and open the public ingress ports. Default false = egress-only, no public surface, no EIP."
  type        = bool
  default     = false
}

variable "public_ingress_ports" {
  description = "TCP ports opened when public_enabled. 443 = Traefik (netbird mgmt + signal gRPC path-mux, Ory, portal); 80 = ACME HTTP-01 fallback."
  type        = list(number)
  default     = [443, 80]
}

variable "public_ingress_cidrs" {
  description = "CIDRs allowed to reach public_ingress_ports when public_enabled."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "wireguard_port" {
  description = "UDP port for the NetBird/WireGuard data plane. Opened only when public_enabled, so peers can form direct tunnels to a cloud-resident host instead of paying the relay hop."
  type        = number
  default     = 51820
}

variable "extra_security_group_ids" {
  description = "Additional SG ids to attach (e.g. an intra-site SG allowing the four nodes to talk Spock/MinIO-replication privately)."
  type        = list(string)
  default     = []
}

variable "tags" {
  description = "Resource tags (merged with the root module's compliance default_tags)."
  type        = map(string)
  default     = {}
}

# ---------------------------------------------------------------------------
# Machine image. Empty (the default) means: use the newest self-owned AMI
# tagged GoatImageClass=<role>, and fall back to stock Ubuntu 24.04 if no bake
# has been published yet. Pin an explicit id to freeze a site on a known image.
# Build a bake with ops/bootstrap/build-image-ec2.sh.
# ---------------------------------------------------------------------------
variable "ami_id" {
  description = "Explicit AMI id. Empty = newest self-owned bake tagged GoatImageClass=<role>, else stock Ubuntu 24.04 (the slow path; the module output says which was used)."
  type        = string
  default     = ""
}
