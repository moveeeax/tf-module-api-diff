variable "name" {
  type        = string
  description = "Name prefix for every resource in the module."
}

# The default is gone: every caller must now pass a CIDR explicitly.
variable "cidr_block" {
  type        = string
  description = "IPv4 CIDR of the VPC."

  validation {
    condition     = can(cidrnetmask(var.cidr_block))
    error_message = "cidr_block must be a valid IPv4 CIDR."
  }
}

variable "tags" {
  type        = map(string)
  description = "Tags merged into every resource created by this module."
  default     = {}
}

variable "subnets" {
  type = map(object({
    cidr   = string
    public = optional(bool, false)
    az     = optional(string)
  }))
  description = "Subnets to create, keyed by name."
  default     = {}
}

# any -> a concrete object: callers who passed anything else now fail.
variable "flow_log_destination" {
  type = object({
    bucket_arn = string
    kms_key_id = optional(string)
  })
  description = "Where flow logs are shipped."
  default     = null
}

variable "single_nat_gateway" {
  type        = bool
  description = "Share one NAT gateway across every private subnet."
  default     = false
}
