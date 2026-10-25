package typeexpr

import "testing"

func TestParseAndRender(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "any"},
		{"  ", "any"},
		{"string", "string"},
		{"list(string)", "list(string)"},
		{"map( object( { a = string } ) )", "map(object({ a = string }))"},
		{"tuple([string, number])", "tuple([string, number])"},
		{"object({ b = number, a = optional(string) })", "object({ b = number, a = optional(string) })"},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		if got.String() != c.want {
			t.Errorf("Parse(%q).String() = %q, want %q", c.in, got.String(), c.want)
		}
	}
}

func TestParseOptionalWithDefault(t *testing.T) {
	ty, err := Parse(`object({ name = string, size = optional(number, 3), tags = optional(map(string)) })`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ty.Kind != KindObject {
		t.Fatalf("kind = %v", ty.Kind)
	}
	if ty.Attr["name"].Optional {
		t.Error("name should be required")
	}
	if !ty.Attr["size"].Optional || ty.Attr["size"].Type.Name != "number" {
		t.Errorf("size = %+v", ty.Attr["size"])
	}
	if !ty.Attr["tags"].Optional || ty.Attr["tags"].Type.String() != "map(string)" {
		t.Errorf("tags = %+v", ty.Attr["tags"])
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, in := range []string{"strung", "list(", "list(string))", "object(string)", "object({ a })"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) should have failed", in)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		name     string
		old, new string
		want     Relation
	}{
		{"identical", "string", "string", Same},
		{"primitive swap", "string", "number", Narrower},
		{"widen to any", "string", "any", Wider},
		{"narrow from any", "", "string", Narrower},
		{"omitted both sides", "", "", Same},
		{"element narrowed", "list(any)", "list(string)", Narrower},
		{"element widened", "list(string)", "list(any)", Wider},
		{"list to set", "list(string)", "set(string)", Narrower},
		{"collection to primitive", "list(string)", "string", Narrower},
		{"object attr added optional", "object({ a = string })", "object({ a = string, b = optional(number) })", Wider},
		{"object attr added required", "object({ a = string })", "object({ a = string, b = number })", Narrower},
		{"object attr removed", "object({ a = string, b = number })", "object({ a = string })", Narrower},
		{"object attr became optional", "object({ a = string })", "object({ a = optional(string) })", Wider},
		{"object attr became required", "object({ a = optional(string) })", "object({ a = string })", Narrower},
		{"object attr retyped", "object({ a = string })", "object({ a = bool })", Narrower},
		{"tuple length changed", "tuple([string])", "tuple([string, string])", Narrower},
		{"tuple same", "tuple([string, number])", "tuple([string, number])", Same},
	}
	for _, c := range cases {
		oldT, err := Parse(c.old)
		if err != nil {
			t.Fatalf("%s: Parse(%q): %v", c.name, c.old, err)
		}
		newT, err := Parse(c.new)
		if err != nil {
			t.Fatalf("%s: Parse(%q): %v", c.name, c.new, err)
		}
		if got := Compare(oldT, newT); got != c.want {
			t.Errorf("%s: Compare(%q, %q) = %v, want %v", c.name, c.old, c.new, got, c.want)
		}
	}
}

func TestCompareIgnoresAttributeOrder(t *testing.T) {
	oldT, _ := Parse("object({ a = string, b = number })")
	newT, _ := Parse("object({ b = number, a = string })")
	if got := Compare(oldT, newT); got != Same {
		t.Errorf("reordered attributes = %v, want same", got)
	}
}
