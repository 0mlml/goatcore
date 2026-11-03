output "public_ip" {
  description = "Elastic IP of the crutch tier instance"
  value       = aws_eip.crutch.public_ip
}

output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.crutch.id
}

output "crutch_url" {
  description = "Full DNS hostname for this crutch instance (e.g. goat-public-fra.netbird-prod.goat.example). Reachable from public DNS only when dns_records_published = true."
  value       = "${local.dns_name}.${var.cloudflare_zone_name}"
}

output "round_robin_url" {
  description = "Full DNS hostname for the crutch round-robin pool (e.g. goat-public.netbird-prod.goat.example). Reachable from public DNS only when dns_records_published = true."
  value       = "${local.rr_dns_name}.${var.cloudflare_zone_name}"
}

output "ssh_key_secret_name" {
  description = "Secrets Manager name for SSH key"
  value       = aws_secretsmanager_secret.ssh_key.name
}

output "vpc_id" {
  description = "VPC ID (created or provided)"
  value       = local.vpc_id
}

output "dns_records_published" {
  description = "Whether public DNS records are currently published for this crutch (mirrors var.dns_records_published)."
  value       = var.dns_records_published
}
