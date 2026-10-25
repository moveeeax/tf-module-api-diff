// Package diff compares two module APIs and classifies the result as a semver
// severity.
//
// The rule is caller-centric. If a module consumer who was happy before could
// now fail terraform init, plan or apply without changing their own code, the
// change is major. If they gain something they can opt into, it is minor.
// Everything else is patch, including a changed default value, which alters
// behaviour without breaking the call and so is always listed explicitly.
package diff

import (
	"fmt"
	"sort"

	"github.com/moveeeax/tf-module-api-diff/internal/modapi"
	"github.com/moveeeax/tf-module-api-diff/internal/typeexpr"
)

// Severity is a semver bump level.
type Severity int

const (
	Patch Severity = iota
	Minor
	Major
)

// String renders the severity as the word used on the command line.
func (s Severity) String() string {
	switch s {
	case Major:
		return "major"
	case Minor:
		return "minor"
	default:
		return "patch"
	}
}

// ParseSeverity reads a severity word, as accepted by --expect and --fail-on.
func ParseSeverity(s string) (Severity, error) {
	switch s {
	case "major":
		return Major, nil
	case "minor":
		return Minor, nil
	case "patch":
		return Patch, nil
	}
	return Patch, fmt.Errorf("unknown severity %q (want major, minor or patch)", s)
}

// Change is one difference between the two APIs.
type Change struct {
	Severity Severity `json:"-"`
	Level    string   `json:"severity"`
	Kind     string   `json:"kind"`
	Target   string   `json:"target"`
	Detail   string   `json:"detail"`
}

// Result is the whole comparison.
type Result struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Dir      string   `json:"dir"`
	Severity Severity `json:"-"`
	Level    string   `json:"severity"`
	Changes  []Change `json:"changes"`
}

// Changed reports whether anything differs at all.
func (r Result) Changed() bool { return len(r.Changes) > 0 }

// Count returns how many changes carry the given severity.
func (r Result) Count(s Severity) int {
	n := 0
	for _, c := range r.Changes {
		if c.Severity == s {
			n++
		}
	}
	return n
}

// Diff compares the old API against the new one.
func Diff(oldAPI, newAPI *modapi.API) Result {
	var changes []Change
	add := func(sev Severity, kind, target, detail string) {
		changes = append(changes, Change{
			Severity: sev, Level: sev.String(), Kind: kind, Target: target, Detail: detail,
		})
	}

	for _, name := range oldAPI.VariableNames() {
		oldVar := oldAPI.Variables[name]
		newVar, ok := newAPI.Variables[name]
		if !ok {
			add(Major, "VARIABLE_REMOVED", "var."+name,
				"callers that set it will fail with an unknown variable error")
			continue
		}
		compareVariable(add, oldVar, newVar)
	}
	for _, name := range newAPI.VariableNames() {
		if _, ok := oldAPI.Variables[name]; ok {
			continue
		}
		newVar := newAPI.Variables[name]
		if newVar.HasDefault {
			add(Minor, "VARIABLE_ADDED_OPTIONAL", "var."+name,
				"new input with default "+short(newVar.Default))
			continue
		}
		add(Major, "VARIABLE_ADDED_REQUIRED", "var."+name,
			"new input with no default, so every existing caller must now set it")
	}

	for _, name := range oldAPI.OutputNames() {
		oldOut := oldAPI.Outputs[name]
		newOut, ok := newAPI.Outputs[name]
		if !ok {
			add(Major, "OUTPUT_REMOVED", "output."+name,
				"anything referencing this output will fail to resolve")
			continue
		}
		switch {
		case !oldOut.Sensitive && newOut.Sensitive:
			add(Major, "OUTPUT_SENSITIVE_ADDED", "output."+name,
				"consumers passing it to a non-sensitive context now error")
		case oldOut.Sensitive && !newOut.Sensitive:
			add(Minor, "OUTPUT_SENSITIVE_REMOVED", "output."+name, "no longer marked sensitive")
		}
		if oldOut.Description != newOut.Description {
			add(Patch, "OUTPUT_DESCRIPTION_CHANGED", "output."+name, "description text only")
		}
	}
	for _, name := range newAPI.OutputNames() {
		if _, ok := oldAPI.Outputs[name]; !ok {
			add(Minor, "OUTPUT_ADDED", "output."+name, "new output, nothing to break")
		}
	}

	severity := Patch
	for _, c := range changes {
		if c.Severity > severity {
			severity = c.Severity
		}
	}
	sortChanges(changes)
	return Result{Severity: severity, Level: severity.String(), Changes: changes}
}

func compareVariable(add func(Severity, string, string, string), oldVar, newVar *modapi.Variable) {
	target := "var." + oldVar.Name

	switch {
	case oldVar.HasDefault && !newVar.HasDefault:
		add(Major, "VARIABLE_BECAME_REQUIRED", target,
			"default "+short(oldVar.Default)+" was dropped, so the caller must now set it")
	case !oldVar.HasDefault && newVar.HasDefault:
		add(Minor, "VARIABLE_BECAME_OPTIONAL", target, "gained default "+short(newVar.Default))
	case oldVar.HasDefault && newVar.HasDefault && oldVar.Default != newVar.Default:
		add(Patch, "VARIABLE_DEFAULT_CHANGED", target,
			short(oldVar.Default)+" -> "+short(newVar.Default)+" (behaviour change, not a break)")
	}

	switch typeexpr.Compare(oldVar.ParsedType(), newVar.ParsedType()) {
	case typeexpr.Narrower:
		add(Major, "VARIABLE_TYPE_NARROWED", target, oldVar.Type+" -> "+newVar.Type)
	case typeexpr.Wider:
		add(Minor, "VARIABLE_TYPE_WIDENED", target, oldVar.Type+" -> "+newVar.Type)
	}

	switch {
	case oldVar.Nullable && !newVar.Nullable:
		add(Major, "VARIABLE_NULLABLE_DISALLOWED", target, "callers passing null now error")
	case !oldVar.Nullable && newVar.Nullable:
		add(Minor, "VARIABLE_NULLABLE_ALLOWED", target, "null is now accepted")
	}

	switch {
	case !oldVar.Sensitive && newVar.Sensitive:
		add(Minor, "VARIABLE_SENSITIVE_ADDED", target, "value is now redacted in plan output")
	case oldVar.Sensitive && !newVar.Sensitive:
		add(Minor, "VARIABLE_SENSITIVE_REMOVED", target, "value is no longer redacted")
	}

	switch {
	case newVar.Validations > oldVar.Validations:
		add(Minor, "VARIABLE_VALIDATION_ADDED", target,
			fmt.Sprintf("%d -> %d validation blocks; a previously accepted value may now be rejected",
				oldVar.Validations, newVar.Validations))
	case newVar.Validations < oldVar.Validations:
		add(Patch, "VARIABLE_VALIDATION_REMOVED", target,
			fmt.Sprintf("%d -> %d validation blocks", oldVar.Validations, newVar.Validations))
	}

	if oldVar.Description != newVar.Description {
		add(Patch, "VARIABLE_DESCRIPTION_CHANGED", target, "description text only")
	}
}

func sortChanges(changes []Change) {
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Severity != changes[j].Severity {
			return changes[i].Severity > changes[j].Severity
		}
		if changes[i].Target != changes[j].Target {
			return changes[i].Target < changes[j].Target
		}
		return changes[i].Kind < changes[j].Kind
	})
}

func short(s string) string {
	const limit = 48
	if s == "" {
		return `""`
	}
	if len(s) <= limit {
		return s
	}
	return s[:limit-3] + "..."
}
