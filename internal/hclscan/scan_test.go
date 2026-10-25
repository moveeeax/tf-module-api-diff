package hclscan

import "testing"

func TestParseBlocksAndAttrs(t *testing.T) {
	src := `
# a leading comment
terraform {
  required_version = ">= 1.5"
}

variable "name" {
  type        = string
  description = "the name" // trailing comment
}

locals {
  tags = {
    env = "prod"
  }
}
`
	blocks, attrs, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(attrs) != 0 {
		t.Fatalf("expected no top-level attributes, got %v", attrs)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d: %+v", len(blocks), blocks)
	}
	v := blocks[1]
	if v.Type != "variable" || len(v.Labels) != 1 || v.Labels[0] != "name" {
		t.Fatalf("unexpected variable block header: %+v", v)
	}
	_, inner, err := Parse(v.Body)
	if err != nil {
		t.Fatalf("Parse body: %v", err)
	}
	if got, _ := AttrValue(inner, "type"); got != "string" {
		t.Errorf("type = %q, want string", got)
	}
	if got, _ := AttrValue(inner, "description"); got != `"the name"` {
		t.Errorf("description = %q", got)
	}
}

func TestParseKeepsNestedTypeExpressionIntact(t *testing.T) {
	src := `
variable "subnets" {
  type = map(object({
    cidr     = string
    public   = optional(bool, false)
  }))
  default = {}
}
`
	blocks, _, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	_, attrs, err := Parse(blocks[0].Body)
	if err != nil {
		t.Fatalf("Parse body: %v", err)
	}
	got, ok := AttrValue(attrs, "type")
	if !ok {
		t.Fatal("no type attribute")
	}
	want := "map(object({\n    cidr     = string\n    public   = optional(bool, false)\n  }))"
	if got != want {
		t.Errorf("type = %q\nwant %q", got, want)
	}
	if got, ok := AttrValue(attrs, "default"); !ok || got != "{}" {
		t.Errorf("default = %q ok=%v", got, ok)
	}
}

func TestParseHandlesStringsHeredocsAndValidation(t *testing.T) {
	src := `
variable "message" {
  type    = string
  default = "braces ${ "{" } and a hash # inside"

  description = <<-EOT
    a heredoc with a } brace
    and a "quote"
  EOT

  validation {
    condition     = length(var.message) > 0
    error_message = "must not be empty"
  }

  validation {
    condition     = can(regex("^[a-z]+$", var.message))
    error_message = "lowercase only"
  }
}
`
	blocks, _, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	nested, attrs, err := Parse(blocks[0].Body)
	if err != nil {
		t.Fatalf("Parse body: %v", err)
	}
	if n := len(BlocksOfType(nested, "validation")); n != 2 {
		t.Errorf("validation blocks = %d, want 2", n)
	}
	if got, _ := AttrValue(attrs, "default"); got != `"braces ${ "{" } and a hash # inside"` {
		t.Errorf("default = %q", got)
	}
}

func TestParseRejectsUnbalancedSource(t *testing.T) {
	for name, src := range map[string]string{
		"unterminated block":  `variable "a" {`,
		"stray close":         `}`,
		"unterminated string": "variable \"a\" {\n  default = \"oops\n}\n",
		"no body":             `variable "a"`,
	} {
		if _, _, err := Parse(src); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
