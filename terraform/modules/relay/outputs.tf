output "public_ip" {
  description = "Elastic IP of the relay"
  value       = aws_eip.relay.public_ip
}

output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.relay.id
}

output "relay_url" {
  description = "Full DNS hostname for the relay (e.g. relay-rlb.legacy.example or relay-rlb.goat.example depending on cloudflare_zone_name)."
  value       = "${local.dns_name}.${var.cloudflare_zone_name}"
}

output "ssh_key_secret_name" {
  description = "Secrets Manager name for SSH key"
  value       = aws_secretsmanager_secret.ssh_key.name
}

output "rathole_secret_name" {
  description = "Secrets Manager name for rathole credentials"
  value       = aws_secretsmanager_secret.rathole_credentials.name
}

output "vpc_id" {
  description = "VPC ID (created or provided)"
  value       = local.vpc_id
}
