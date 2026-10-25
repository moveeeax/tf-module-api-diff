terraform {
  required_version = ">= 1.5"
}

locals {
  base_tags = merge(var.tags, {
    Module = var.name
  })
}
