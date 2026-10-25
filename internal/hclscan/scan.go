// Package hclscan is a small structural scanner for HCL source.
//
// It does not evaluate anything. It walks the text tracking brace depth,
// quoted strings (including ${...} interpolation), comments and heredocs, and
// hands back the top-level blocks with their raw bodies plus the top-level
// `name = expression` attributes. That is enough to read a Terraform module's
// variable and output blocks without a terraform binary, a provider download
// or a vendored HCL parser, and a type like object({ a = string }) survives
// intact because the scanner counts structure instead of matching patterns.
package hclscan

import (
	"fmt"
	"strings"
)

// Block is a top-level HCL block: its type, labels and raw body text.
type Block struct {
	Type   string
	Labels []string
	Body   string
	Line   int
}

// Attr is a top-level `name = expression` assignment.
type Attr struct {
	Name  string
	Value string
	Line  int
}

// Parse splits src into its top-level blocks and attributes. Bodies are
// returned verbatim, so Parse can be called again on a block body to read its
// own attributes and nested blocks.
func Parse(src string) ([]Block, []Attr, error) {
	p := &parser{src: src, line: 1}
	var (
		blocks []Block
		attrs  []Attr
	)
	for {
		if err := p.skipSpace(true); err != nil {
			return nil, nil, err
		}
		if p.i >= len(p.src) {
			return blocks, attrs, nil
		}
		if p.src[p.i] == '}' {
			return nil, nil, fmt.Errorf("line %d: unexpected '}'", p.line)
		}
		line := p.line
		name := p.readIdent()
		if name == "" {
			return nil, nil, fmt.Errorf("line %d: unexpected %q", p.line, string(p.src[p.i]))
		}

		if err := p.skipSpace(false); err != nil {
			return nil, nil, err
		}
		if p.i < len(p.src) && p.src[p.i] == '=' && !p.hasAt(p.i+1, "=") {
			p.i++
			if err := p.skipSpace(false); err != nil {
				return nil, nil, err
			}
			value, err := p.readValue()
			if err != nil {
				return nil, nil, err
			}
			attrs = append(attrs, Attr{Name: name, Value: value, Line: line})
			continue
		}

		labels, err := p.readLabels(name, line)
		if err != nil {
			return nil, nil, err
		}
		body, err := p.readBody()
		if err != nil {
			return nil, nil, fmt.Errorf("block %q at line %d: %w", name, line, err)
		}
		blocks = append(blocks, Block{Type: name, Labels: labels, Body: body, Line: line})
	}
}

// BlocksOfType returns the blocks whose type matches want.
func BlocksOfType(blocks []Block, want string) []Block {
	var out []Block
	for _, b := range blocks {
		if b.Type == want {
			out = append(out, b)
		}
	}
	return out
}

// AttrValue returns the value of the named attribute and whether it was set.
// The last assignment wins, matching how a duplicate would be read last.
func AttrValue(attrs []Attr, name string) (string, bool) {
	value, ok := "", false
	for _, a := range attrs {
		if a.Name == name {
			value, ok = a.Value, true
		}
	}
	return value, ok
}

type parser struct {
	src  string
	i    int
	line int
}

func (p *parser) hasAt(i int, s string) bool {
	return i >= 0 && i+len(s) <= len(p.src) && p.src[i:i+len(s)] == s
}

