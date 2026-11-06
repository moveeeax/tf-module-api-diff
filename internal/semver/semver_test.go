package semver

import (
	"testing"

	"github.com/moveeeax/tf-module-api-diff/internal/diff"
)

func TestParseAcceptsPrefixAndOptionalV(t *testing.T) {
	cases := []struct {
		tag, prefix string
		want        Version
	}{
		{"v1.2.3", "", Version{V: true, Major: 1, Minor: 2, Patch: 3}},
		{"1.2.3", "", Version{Major: 1, Minor: 2, Patch: 3}},
		{"vpc/v1.2.3", "vpc/", Version{Prefix: "vpc/", V: true, Major: 1, Minor: 2, Patch: 3}},
		{"modules/vpc-10.0.12", "modules/vpc-", Version{Prefix: "modules/vpc-", Major: 10, Patch: 12}},
	}
	for _, c := range cases {
		got, err := Parse(c.tag, c.prefix)
		if err != nil {
			t.Errorf("Parse(%q, %q): %v", c.tag, c.prefix, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q, %q) = %+v, want %+v", c.tag, c.prefix, got, c.want)
		}
		if got.String() != c.tag {
			t.Errorf("String() = %q, want %q", got.String(), c.tag)
		}
	}
}

func TestParseRejectsNonReleases(t *testing.T) {
	cases := []struct{ tag, prefix string }{
		{"vpc/v1.2.3", ""},
		{"v1.2.3", "vpc/"},
		{"v1.2", ""},
		{"v1.2.3-rc.1", ""},
		{"v1.2.3+build", ""},
		{"v01.2.3", ""},
		{"main", ""},
		{"v", ""},
	}
	for _, c := range cases {
		if v, err := Parse(c.tag, c.prefix); err == nil {
			t.Errorf("Parse(%q, %q) = %+v, want an error", c.tag, c.prefix, v)
		}
	}
}

func TestBumpAppliesTheSeverity(t *testing.T) {
	v, _ := Parse("vpc/v1.2.3", "vpc/")
	cases := map[diff.Severity]string{
		diff.Major: "vpc/v2.0.0",
		diff.Minor: "vpc/v1.3.0",
		diff.Patch: "vpc/v1.2.4",
	}
	for sev, want := range cases {
		if got := v.Bump(sev).String(); got != want {
			t.Errorf("Bump(%s) = %s, want %s", sev, got, want)
		}
	}
	zero, _ := Parse("0.4.1", "")
	if got := zero.Bump(diff.Major).String(); got != "1.0.0" {
		t.Errorf("0.4.1 major bump = %s, want 1.0.0", got)
	}
}

func TestHighestComparesNumbersNotStrings(t *testing.T) {
	tags := []string{"vpc/v1.9.0", "vpc/v1.10.0", "vpc/v2.0.0-rc.1", "eks/v3.0.0", "vpc/v1.2.3", "notes"}
	v, tag, ok := Highest(tags, "vpc/")
	if !ok || tag != "vpc/v1.10.0" || v.Minor != 10 {
		t.Errorf("Highest = %+v %q %v, want vpc/v1.10.0", v, tag, ok)
	}
	if _, _, ok := Highest([]string{"eks/v1.0.0"}, "vpc/"); ok {
		t.Error("no tag under the prefix should report ok = false")
	}
}
