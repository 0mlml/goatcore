variable "crutch_name" {
  description = "Short name for this crutch instance (e.g., 'fra', 'isr', 'mum'). Empty = primary (no suffix)."
  type        = string
  default     = ""
}

variable "region" {
  description = "AWS region (used for subnet AZ)"
  type        = string
}

variable "project" {
  description = "Project name prefix"
  type        = string
}

variable "environment" {
  description = "Environment name"
  type        = string
}

variable "service_name" {
  description = "Short service name (e.g., 'goat-public-mtls-crutch')"
  type        = string
  default     = "goat-public-mtls-crutch"
}

variable "instance_type" {
  description = <<-EOT
    EC2 instance type. Default t3.small per ADR 0843 §D2 / Block 80A — the
    starting size is locked here per operator decision 2026-05-11; Block 80H
    capacity drill validates against the mobile-bridging activation envelope
    and upgrades (e.g., to c6i.large) only if measured headroom is
    insufficient.
  EOT
  type        = string
  default     = "t3.small"
}

variable "vpc_id" {
  description = "Existing VPC ID. Empty = create standalone VPC."
  type        = string
  default     = ""
}

variable "public_subnet_id" {
  description = "Existing public subnet ID. Required when vpc_id is set."
  type        = string
  default     = ""
}

variable "vpc_cidr" {
  description = "CIDR for auto-created VPC"
  type        = string
  default     = "10.99.2.0/24"
}

variable "ssh_cidr_blocks" {
  description = "CIDR blocks allowed for SSH. Default empty = SSM-only."
  type        = list(string)
  default     = []
}

variable "cloudflare_zone_id" {
  description = "Cloudflare zone ID for DNS records"
  type        = string
}

variable "cloudflare_zone_name" {
  description = <<-EOT
    Cloudflare zone name (e.g. goat.example). Used by the crutch_url output to
    produce the full FQDN.
  EOT
  type        = string
  default     = "goat.example"
}

variable "crutch_dns_prefix" {
  description = <<-EOT
    Prefix for per-crutch public DNS records. Built as `<prefix>` (when
    crutch_name is empty) or `<prefix>-<crutch_name>`. Default `goat-public`
    matches the design doc §3 naming.
  EOT
  type        = string
  default     = "goat-public"
}

variable "crutch_dns_parent" {
  description = <<-EOT
    Parent subdomain under the Cloudflare zone for crutch records. Default
    `netbird-prod` matches the design doc §3 production-tree naming
    (`goat-public-<name>.netbird-prod.<zone>`).
  EOT
  type        = string
  default     = "netbird-prod"
}

variable "round_robin_dns" {
  description = <<-EOT
    Add this crutch to the goat-public round-robin pool. Default true so
    that all three goat-public-{fra,isr,mum} instances answer the same
    `<round_robin_dns_name>.<round_robin_dns_parent>.<zone>` hostname.
  EOT
  type        = bool
  default     = true
}

variable "round_robin_dns_name" {
  description = "Hostname (sans parent + zone) for the round-robin record."
  type        = string
  default     = "goat-public"
}

variable "round_robin_dns_parent" {
  description = "Parent subdomain under the Cloudflare zone for the round-robin record."
  type        = string
  default     = "netbird-prod"
}

variable "dns_records_published" {
  description = <<-EOT
    Block 80 lifecycle gate per ADR 0843 §D5 + design §8. When false (the
    default), no Cloudflare DNS records are created for this crutch — the
    VPS may be up and Traefik may be serving, but the host is undiscoverable
    via public DNS. This is the "Deactivated with VPSes kept" sub-state that
    lets operators pull discoverability cleanly without destroying instances.
    When true, the per-host record and (optionally) the round-robin entry
    are published.
  EOT
  type        = bool
  default     = false
}
