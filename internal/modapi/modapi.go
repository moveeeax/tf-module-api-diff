// Package modapi extracts a Terraform module's public API — its variables and
// outputs — from a set of .tf files.
//
// Everything a caller can see about a module is in these two block types: the
// variables it must or may set, and the outputs it can wire onward. Reading
// them is enough to know the module's contract at a given revision.
package modapi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/moveeeax/tf-module-api-diff/internal/hclscan"
	"github.com/moveeeax/tf-module-api-diff/internal/typeexpr"
)

// Variable is one input of the module.
type Variable struct {
	Name        string `json:"name"`
	Type        string `json:"type"`       // canonical type expression, "any" when omitted
	HasDefault  bool   `json:"hasDefault"` // false means the caller must set it
	Default     string `json:"default"`
	Nullable    bool   `json:"nullable"` // Terraform's default is true
	Sensitive   bool   `json:"sensitive"`
	Description string `json:"description"`
	Validations int    `json:"validations"`
	File        string `json:"file"`

	parsedType *typeexpr.Type
}

// ParsedType is the variable's type constraint, ready for comparison.
func (v *Variable) ParsedType() *typeexpr.Type {
	if v.parsedType == nil {
		return typeexpr.Any()
	}
	return v.parsedType
}

// Output is one value the module publishes.
type Output struct {
	Name        string `json:"name"`
	Sensitive   bool   `json:"sensitive"`
	Description string `json:"description"`
	File        string `json:"file"`
}

// API is the whole public surface of a module directory.
type API struct {
	Variables map[string]*Variable `json:"variables"`
	Outputs   map[string]*Output   `json:"outputs"`
}

// VariableNames returns the input names in sorted order.
func (a *API) VariableNames() []string { return sortedVarKeys(a.Variables) }

// OutputNames returns the output names in sorted order.
func (a *API) OutputNames() []string { return sortedOutKeys(a.Outputs) }

// FromFiles reads the API out of a filename-to-source map. Files are processed
// in name order so that a duplicate declaration reports deterministically.
func FromFiles(files map[string]string) (*API, error) {
	api := &API{Variables: map[string]*Variable{}, Outputs: map[string]*Output{}}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		blocks, _, err := hclscan.Parse(files[name])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for _, b := range blocks {
			switch b.Type {
			case "variable":
				v, err := readVariable(name, b)
				if err != nil {
					return nil, err
				}
				if _, dup := api.Variables[v.Name]; dup {
					return nil, fmt.Errorf("%s: variable %q declared twice", name, v.Name)
				}
				api.Variables[v.Name] = v
			case "output":
				o, err := readOutput(name, b)
				if err != nil {
					return nil, err
				}
				if _, dup := api.Outputs[o.Name]; dup {
					return nil, fmt.Errorf("%s: output %q declared twice", name, o.Name)
				}
				api.Outputs[o.Name] = o
			}
		}
	}
	return api, nil
}

func readVariable(file string, b hclscan.Block) (*Variable, error) {
	if len(b.Labels) != 1 || b.Labels[0] == "" {
		return nil, fmt.Errorf("%s:%d: variable block needs exactly one name label", file, b.Line)
	}
	nested, attrs, err := hclscan.Parse(b.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: variable %q: %w", file, b.Labels[0], err)
	}

	v := &Variable{Name: b.Labels[0], File: file, Nullable: true}

	rawType, _ := hclscan.AttrValue(attrs, "type")
	parsed, err := typeexpr.Parse(normalize(rawType))
	if err != nil {
		return nil, fmt.Errorf("%s: variable %q: type: %w", file, v.Name, err)
	}
	v.parsedType = parsed
	v.Type = parsed.String()

	if raw, ok := hclscan.AttrValue(attrs, "default"); ok {
		v.HasDefault = true
		v.Default = normalize(raw)
	}
	if raw, ok := hclscan.AttrValue(attrs, "nullable"); ok {
		v.Nullable = isTrue(raw)
	}
	if raw, ok := hclscan.AttrValue(attrs, "sensitive"); ok {
		v.Sensitive = isTrue(raw)
	}
	if raw, ok := hclscan.AttrValue(attrs, "description"); ok {
		v.Description = unquote(normalize(raw))
	}
	v.Validations = len(hclscan.BlocksOfType(nested, "validation"))
	return v, nil
}

func readOutput(file string, b hclscan.Block) (*Output, error) {
	if len(b.Labels) != 1 || b.Labels[0] == "" {
		return nil, fmt.Errorf("%s:%d: output block needs exactly one name label", file, b.Line)
	}
	_, attrs, err := hclscan.Parse(b.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: output %q: %w", file, b.Labels[0], err)
	}
	o := &Output{Name: b.Labels[0], File: file}
	if raw, ok := hclscan.AttrValue(attrs, "sensitive"); ok {
		o.Sensitive = isTrue(raw)
	}
	if raw, ok := hclscan.AttrValue(attrs, "description"); ok {
		o.Description = unquote(normalize(raw))
	}
	return o, nil
}

// normalize collapses the whitespace of an expression so that a reformatted
// but unchanged declaration does not read as a diff.
func normalize(expr string) string {
	return strings.Join(strings.Fields(expr), " ")
}

// isTrue treats only a literal true as true. A computed expression in
// `sensitive` or `nullable` is not something this tool evaluates.
func isTrue(raw string) bool {
	return strings.TrimSpace(raw) == "true"
}

func unquote(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s[1 : len(s)-1]
	}
	return s
}

func sortedVarKeys(m map[string]*Variable) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedOutKeys(m map[string]*Output) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
