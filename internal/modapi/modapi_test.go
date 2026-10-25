package modapi

import "testing"

func TestFromFilesReadsVariableFacts(t *testing.T) {
	api, err := FromFiles(map[string]string{"variables.tf": `
variable "name" {
  type        = string
  description = "the name"
}

variable "size" {
  type      = number
  default   = 3
  sensitive = true
  nullable  = false

  validation {
    condition     = var.size > 0
    error_message = "positive please"
  }
}

variable "loose" {}
`})
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}
	if got := api.VariableNames(); len(got) != 3 || got[0] != "loose" {
		t.Fatalf("names = %v", got)
	}

	name := api.Variables["name"]
	if name.HasDefault {
		t.Error("name should have no default")
	}
	if !name.Nullable {
		t.Error("nullable defaults to true in Terraform")
	}
	if name.Description != "the name" {
		t.Errorf("description = %q", name.Description)
	}

	size := api.Variables["size"]
	if !size.HasDefault || size.Default != "3" {
		t.Errorf("default = %q has=%v", size.Default, size.HasDefault)
	}
	if !size.Sensitive || size.Nullable {
		t.Errorf("sensitive=%v nullable=%v", size.Sensitive, size.Nullable)
	}
	if size.Validations != 1 {
		t.Errorf("validations = %d", size.Validations)
	}

	if got := api.Variables["loose"].Type; got != "any" {
		t.Errorf("omitted type = %q, want any", got)
	}
}

func TestFromFilesReadsOutputs(t *testing.T) {
	api, err := FromFiles(map[string]string{"outputs.tf": `
output "plain" {
  value = 1
}

output "secret" {
  value       = "s3cret"
  sensitive   = true
  description = "do not log"
}
`})
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}
	if got := api.OutputNames(); len(got) != 2 {
		t.Fatalf("outputs = %v", got)
	}
	if api.Outputs["plain"].Sensitive {
		t.Error("plain should not be sensitive")
	}
	if !api.Outputs["secret"].Sensitive || api.Outputs["secret"].Description != "do not log" {
		t.Errorf("secret = %+v", *api.Outputs["secret"])
	}
}

func TestFromFilesIgnoresOtherBlockTypes(t *testing.T) {
	api, err := FromFiles(map[string]string{"main.tf": `
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

resource "aws_vpc" "this" {
  cidr_block = var.cidr_block
}

variable "cidr_block" {
  type = string
}
`})
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}
	if len(api.Variables) != 1 || len(api.Outputs) != 0 {
		t.Errorf("vars=%d outputs=%d", len(api.Variables), len(api.Outputs))
	}
}

func TestFromFilesRejectsBadInput(t *testing.T) {
	if _, err := FromFiles(map[string]string{"bad.tf": `variable "a" {`}); err == nil {
		t.Error("unbalanced HCL should fail")
	}
	if _, err := FromFiles(map[string]string{"bad.tf": `
variable "a" { type = string }
variable "a" { type = number }`}); err == nil {
		t.Error("duplicate variable should fail")
	}
	if _, err := FromFiles(map[string]string{"bad.tf": `variable "a" { type = strung }`}); err == nil {
		t.Error("unknown type should fail")
	}
}
