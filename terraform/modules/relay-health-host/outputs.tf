output "instance_id" {
  value = aws_instance.this.id
}

output "hostname" {
  value = "relay-health-${var.goatnet}-${var.name}"
}

output "private_ip" {
  value = aws_instance.this.private_ip
}

# Stable public address — only set in PHASE 2 (public_enabled). Empty in PHASE 1.
output "public_ip" {
  value = var.public_enabled ? aws_eip.this[0].public_ip : ""
}

output "az" {
  value = aws_instance.this.availability_zone
}
