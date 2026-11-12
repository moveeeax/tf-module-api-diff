package contract

import (
	"bytes"
	"strings"
	"testing"

	"github.com/moveeeax/tf-module-api-diff/internal/diff"
	"github.com/moveeeax/tf-module-api-diff/internal/modapi"
)

const module = `
variable "name" {
  type        = string
  description = "the name"
}

variable "subnets" {
  type = map(object({
    cidr   = string
    public = optional(bool, false)
  }))
  default   = {}
  nullable  = false
  sensitive = true

  validation {
    condition     = length(var.subnets) < 10
    error_message = "too many <subnets> & more"
  }
}

output "vpc_id" {
  description = "the VPC"
  value       = "x"
}

output "secret" {
  value     = "y"
  sensitive = true
}
`

func scanned(t *testing.T) *modapi.API {
	t.Helper()
	api, err := modapi.FromFiles(map[string]string{"main.tf": module})
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func TestWriteThenReadComparesAsUnchanged(t *testing.T) {
	api := scanned(t)
	var buf bytes.Buffer
	if err := Write(&buf, api); err != nil {
		t.Fatal(err)
	}
	back, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read: %v\n%s", err, buf.String())
	}
	if r := diff.Diff(back, api); r.Changed() {
		t.Errorf("round trip produced changes: %+v", r.Changes)
	}

	subnets := back.Variables["subnets"]
	if !subnets.HasDefault || subnets.Default != "{}" || subnets.Nullable || !subnets.Sensitive || subnets.Validations != 1 {
		t.Errorf("subnets = %+v", subnets)
	}
	if back.Variables["name"].HasDefault {
		t.Error("name has no default in the module, so none in the contract")
	}
	if !back.Outputs["secret"].Sensitive || back.Outputs["vpc_id"].Description != "the VPC" {
		t.Errorf("outputs = %+v %+v", back.Outputs["secret"], back.Outputs["vpc_id"])
	}
}

func TestWriteIsStableAndReadable(t *testing.T) {
	var a, b bytes.Buffer
	if err := Write(&a, scanned(t)); err != nil {
		t.Fatal(err)
	}
	if err := Write(&b, scanned(t)); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Error("two writes of the same module differ")
	}
	out := a.String()
	for _, want := range []string{`"contract": 1`, `"default": "{}"`, `"nullable": false`, `optional(bool)`} {
		if !strings.Contains(out, want) {
			t.Errorf("contract missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `\u003c`) {
		t.Errorf("HTML escaping leaked into the contract:\n%s", out)
	}
	// Keys are sorted, so outputs appear secret before vpc_id.
	if strings.Index(out, `"secret"`) > strings.Index(out, `"vpc_id"`) {
		t.Errorf("output keys not sorted:\n%s", out)
	}
}

func TestReadAppliesTerraformDefaultsToOmittedFields(t *testing.T) {
	api, err := Read(strings.NewReader(`{
  "contract": 1,
  "variables": {"region": {}, "tags": {"type": "map( string )", "default": "{ }"}},
  "outputs": {"id": {}}
}`))
	if err != nil {
		t.Fatal(err)
	}
	region := api.Variables["region"]
	if region.Type != "any" || region.HasDefault || !region.Nullable || region.Sensitive {
		t.Errorf("region = %+v, want any, required, nullable", region)
	}
	tags := api.Variables["tags"]
	if tags.Type != "map(string)" || tags.Default != "{ }" {
		t.Errorf("tags = %+v", tags)
	}
	if o := api.Outputs["id"]; o == nil || o.Name != "id" || o.Sensitive {
		t.Errorf("output = %+v", o)
	}
}

func TestReadRejectsBadContracts(t *testing.T) {
	cases := map[string]string{
		"not json":         `variable "x" {}`,
		"missing version":  `{"variables": {}, "outputs": {}}`,
		"future version":   `{"contract": 2, "variables": {}, "outputs": {}}`,
		"misspelt field":   `{"contract": 1, "outputs": {"id": {"sensitve": true}}}`,
		"unknown top key":  `{"contract": 1, "inputs": {}}`,
		"bad type":         `{"contract": 1, "variables": {"x": {"type": "map(("}}}`,
		"negative count":   `{"contract": 1, "variables": {"x": {"validations": -1}}}`,
		"empty name":       `{"contract": 1, "outputs": {"": {}}}`,
		"trailing objects": `{"contract": 1} {"contract": 1}`,
	}
	for name, src := range cases {
		if _, err := Read(strings.NewReader(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
