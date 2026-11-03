output "instance_id" {
  description = "EC2 instance id — the SSM target for ansible and for shell."
  value       = aws_instance.this.id
}

output "hostname" {
  description = "Inventory hostname. mgmt-host-baseline.yml renames the host to this."
  value       = local.hostname
}

output "private_ip" {
  description = "VPC private address. Intra-site traffic (Spock, MinIO replication) rides this, not the mesh."
  value       = aws_instance.this.private_ip
}

output "public_ip" {
  description = "Stable public address. Empty unless public_enabled — an empty value here is the correct reading of 'this node has no public surface', not a lookup failure."
  value       = var.public_enabled ? aws_eip.this[0].public_ip : ""
}

output "az" {
  description = "Availability zone. The A/B halves of a pair must differ; the site file asserts it."
  value       = aws_instance.this.availability_zone
}

output "data_volume_id" {
  description = "EBS data volume backing /opt/docker-data."
  value       = aws_ebs_volume.data.id
}

output "security_group_id" {
  description = "This node's SG, so an intra-site rule can name it as a source."
  value       = aws_security_group.this.id
}

output "ami_id" {
  description = "AMI the instance was launched from."
  value       = local.resolved_ami
}

output "image_path" {
  description = "'prebaked' (fast: engine + container images already in the image) or 'stock-ubuntu-slow-path' (first boot installs and pulls everything). Reported so a slow bringup has a visible cause instead of a mystery."
  value       = local.image_path
}
