# modules/bridge — Network bridge VPS for inter-network connectivity
#
# General-purpose egress/ingress point for traffic between the goat
# mesh and external secure networks (partner TAK servers, etc.).
# Joins the Netbird mesh for internal connectivity. Forwards specific TCP
# ports to partner endpoints via socat.
#
# Separate from Netbird relay VPSes — bridges carry partner/federation
# traffic, relays carry Netbird management traffic.

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
  vpc_id        = local.create_vpc ? aws_vpc.bridge[0].id : var.vpc_id
  subnet_id     = local.create_vpc ? aws_subnet.bridge[0].id : var.public_subnet_id
  bridge_suffix = var.bridge_name != "" ? "-${var.bridge_name}" : ""
  resource_name = "${var.project}-${var.environment}-${var.service_name}${local.bridge_suffix}"
  dns_name      = var.bridge_name != "" ? "bridge-${var.bridge_name}" : "bridge"
}

# -- VPC (created only when vpc_id is not provided) ---------------------------

resource "aws_vpc" "bridge" {
  count      = local.create_vpc ? 1 : 0
  cidr_block = var.vpc_cidr

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

resource "aws_internet_gateway" "bridge" {
  count  = local.create_vpc ? 1 : 0
  vpc_id = aws_vpc.bridge[0].id

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

resource "aws_subnet" "bridge" {
  count                   = local.create_vpc ? 1 : 0
  vpc_id                  = aws_vpc.bridge[0].id
  cidr_block              = var.vpc_cidr
  map_public_ip_on_launch = true
  availability_zone       = "${var.region}a"

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

resource "aws_route_table" "bridge" {
  count  = local.create_vpc ? 1 : 0
  vpc_id = aws_vpc.bridge[0].id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.bridge[0].id
  }

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

resource "aws_route_table_association" "bridge" {
  count          = local.create_vpc ? 1 : 0
  subnet_id      = aws_subnet.bridge[0].id
  route_table_id = aws_route_table.bridge[0].id
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
  description = "Network bridge: TCP forwarding + WARP egress"  # keep original to avoid SG replacement
  vpc_id      = local.vpc_id

  tags = { Service = var.service_name }
}

# Separate rule resources instead of inline dynamic blocks. Inline rules with
# an empty for_each omit the ingress attribute entirely, which makes the AWS
# provider treat existing rules as externally managed and skip removal.

resource "aws_vpc_security_group_ingress_rule" "ssh" {
  count = length(var.ssh_cidr_blocks) > 0 ? 1 : 0

  security_group_id = aws_security_group.main.id
  ip_protocol       = "tcp"
  from_port         = 22
  to_port           = 22
  cidr_ipv4         = var.ssh_cidr_blocks[0]
  description       = "SSH (restricted)"
}

resource "aws_vpc_security_group_ingress_rule" "forwarded_port" {
  for_each = {
    for pair in setproduct(var.forwarded_tcp_ports, var.forwarded_port_cidr_blocks) :
    "${pair[0]}-${pair[1]}" => { port = pair[0], cidr = pair[1] }
  }

  security_group_id = aws_security_group.main.id
  ip_protocol       = "tcp"
  from_port         = each.value.port
  to_port           = each.value.port
  cidr_ipv4         = each.value.cidr
  description       = "Forwarded TCP port ${each.value.port}"
}

resource "aws_vpc_security_group_ingress_rule" "forwarded_range" {
  # Keys use the (range-index, cidr-index) tuple — NOT the cidr string itself —
  # so the for_each is computable at plan time even when cidr_blocks contains
  # apply-time values (e.g. a freshly-created EC2's public_ip). A key that
  # embeds `${cidr}` directly breaks plan with "Invalid for_each
  # argument: known only after apply" the moment any cidr came from a sibling
  # resource.
  for_each = {
    for pair in flatten([
      for r_idx, r in var.forwarded_port_ranges : [
        for c_idx, cidr in r.cidr_blocks : {
          key      = "${r.protocol}-${r.from}-${r.to}-${r_idx}-${c_idx}"
          from     = r.from
          to       = r.to
          protocol = r.protocol
          cidr     = cidr
        }
      ]
    ]) : pair.key => pair
  }

  security_group_id = aws_security_group.main.id
  ip_protocol       = each.value.protocol
  from_port         = each.value.from
  to_port           = each.value.to
  cidr_ipv4         = each.value.cidr
  description       = "Forwarded ${each.value.protocol} ${each.value.from}${each.value.from != each.value.to ? "-${each.value.to}" : ""}"
}

resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.main.id
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
  description       = "All outbound (Netbird + forwarded traffic)"
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

resource "aws_instance" "bridge" {
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
    Purpose = "Network bridge: Netbird mesh + TCP forwarding to partner networks"
  }

  lifecycle {
    ignore_changes = [ami]
  }
}

# -- Elastic IP (stable public address for partner whitelisting) ---------------

resource "aws_eip" "bridge" {
  instance = aws_instance.bridge.id
  domain   = "vpc"

  tags = {
    Name    = local.resource_name
    Service = var.service_name
  }
}

# -- Cloudflare DNS -------------------------------------------------------------

resource "cloudflare_record" "bridge" {
  zone_id = var.cloudflare_zone_id
  name    = local.dns_name
  content = aws_eip.bridge.public_ip
  type    = "A"
  proxied = false
}
