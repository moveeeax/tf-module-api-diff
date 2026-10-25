output "vpc_id" {
  description = "ID of the VPC this module created."
  value       = "vpc-placeholder"
}

output "subnet_ids" {
  description = "Subnet IDs keyed by subnet name."
  value       = {}
}

output "flow_log_role_arn" {
  description = "ARN of the role used to publish flow logs."
  value       = ""
}

output "nat_gateway_ids" {
  description = "NAT gateway IDs, empty when enable_nat is false."
  value       = []
}
