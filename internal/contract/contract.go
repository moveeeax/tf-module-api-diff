// Package contract reads and writes a checked-in snapshot of a module's public
// API.
//
// A contract file is the old side of a comparison that does not need git
// history: the module's pipeline diffs the file against the module as it
// stands, so a renamed output fails the module's own CI instead of the next
// consumer's plan. The file is JSON, written with sorted keys and one fact per
// line, so a deliberate API change shows up as a readable diff in review.
//
// It records what a module declares and nothing it would take Terraform to
// evaluate. Output blocks have no type or nullability argument, so an output
// is its name, its sensitivity and its description.
package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/moveeeax/tf-module-api-diff/internal/modapi"
)

// Version is the contract file format this build reads and writes.
const Version = 1

type file struct {
	Contract  int                 `json:"contract"`
	Variables map[string]variable `json:"variables"`
	Outputs   map[string]output   `json:"outputs"`
}

// variable leaves Terraform's defaults implicit for hand-written files: an
// omitted type is any, an omitted default means the input is required, and an
// omitted nullable is true. Written files spell every field out.
type variable struct {
	Type        string  `json:"type,omitempty"`
	Default     *string `json:"default,omitempty"` // the HCL expression text
	Nullable    *bool   `json:"nullable,omitempty"`
	Sensitive   bool    `json:"sensitive"`
	Description string  `json:"description"`
	Validations int     `json:"validations"`
}

type output struct {
	Sensitive   bool   `json:"sensitive"`
	Description string `json:"description"`
}

// Write renders api as a contract file.
func Write(w io.Writer, api *modapi.API) error {
	f := file{
		Contract:  Version,
		Variables: map[string]variable{},
		Outputs:   map[string]output{},
	}
	for _, name := range api.VariableNames() {
		v := api.Variables[name]
		nullable := v.Nullable
		cv := variable{
			Type:        v.Type,
			Nullable:    &nullable,
			Sensitive:   v.Sensitive,
			Description: v.Description,
			Validations: v.Validations,
		}
		if v.HasDefault {
			def := v.Default
			cv.Default = &def
		}
		f.Variables[name] = cv
	}
	for _, name := range api.OutputNames() {
		o := api.Outputs[name]
		f.Outputs[name] = output{Sensitive: o.Sensitive, Description: o.Description}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Descriptions and defaults are HCL text; keep "<" and "&" readable.
	enc.SetEscapeHTML(false)
	return enc.Encode(f)
}

// Read parses a contract file into an API that compares like a scanned one.
// Unknown fields are rejected so a misspelt key cannot silently mean "absent".
func Read(r io.Reader) (*modapi.API, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("decode: unexpected data after the contract object")
	}
	switch f.Contract {
	case Version:
	case 0:
		return nil, fmt.Errorf(`missing "contract": %d`, Version)
	default:
		return nil, fmt.Errorf("contract format %d is not supported (this build reads %d)", f.Contract, Version)
	}

	api := &modapi.API{Variables: map[string]*modapi.Variable{}, Outputs: map[string]*modapi.Output{}}
	for name, cv := range f.Variables {
		if name == "" {
			return nil, fmt.Errorf("a variable has an empty name")
		}
		v := &modapi.Variable{
			Name:        name,
			Nullable:    cv.Nullable == nil || *cv.Nullable,
			Sensitive:   cv.Sensitive,
			Description: cv.Description,
			Validations: cv.Validations,
		}
		if err := v.SetType(cv.Type); err != nil {
			return nil, fmt.Errorf("variable %q: type: %w", name, err)
		}
		if cv.Default != nil {
			v.HasDefault = true
			// Same whitespace folding the scanner applies, so a hand-edited
			// default does not differ from the module by spacing alone.
			v.Default = strings.Join(strings.Fields(*cv.Default), " ")
		}
		if cv.Validations < 0 {
			return nil, fmt.Errorf("variable %q: validations cannot be negative", name)
		}
		api.Variables[name] = v
	}
	for name, co := range f.Outputs {
		if name == "" {
			return nil, fmt.Errorf("an output has an empty name")
		}
		api.Outputs[name] = &modapi.Output{Name: name, Sensitive: co.Sensitive, Description: co.Description}
	}
	return api, nil
}

// Load reads the contract file at path.
func Load(path string) (*modapi.API, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	api, err := Read(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return api, nil
}
