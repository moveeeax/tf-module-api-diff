// Package typeexpr parses Terraform type constraint expressions and compares
// two of them for caller compatibility.
//
// The question it answers is not "are these types equal" but "can every value
// a caller could legally pass before still be passed now". Widening the
// constraint (string -> any, adding an optional object attribute) keeps old
// calls valid. Narrowing it (any -> string, adding a required object
// attribute, dropping one) breaks them, and that is the signal the severity
// classifier needs.
package typeexpr

import (
	"fmt"
	"sort"
	"strings"
)

// Kind distinguishes the shapes a type constraint can take.
type Kind int

const (
	KindAny Kind = iota
	KindPrimitive
	KindCollection // list, set, map
	KindObject
	KindTuple
)

// Type is a parsed type constraint.
type Type struct {
	Kind  Kind
	Name  string // primitive name for KindPrimitive, collection name for KindCollection
	Elem  *Type  // element type for KindCollection
	Attr  map[string]ObjAttr
	Keys  []string // object attribute names in source order
	Elems []*Type  // element types for KindTuple
}

// ObjAttr is one attribute of an object type constraint.
type ObjAttr struct {
	Type     *Type
	Optional bool
}

// Relation is how a new constraint relates to an old one.
type Relation int

const (
	// Same: the constraints accept the same values.
	Same Relation = iota
	// Wider: everything valid before is still valid, and more besides.
	Wider
	// Narrower: at least one previously valid call is now rejected.
	Narrower
)

func (r Relation) String() string {
	switch r {
	case Wider:
		return "wider"
	case Narrower:
		return "narrower"
	default:
		return "same"
	}
}

// Any is the constraint an omitted `type` argument implies.
func Any() *Type { return &Type{Kind: KindAny, Name: "any"} }

var primitives = map[string]bool{"string": true, "number": true, "bool": true}

// Parse reads a type constraint expression such as
// `object({ name = string, tags = optional(map(string)) })`.
func Parse(expr string) (*Type, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return Any(), nil
	}
	p := &parser{src: trimmed}
	t, err := p.parseType()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.i < len(p.src) {
		return nil, fmt.Errorf("unexpected %q after type", p.src[p.i:])
	}
	return t, nil
}

// String renders a type back to a canonical expression, used in diff output.
func (t *Type) String() string {
	if t == nil {
		return "any"
	}
	switch t.Kind {
	case KindAny:
		return "any"
	case KindPrimitive:
		return t.Name
	case KindCollection:
		return t.Name + "(" + t.Elem.String() + ")"
	case KindTuple:
		parts := make([]string, 0, len(t.Elems))
		for _, e := range t.Elems {
			parts = append(parts, e.String())
		}
		return "tuple([" + strings.Join(parts, ", ") + "])"
	case KindObject:
		parts := make([]string, 0, len(t.Keys))
		for _, k := range t.Keys {
			a := t.Attr[k]
			rendered := a.Type.String()
			if a.Optional {
				rendered = "optional(" + rendered + ")"
			}
			parts = append(parts, k+" = "+rendered)
		}
		return "object({ " + strings.Join(parts, ", ") + " })"
	}
	return "any"
}

// Compare reports how the new constraint relates to the old one.
func Compare(oldT, newT *Type) Relation {
	if oldT == nil {
		oldT = Any()
	}
	if newT == nil {
		newT = Any()
	}
	switch {
	case oldT.Kind == KindAny && newT.Kind == KindAny:
		return Same
	case newT.Kind == KindAny:
		// Anything the old constraint accepted, `any` accepts too.
		return Wider
	case oldT.Kind == KindAny:
		return Narrower
	case oldT.Kind != newT.Kind:
		return Narrower
	}

	switch oldT.Kind {
	case KindPrimitive:
		if oldT.Name == newT.Name {
			return Same
		}
		return Narrower
	case KindCollection:
		if oldT.Name != newT.Name {
			// list -> set loses ordering, set -> list loses dedup; either way a
			// caller's value may no longer round-trip.
			return Narrower
		}
		return Compare(oldT.Elem, newT.Elem)
	case KindTuple:
		if len(oldT.Elems) != len(newT.Elems) {
			return Narrower
		}
		worst := Same
		for i := range oldT.Elems {
			worst = worse(worst, Compare(oldT.Elems[i], newT.Elems[i]))
		}
		return worst
	case KindObject:
		return compareObjects(oldT, newT)
	}
	return Narrower
}

func compareObjects(oldT, newT *Type) Relation {
	worst := Same
	for _, k := range sortedKeys(oldT.Attr) {
		oldAttr := oldT.Attr[k]
		newAttr, ok := newT.Attr[k]
		if !ok {
			// A removed attribute rejects every object that still carries it.
			return Narrower
		}
		if oldAttr.Optional && !newAttr.Optional {
			return Narrower
		}
		if !oldAttr.Optional && newAttr.Optional {
			worst = worse(worst, Wider)
		}
		worst = worse(worst, Compare(oldAttr.Type, newAttr.Type))
	}
	for _, k := range sortedKeys(newT.Attr) {
		if _, ok := oldT.Attr[k]; ok {
			continue
		}
		if newT.Attr[k].Optional {
			worst = worse(worst, Wider)
			continue
		}
		return Narrower
	}
	return worst
}

