output "vpc_id" {
  description = "ID of the VPC this module created."
  value       = "vpc-placeholder"
}

output "subnet_ids" {
  description = "Subnet IDs keyed by subnet name."
  value       = {}
}

# Now sensitive: consumers that interpolated it into a plain string error out.
output "flow_log_role_arn" {
  description = "ARN of the role used to publish flow logs."
  value       = ""
  sensitive   = true
}

output "nat_gateway_ids" {
  description = "NAT gateway IDs, empty when no NAT gateway is created."
  value       = []
}
