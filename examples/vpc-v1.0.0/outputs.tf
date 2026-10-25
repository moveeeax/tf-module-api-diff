output "vpc_id" {
  description = "ID of the created VPC."
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
