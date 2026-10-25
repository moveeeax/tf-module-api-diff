package diff

import (
	"strings"
	"testing"

	"github.com/moveeeax/tf-module-api-diff/internal/modapi"
)

// apiOf builds an API from a single synthetic variables.tf.
func apiOf(t *testing.T, src string) *modapi.API {
	t.Helper()
	api, err := modapi.FromFiles(map[string]string{"variables.tf": src})
	if err != nil {
		t.Fatalf("FromFiles: %v\n%s", err, src)
	}
	return api
}

func kinds(r Result) []string {
	out := make([]string, 0, len(r.Changes))
	for _, c := range r.Changes {
		out = append(out, c.Kind)
	}
	return out
}

func hasKind(r Result, kind string) *Change {
	for i := range r.Changes {
		if r.Changes[i].Kind == kind {
			return &r.Changes[i]
		}
	}
	return nil
}

func TestIdenticalAPIIsPatchWithNoChanges(t *testing.T) {
	src := `
variable "a" {
  type    = string
  default = "x"
}

output "b" {
  value = 1
}
`
	r := Diff(apiOf(t, src), apiOf(t, src))
	if r.Severity != Patch {
		t.Errorf("severity = %s, want patch", r.Severity)
	}
	if r.Changed() {
		t.Errorf("expected no changes, got %v", kinds(r))
	}
}

func TestRemovedVariableIsMajorAndNamed(t *testing.T) {
	oldSrc := `variable "keep" { type = string }
variable "gone" {
  type    = string
  default = "x"
}`
	newSrc := `variable "keep" { type = string }`
	r := Diff(apiOf(t, oldSrc), apiOf(t, newSrc))
	if r.Severity != Major {
		t.Fatalf("severity = %s, want major", r.Severity)
	}
	c := hasKind(r, "VARIABLE_REMOVED")
	if c == nil {
		t.Fatalf("no VARIABLE_REMOVED in %v", kinds(r))
	}
	if c.Target != "var.gone" {
		t.Errorf("target = %q, want var.gone", c.Target)
	}
}

func TestDroppedDefaultIsMajor(t *testing.T) {
	oldSrc := `variable "region" {
  type    = string
  default = "eu-west-1"
}`
	newSrc := `variable "region" { type = string }`
	r := Diff(apiOf(t, oldSrc), apiOf(t, newSrc))
	if r.Severity != Major {
		t.Fatalf("severity = %s, want major", r.Severity)
	}
	c := hasKind(r, "VARIABLE_BECAME_REQUIRED")
	if c == nil {
		t.Fatalf("no VARIABLE_BECAME_REQUIRED in %v", kinds(r))
	}
	if c.Target != "var.region" || !strings.Contains(c.Detail, "eu-west-1") {
		t.Errorf("change = %+v", *c)
	}
}

func TestAddedVariableSeverityDependsOnDefault(t *testing.T) {
	oldSrc := `variable "a" { type = string }`

	r := Diff(apiOf(t, oldSrc), apiOf(t, oldSrc+`
variable "b" {
  type    = number
  default = 3
}`))
	if r.Severity != Minor {
		t.Errorf("new optional variable = %s, want minor (%v)", r.Severity, kinds(r))
	}
	if hasKind(r, "VARIABLE_ADDED_OPTIONAL") == nil {
		t.Errorf("kinds = %v", kinds(r))
	}

	r = Diff(apiOf(t, oldSrc), apiOf(t, oldSrc+`
variable "b" { type = number }`))
	if r.Severity != Major {
		t.Errorf("new required variable = %s, want major (%v)", r.Severity, kinds(r))
	}
	if hasKind(r, "VARIABLE_ADDED_REQUIRED") == nil {
		t.Errorf("kinds = %v", kinds(r))
	}
}

func TestTypeChangeDirection(t *testing.T) {
	narrow := Diff(
		apiOf(t, `variable "a" { type = any }`),
		apiOf(t, `variable "a" { type = string }`),
	)
	if narrow.Severity != Major || hasKind(narrow, "VARIABLE_TYPE_NARROWED") == nil {
		t.Errorf("any -> string = %s %v", narrow.Severity, kinds(narrow))
	}

	widen := Diff(
		apiOf(t, `variable "a" { type = string }`),
		apiOf(t, `variable "a" { type = any }`),
	)
	if widen.Severity != Minor || hasKind(widen, "VARIABLE_TYPE_WIDENED") == nil {
		t.Errorf("string -> any = %s %v", widen.Severity, kinds(widen))
	}
}

