terraform {
  required_version = ">= 1.6"
}

locals {
  base_tags = merge(var.tags, {
    Module = var.name
  })
}
