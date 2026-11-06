// Package semver reads release tags and computes the next one.
//
// A tag is a prefix, an optional "v" and MAJOR.MINOR.PATCH. The prefix is
// whatever the caller says it is (empty for a single-module repo, "vpc/" for a
// monorepo that tags each module separately), so "vpc/v1.2.3" and "1.2.3" are
// both valid tags under the right prefix. Pre-release and build suffixes are
// not releases a gate should compare against, so they do not parse.
package semver

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/moveeeax/tf-module-api-diff/internal/diff"
)

// Version is one parsed release tag.
type Version struct {
	Prefix string // the tag prefix, as given by the caller
	V      bool   // whether the number carried a leading "v"
	Major  int
	Minor  int
	Patch  int
}

// Parse reads tag under prefix. It fails when the tag does not start with the
// prefix or the rest is not a plain release number.
func Parse(tag, prefix string) (Version, error) {
	if !strings.HasPrefix(tag, prefix) {
		return Version{}, fmt.Errorf("tag %q does not start with prefix %q", tag, prefix)
	}
	rest := strings.TrimPrefix(tag, prefix)
	v := Version{Prefix: prefix}
	if strings.HasPrefix(rest, "v") {
		v.V = true
		rest = rest[1:]
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("tag %q is not %sMAJOR.MINOR.PATCH", tag, prefix)
	}
	nums := [3]int{}
	for i, p := range parts {
		n, err := number(p)
		if err != nil {
			return Version{}, fmt.Errorf("tag %q is not %sMAJOR.MINOR.PATCH: %w", tag, prefix, err)
		}
		nums[i] = n
	}
	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
	return v, nil
}

// number accepts a semver numeric identifier: digits only, no leading zero.
func number(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty number")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%q is not a number", s)
		}
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("%q has a leading zero", s)
	}
	return strconv.Atoi(s)
}

// String renders the version back as a tag, keeping the prefix and the "v".
func (v Version) String() string {
	s := v.Prefix
	if v.V {
		s += "v"
	}
	return fmt.Sprintf("%s%d.%d.%d", s, v.Major, v.Minor, v.Patch)
}

// Less orders versions by number, ignoring the prefix and the "v".
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// Bump returns the next release after applying sev. A major bump on 0.x still
// goes to 1.0.0: the tool reports what callers see, and a breaking change is
// breaking whatever the leading digit.
func (v Version) Bump(sev diff.Severity) Version {
	n := v
	switch sev {
	case diff.Major:
		n.Major, n.Minor, n.Patch = v.Major+1, 0, 0
	case diff.Minor:
		n.Minor, n.Patch = v.Minor+1, 0
	default:
		n.Patch = v.Patch + 1
	}
	return n
}

// Highest picks the highest release among tags under prefix, skipping tags
// that do not parse. ok is false when none does.
func Highest(tags []string, prefix string) (best Version, tag string, ok bool) {
	for _, t := range tags {
		v, err := Parse(t, prefix)
		if err != nil {
			continue
		}
		if !ok || best.Less(v) {
			best, tag, ok = v, t, true
		}
	}
	return best, tag, ok
}