// skipSpace advances past whitespace and comments. With newlines=false it
// stops at the first line break, which is how HCL terminates an attribute.
func (p *parser) skipSpace(newlines bool) error {
	for p.i < len(p.src) {
		switch c := p.src[p.i]; {
		case c == '\n':
			if !newlines {
				return nil
			}
			p.line++
			p.i++
		case c == ' ' || c == '\t' || c == '\r':
			p.i++
		case c == '#' || p.hasAt(p.i, "//"):
			for p.i < len(p.src) && p.src[p.i] != '\n' {
				p.i++
			}
		case p.hasAt(p.i, "/*"):
			if err := p.skipBlockComment(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

func (p *parser) skipBlockComment() error {
	p.i += 2
	for p.i < len(p.src) {
		if p.src[p.i] == '\n' {
			p.line++
		}
		if p.hasAt(p.i, "*/") {
			p.i += 2
			return nil
		}
		p.i++
	}
	return fmt.Errorf("line %d: unterminated block comment", p.line)
}

func (p *parser) readIdent() string {
	start := p.i
	for p.i < len(p.src) && isIdentChar(p.src[p.i]) {
		p.i++
	}
	return p.src[start:p.i]
}

func (p *parser) readLabels(blockType string, line int) ([]string, error) {
	var labels []string
	for {
		if err := p.skipSpace(true); err != nil {
			return nil, err
		}
		if p.i >= len(p.src) {
			return nil, fmt.Errorf("line %d: block %q has no body", line, blockType)
		}
		switch c := p.src[p.i]; {
		case c == '{':
			p.i++
			return labels, nil
		case c == '"':
			start := p.i
			if err := p.skipString(); err != nil {
				return nil, err
			}
			labels = append(labels, strings.Trim(p.src[start:p.i], `"`))
		case isIdentChar(c):
			labels = append(labels, p.readIdent())
		default:
			return nil, fmt.Errorf("line %d: unexpected %q in header of block %q", p.line, string(c), blockType)
		}
	}
}

// readBody consumes a block body, assuming the opening brace is already gone,
// and returns the text up to the matching close brace.
func (p *parser) readBody() (string, error) {
	start := p.i
	depth := 1
	for p.i < len(p.src) {
		switch c := p.src[p.i]; {
		case c == '"':
			if err := p.skipString(); err != nil {
				return "", err
			}
		case c == '<':
			ok, err := p.skipHeredoc()
			if err != nil {
				return "", err
			}
			if !ok {
				p.i++
			}
		case c == '#' || p.hasAt(p.i, "//"):
			for p.i < len(p.src) && p.src[p.i] != '\n' {
				p.i++
			}
		case p.hasAt(p.i, "/*"):
			if err := p.skipBlockComment(); err != nil {
				return "", err
			}
		case c == '{':
			depth++
			p.i++
		case c == '}':
			depth--
			if depth == 0 {
				body := p.src[start:p.i]
				p.i++
				return body, nil
			}
			p.i++
		case c == '\n':
			p.line++
			p.i++
		default:
			p.i++
		}
	}
	return "", fmt.Errorf("unterminated body")
}

// readValue consumes an expression. It ends at the first newline reached
// outside any bracket, string or heredoc, which is HCL's attribute terminator.
//
// Comments inside the expression are dropped rather than carried along: a
// comment sitting between two object-type attributes is not part of the type,
// and leaving it in the captured text makes the expression unparseable and a
// reworded comment look like an API change.
func (p *parser) readValue() (string, error) {
	var (
		out      strings.Builder
		segStart = p.i
		depth    = 0
	)
	flush := func() {
		out.WriteString(p.src[segStart:p.i])
	}
	done := func() (string, error) {
		flush()
		return strings.TrimSpace(out.String()), nil
	}

	for p.i < len(p.src) {
		switch c := p.src[p.i]; {
		case c == '"':
			if err := p.skipString(); err != nil {
				return "", err
			}
		case c == '<':
			ok, err := p.skipHeredoc()
			if err != nil {
				return "", err
			}
			if !ok {
				p.i++
			}
		case c == '#' || p.hasAt(p.i, "//"):
			if depth == 0 {
				return done()
			}
			flush()
			for p.i < len(p.src) && p.src[p.i] != '\n' {
				p.i++
			}
			segStart = p.i
		case p.hasAt(p.i, "/*"):
			flush()
			if err := p.skipBlockComment(); err != nil {
				return "", err
			}
			segStart = p.i
		case c == '(' || c == '[' || c == '{':
			depth++
			p.i++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				// A close brace belonging to the enclosing block ends the value.
				return done()
			}
			depth--
			p.i++
		case c == '\n':
			if depth == 0 {
				return done()
			}
			p.line++
			p.i++
		default:
			p.i++
		}
	}
	return done()
}

func (p *parser) skipString() error {
	p.i++ // opening quote
	for p.i < len(p.src) {
		switch c := p.src[p.i]; {
		case c == '\\':
			p.i += 2
		case c == '"':
			p.i++
			return nil
		case c == '\n':
			return fmt.Errorf("line %d: unterminated string", p.line)
		case (c == '$' || c == '%') && p.hasAt(p.i+1, "{"):
			p.i += 2
			if err := p.skipInterp(); err != nil {
				return err
			}
		default:
			p.i++
		}
	}
	return fmt.Errorf("line %d: unterminated string", p.line)
}

// skipInterp consumes a ${...} or %{...} body, which may itself contain
// strings and braces.
func (p *parser) skipInterp() error {
	depth := 1
	for p.i < len(p.src) {
		switch c := p.src[p.i]; {
		case c == '"':
			if err := p.skipString(); err != nil {
				return err
			}
		case c == '{':
			depth++
			p.i++
		case c == '}':
			depth--
			p.i++
			if depth == 0 {
				return nil
			}
		case c == '\n':
			p.line++
			p.i++
		default:
			p.i++
		}
	}
	return fmt.Errorf("line %d: unterminated interpolation", p.line)
}

// skipHeredoc consumes a <<TAG / <<-TAG body when one starts at the cursor.
// It reports false when the '<' is not a heredoc opener.
func (p *parser) skipHeredoc() (bool, error) {
	if !p.hasAt(p.i, "<<") {
		return false, nil
	}
	j := p.i + 2
	if j < len(p.src) && p.src[j] == '-' {
		j++
	}
	k := j
	for k < len(p.src) && isIdentChar(p.src[k]) {
		k++
	}
	if k == j {
		return false, nil
	}
	tag := p.src[j:k]
	nl := strings.IndexByte(p.src[k:], '\n')
	if nl < 0 {
		return false, fmt.Errorf("line %d: unterminated heredoc %s", p.line, tag)
	}
	pos := k + nl + 1
	p.line++
	for pos <= len(p.src) {
		end := strings.IndexByte(p.src[pos:], '\n')
		text := p.src[pos:]
		if end >= 0 {
			text = p.src[pos : pos+end]
		}
		if strings.TrimSpace(text) == tag {
			if end < 0 {
				p.i = len(p.src)
			} else {
				p.i = pos + end // leave the newline for the caller
			}
			return true, nil
		}
		if end < 0 {
			break
		}
		pos += end + 1
		p.line++
	}
	return false, fmt.Errorf("unterminated heredoc %s", tag)
}

func isIdentChar(c byte) bool {
	return c == '_' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