func worse(a, b Relation) Relation {
	if b == Narrower || a == Narrower {
		return Narrower
	}
	if b == Wider || a == Wider {
		return Wider
	}
	return Same
}

func sortedKeys(m map[string]ObjAttr) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type parser struct {
	src string
	i   int
}

func (p *parser) ws() {
	for p.i < len(p.src) {
		switch p.src[p.i] {
		case ' ', '\t', '\n', '\r', ',':
			p.i++
		default:
			return
		}
	}
}

// wsNoComma skips whitespace but keeps a comma as a separator token.
func (p *parser) wsNoComma() {
	for p.i < len(p.src) {
		switch p.src[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) ident() string {
	start := p.i
	for p.i < len(p.src) {
		c := p.src[p.i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			p.i++
			continue
		}
		break
	}
	return p.src[start:p.i]
}

func (p *parser) expect(c byte) error {
	p.wsNoComma()
	if p.i >= len(p.src) || p.src[p.i] != c {
		return fmt.Errorf("expected %q at offset %d in %q", string(c), p.i, p.src)
	}
	p.i++
	return nil
}

func (p *parser) parseType() (*Type, error) {
	p.wsNoComma()
	name := p.ident()
	if name == "" {
		return nil, fmt.Errorf("expected a type name at offset %d in %q", p.i, p.src)
	}
	switch {
	case name == "any":
		return Any(), nil
	case primitives[name]:
		return &Type{Kind: KindPrimitive, Name: name}, nil
	case name == "list" || name == "set" || name == "map":
		if err := p.expect('('); err != nil {
			return nil, err
		}
		elem, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return &Type{Kind: KindCollection, Name: name, Elem: elem}, nil
	case name == "object":
		return p.parseObject()
	case name == "tuple":
		return p.parseTuple()
	}
	return nil, fmt.Errorf("unknown type %q", name)
}

func (p *parser) parseObject() (*Type, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	t := &Type{Kind: KindObject, Attr: map[string]ObjAttr{}}
	for {
		p.ws()
		if p.i >= len(p.src) {
			return nil, fmt.Errorf("unterminated object type in %q", p.src)
		}
		if p.src[p.i] == '}' {
			p.i++
			break
		}
		key := p.ident()
		if key == "" {
			if p.src[p.i] == '"' {
				quoted, err := p.quoted()
				if err != nil {
					return nil, err
				}
				key = quoted
			} else {
				return nil, fmt.Errorf("expected an attribute name at offset %d in %q", p.i, p.src)
			}
		}
		if err := p.expect('='); err != nil {
			return nil, err
		}
		attr, err := p.parseAttrType()
		if err != nil {
			return nil, err
		}
		if _, dup := t.Attr[key]; !dup {
			t.Keys = append(t.Keys, key)
		}
		t.Attr[key] = attr
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return t, nil
}

// parseAttrType reads an object attribute type, unwrapping optional(T) and
// optional(T, default) into an ObjAttr.
func (p *parser) parseAttrType() (ObjAttr, error) {
	p.wsNoComma()
	save := p.i
	if p.ident() == "optional" {
		if err := p.expect('('); err != nil {
			return ObjAttr{}, err
		}
		inner, err := p.parseType()
		if err != nil {
			return ObjAttr{}, err
		}
		// An optional() may carry a default expression; skip it structurally.
		p.wsNoComma()
		if p.i < len(p.src) && p.src[p.i] == ',' {
			p.i++
			if err := p.skipBalanced(); err != nil {
				return ObjAttr{}, err
			}
		}
		if err := p.expect(')'); err != nil {
			return ObjAttr{}, err
		}
		return ObjAttr{Type: inner, Optional: true}, nil
	}
	p.i = save
	t, err := p.parseType()
	if err != nil {
		return ObjAttr{}, err
	}
	return ObjAttr{Type: t}, nil
}

// skipBalanced consumes a default expression up to the close paren that ends
// the enclosing optional().
func (p *parser) skipBalanced() error {
	depth := 0
	for p.i < len(p.src) {
		switch c := p.src[p.i]; c {
		case '"':
			if _, err := p.quoted(); err != nil {
				return err
			}
			continue
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 && c == ')' {
				return nil
			}
			depth--
		}
		p.i++
	}
	return fmt.Errorf("unterminated default expression in %q", p.src)
}

func (p *parser) parseTuple() (*Type, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	if err := p.expect('['); err != nil {
		return nil, err
	}
	t := &Type{Kind: KindTuple}
	for {
		p.ws()
		if p.i >= len(p.src) {
			return nil, fmt.Errorf("unterminated tuple type in %q", p.src)
		}
		if p.src[p.i] == ']' {
			p.i++
			break
		}
		elem, err := p.parseType()
		if err != nil {
			return nil, err
		}
		t.Elems = append(t.Elems, elem)
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return t, nil
}

func (p *parser) quoted() (string, error) {
	p.i++ // opening quote
	start := p.i
	for p.i < len(p.src) {
		if p.src[p.i] == '\\' {
			p.i += 2
			continue
		}
		if p.src[p.i] == '"' {
			s := p.src[start:p.i]
			p.i++
			return s, nil
		}
		p.i++
	}
	return "", fmt.Errorf("unterminated string in %q", p.src)
}
