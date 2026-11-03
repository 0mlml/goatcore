# modules/goat-public-mtls-crutch — Block 80 / ADR 0843
#
# Public mTLS-gated crutch tier — operator-toggleable parallel reachability
# surface. One VPS instance per region; consumed from terraform-prod with
# three module stanzas (fra / isr / mum). Sibling-shape to modules/goat-relay
# with these deltas:
#
#  - instance_type default = t3.small (operator decision per
#    ADR 0843 §D2; Block 80H capacity drill validates or upgrades).
#  - Security group exposes ONLY TCP 443 (Traefik with
#    RequireAndVerifyClientCert mTLS gate). No relay data plane (relay leg
#    reverse-proxies the existing goat-relay-rewrite tier over wg-cp0 per
#    ADR 0843 §D8). No wg-cp0 listener (this tier is a wg-cp0 *client*,
#    not a server — outbound only, covered by the egress-all rule).
#  - DNS publishes goat-public-<name>.netbird-prod.<zone> + optional
#    round-robin goat-public.netbird-prod.<zone> per design §3.
#  - dns_records_published variable lets operators pull DNS cleanly without
#    destroying instances (the "Deactivated with VPSes kept" sub-state per
#    design §8 / ADR 0843 §D5).
#  - terraform-prod consumer wraps the module in `count = var.crutch_tier_enabled`
#    so the default state of trunk is substrate-as-code only — zero live
#    AWS resources until an operator flips the gate.

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

# -- Locals -------------------------------------------------------------------

locals {
  create_vpc    = var.vpc_id == ""
  vpc_id        = local.create_vpc ? aws_vpc.crutch[0].id : var.vpc_id
  subnet_id     = local.create_vpc ? aws_subnet.crutch[0].id : var.public_subnet_id
  crutch_suffix = var.crutch_name != "" ? "-${var.crutch_name}" : ""
  resource_name = "${var.project}-${var.environment}-${var.service_name}${local.crutch_suffix}"
  dns_name      = var.crutch_name != "" ? "${var.crutch_dns_prefix}-${var.crutch_name}.${var.crutch_dns_parent}" : "${var.crutch_dns_prefix}.${var.crutch_dns_parent}"
  rr_dns_name   = "${var.round_robin_dns_name}.${var.round_robin_dns_parent}"
}

# -- VPC (created only when vpc_id is not provided) ---------------------------