func TestOutputSensitivityAndDescription(t *testing.T) {
	sensitive := Diff(
		apiOf(t, `output "arn" { value = "" }`),
		apiOf(t, `output "arn" {
  value     = ""
  sensitive = true
}`),
	)
	if sensitive.Severity != Major || hasKind(sensitive, "OUTPUT_SENSITIVE_ADDED") == nil {
		t.Errorf("output turned sensitive = %s %v", sensitive.Severity, kinds(sensitive))
	}

	described := Diff(
		apiOf(t, `output "arn" {
  value       = ""
  description = "old words"
}`),
		apiOf(t, `output "arn" {
  value       = ""
  description = "new words"
}`),
	)
	if described.Severity != Patch {
		t.Errorf("description-only = %s, want patch", described.Severity)
	}
	if hasKind(described, "OUTPUT_DESCRIPTION_CHANGED") == nil {
		t.Errorf("kinds = %v", kinds(described))
	}
}

func TestRemovedOutputIsMajor(t *testing.T) {
	r := Diff(
		apiOf(t, `output "a" { value = 1 }
output "b" { value = 2 }`),
		apiOf(t, `output "a" { value = 1 }`),
	)
	if r.Severity != Major || hasKind(r, "OUTPUT_REMOVED") == nil {
		t.Errorf("severity = %s, kinds = %v", r.Severity, kinds(r))
	}
}

func TestDefaultValueChangeIsPatchAndReported(t *testing.T) {
	r := Diff(
		apiOf(t, `variable "size" {
  type    = number
  default = 2
}`),
		apiOf(t, `variable "size" {
  type    = number
  default = 8
}`),
	)
	if r.Severity != Patch {
		t.Fatalf("severity = %s, want patch", r.Severity)
	}
	c := hasKind(r, "VARIABLE_DEFAULT_CHANGED")
	if c == nil {
		t.Fatalf("kinds = %v", kinds(r))
	}
	if !strings.Contains(c.Detail, "2 -> 8") {
		t.Errorf("detail = %q", c.Detail)
	}
}

func TestNullableTighteningIsMajor(t *testing.T) {
	r := Diff(
		apiOf(t, `variable "a" { type = string }`),
		apiOf(t, `variable "a" {
  type     = string
  nullable = false
}`),
	)
	if r.Severity != Major || hasKind(r, "VARIABLE_NULLABLE_DISALLOWED") == nil {
		t.Errorf("severity = %s, kinds = %v", r.Severity, kinds(r))
	}
}

func TestAddedValidationIsMinor(t *testing.T) {
	r := Diff(
		apiOf(t, `variable "a" { type = string }`),
		apiOf(t, `variable "a" {
  type = string

  validation {
    condition     = length(var.a) > 0
    error_message = "no"
  }
}`),
	)
	if r.Severity != Minor || hasKind(r, "VARIABLE_VALIDATION_ADDED") == nil {
		t.Errorf("severity = %s, kinds = %v", r.Severity, kinds(r))
	}
}

func TestReformattingIsNotAChange(t *testing.T) {
	oldSrc := `variable "subnets" {
  type = map(object({ cidr = string, public = optional(bool, false) }))
  default = {}
}`
	newSrc := `variable "subnets" {
  type = map(object({
    cidr   = string
    public = optional(bool, false)
  }))

  default = {}
}`
	r := Diff(apiOf(t, oldSrc), apiOf(t, newSrc))
	if r.Changed() {
		t.Errorf("reformatting produced %v", kinds(r))
	}
}

func TestChangesAreSortedBySeverity(t *testing.T) {
	r := Diff(
		apiOf(t, `variable "a" {
  type        = string
  default     = "x"
  description = "old"
}
variable "b" { type = string }`),
		apiOf(t, `variable "a" {
  type        = string
  default     = "x"
  description = "new"
}`),
	)
	if len(r.Changes) < 2 {
		t.Fatalf("expected at least 2 changes, got %v", kinds(r))
	}
	if r.Changes[0].Severity != Major {
		t.Errorf("first change = %+v, want the major one first", r.Changes[0])
	}
}

func TestSeverityRoundTrip(t *testing.T) {
	for _, word := range []string{"major", "minor", "patch"} {
		s, err := ParseSeverity(word)
		if err != nil {
			t.Fatalf("ParseSeverity(%q): %v", word, err)
		}
		if s.String() != word {
			t.Errorf("%q round-tripped to %q", word, s.String())
		}
	}
	if _, err := ParseSeverity("breaking"); err == nil {
		t.Error("ParseSeverity should reject an unknown word")
	}
}
