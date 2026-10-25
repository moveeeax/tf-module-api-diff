variable "name" {
  type        = string
  description = "Name prefix for every resource in the module."
}

variable "cidr_block" {
  type        = string
  description = "IPv4 CIDR of the VPC."
  default     = "10.0.0.0/16"

  validation {
    condition     = can(cidrnetmask(var.cidr_block))
    error_message = "cidr_block must be a valid IPv4 CIDR."
  }
}

variable "tags" {
  type        = map(string)
  description = "Tags applied to every resource."
  default     = {}
}

variable "subnets" {
  type = map(object({
    cidr   = string
    public = optional(bool, false)
  }))
  description = "Subnets to create, keyed by name."
  default     = {}
}

variable "flow_log_destination" {
  type        = any
  description = "Where flow logs are shipped. Any shape the provider accepts."
  default     = null
}

variable "enable_nat" {
  type        = bool
  description = "Create a NAT gateway for the private subnets."
  default     = true
}