resource "aws_vpc" "crutch" {
  count      = local.create_vpc ? 1 : 0
  cidr_block = var.vpc_cidr

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

resource "aws_internet_gateway" "crutch" {
  count  = local.create_vpc ? 1 : 0
  vpc_id = aws_vpc.crutch[0].id

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

resource "aws_subnet" "crutch" {
  count                   = local.create_vpc ? 1 : 0
  vpc_id                  = aws_vpc.crutch[0].id
  cidr_block              = var.vpc_cidr
  map_public_ip_on_launch = true
  availability_zone       = "${var.region}a"

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

resource "aws_route_table" "crutch" {
  count  = local.create_vpc ? 1 : 0
  vpc_id = aws_vpc.crutch[0].id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.crutch[0].id
  }

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

resource "aws_route_table_association" "crutch" {
  count          = local.create_vpc ? 1 : 0
  subnet_id      = aws_subnet.crutch[0].id
  route_table_id = aws_route_table.crutch[0].id
}

# -- SSH Key Pair --------------------------------------------------------------

resource "tls_private_key" "ssh" {
  algorithm = "RSA"
  rsa_bits  = 4096
}

resource "aws_key_pair" "main" {
  key_name   = local.resource_name
  public_key = tls_private_key.ssh.public_key_openssh
  tags       = { Service = var.service_name }
}

resource "random_id" "secret_suffix" {
  byte_length = 4
}

resource "aws_secretsmanager_secret" "ssh_key" {
  name        = "${local.resource_name}-ssh-${random_id.secret_suffix.hex}"
  description = "SSH private key for ${local.resource_name}"
  tags        = { Service = var.service_name }
}

resource "aws_secretsmanager_secret_version" "ssh_key" {
  secret_id     = aws_secretsmanager_secret.ssh_key.id
  secret_string = tls_private_key.ssh.private_key_pem
}

# -- IAM Role (SSM) -----------------------------------------------------------

resource "aws_iam_role" "ec2" {
  name = local.resource_name

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })

  tags = { Service = var.service_name }
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ec2" {
  name = local.resource_name
  role = aws_iam_role.ec2.name
}

# -- Security Group ------------------------------------------------------------

resource "aws_security_group" "main" {
  name        = local.resource_name
  description = "Block 80 / ADR 0843: Public mTLS crutch tier (TCP 443 only)"
  vpc_id      = local.vpc_id

  # SSH: no public access by default. Use SSM Session Manager for shell.
  dynamic "ingress" {
    for_each = length(var.ssh_cidr_blocks) > 0 ? [1] : []
    content {
      from_port   = 22
      to_port     = 22
      protocol    = "tcp"
      cidr_blocks = var.ssh_cidr_blocks
      description = "SSH (restricted; SSM is the default path)"
    }
  }

  # Traefik mTLS gate on the standard HTTPS port. ADR 0843 §D1: crutch
  # surfaces mgmt-API + signal-server + relay over standard TLS on the
  # standard port. RequireAndVerifyClientCert is enforced at the Traefik
  # layer by the goat-public-mtls-crutch Ansible role (Block 80B).
  # NOTE: SG description fields are sent to the AWS API which rejects
  # em-dashes (regex on ASCII printables only). Keep these ASCII-clean.
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Traefik mTLS (Block 80): RequireAndVerifyClientCert at the gate"
  }

  ingress {
    from_port   = -1
    to_port     = -1
    protocol    = "icmp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "ICMP ping (reachability checks)"
  }

  # All egress — the crutch box runs a wg-cp0-client (Block 22 / ADR 0313)
  # to reach the inner mgmt-plane over the silent control plane; that's an
  # outbound UDP flow to one of the existing wg-cp0 servers and needs no
  # special inbound port. mgmt + signal + relay backends are dialed over
  # that wg-cp0 link, not the public internet.
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "All outbound (wg-cp0-client + inner backends)"
  }

  tags = { Service = var.service_name }
}

# -- EC2 Instance (x86_64 Ubuntu 24.04) ----------------------------------------

data "aws_ami" "ubuntu_x86" {
  most_recent = true
  owners      = ["099720109477"]

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

resource "aws_instance" "crutch" {
  ami                    = data.aws_ami.ubuntu_x86.id
  instance_type          = var.instance_type
  iam_instance_profile   = aws_iam_instance_profile.ec2.name
  vpc_security_group_ids = [aws_security_group.main.id]
  subnet_id              = local.subnet_id
  key_name               = aws_key_pair.main.key_name

  root_block_device {
    volume_size           = 20
    volume_type           = "gp3"
    encrypted             = true
    delete_on_termination = true
  }

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  tags = {
    Name    = local.resource_name
    Service = var.service_name
    Region  = var.region
    Purpose = "Block 80 crutch tier — operator-toggleable mTLS-gated public reachability surface"
  }

  lifecycle {
    ignore_changes = [ami]
  }
}

# -- Elastic IP (stable public address) ----------------------------------------

resource "aws_eip" "crutch" {
  instance = aws_instance.crutch.id
  domain   = "vpc"

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

# -- Cloudflare DNS -------------------------------------------------------------
#
# Per-host record `<crutch_dns_prefix>-<name>.<crutch_dns_parent>.<zone>` plus
# optional membership in the `<round_robin_dns_name>.<round_robin_dns_parent>.<zone>`
# round-robin pool. Both gated by `dns_records_published` — when false (the
# default lifecycle state per ADR 0843 §D5), records are absent from public
# DNS even when the VPSes are up. This is the "Deactivated with VPSes kept"
# sub-state that lets operators pull discoverability cleanly without
# destroying instances.

resource "cloudflare_record" "crutch" {
  count   = var.dns_records_published ? 1 : 0
  zone_id = var.cloudflare_zone_id
  name    = local.dns_name
  content = aws_eip.crutch.public_ip
  type    = "A"
  proxied = false
}

resource "cloudflare_record" "round_robin" {
  count   = var.dns_records_published && var.round_robin_dns ? 1 : 0
  zone_id = var.cloudflare_zone_id
  name    = local.rr_dns_name
  content = aws_eip.crutch.public_ip
  type    = "A"
  ttl     = 60
  proxied = false
}
