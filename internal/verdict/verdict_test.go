package verdict

import (
	"strings"
	"testing"

	"github.com/moveeeax/tf-module-api-diff/internal/diff"
)

func result(sev diff.Severity, changed bool) diff.Result {
	r := diff.Result{Severity: sev, Level: sev.String()}
	if changed {
		r.Changes = []diff.Change{{
			Severity: sev, Level: sev.String(), Kind: "VARIABLE_REMOVED", Target: "var.gone",
		}}
	}
	return r
}

func TestFailOnThreshold(t *testing.T) {
	cases := []struct {
		failOn string
		sev    diff.Severity
		want   int
	}{
		{"major", diff.Major, 1},
		{"major", diff.Minor, 0},
		{"major", diff.Patch, 0},
		{"minor", diff.Major, 1},
		{"minor", diff.Minor, 1},
		{"minor", diff.Patch, 0},
		{"patch", diff.Patch, 1},
		{"never", diff.Major, 0},
	}
	for _, c := range cases {
		v, err := Decide(result(c.sev, true), c.failOn, "")
		if err != nil {
			t.Fatalf("--fail-on %s: %v", c.failOn, err)
		}
		if v.Exit != c.want {
			t.Errorf("--fail-on %s with a %s change: exit %d, want %d", c.failOn, c.sev, v.Exit, c.want)
		}
	}
}

func TestFailOnPatchIgnoresAnEmptyDiff(t *testing.T) {
	v, err := Decide(result(diff.Patch, false), "patch", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Exit != 0 {
		t.Errorf("exit %d (%s), want 0 when nothing changed", v.Exit, v.Reason)
	}
}

func TestExpectRejectsAnUnderstatedBump(t *testing.T) {
	v, err := Decide(result(diff.Minor, true), "major", "patch")
	if err != nil {
		t.Fatal(err)
	}
	if v.Exit != 1 {
		t.Fatalf("exit %d, want 1", v.Exit)
	}
	if !strings.Contains(v.Reason, "minor") || !strings.Contains(v.Reason, "patch") {
		t.Errorf("reason = %q", v.Reason)
	}
}

func TestExpectAllowsAnOverstatedBump(t *testing.T) {
	v, err := Decide(result(diff.Patch, true), "never", "major")
	if err != nil {
		t.Fatal(err)
	}
	if v.Exit != 0 {
		t.Errorf("exit %d, want 0", v.Exit)
	}
	if v.Note == "" {
		t.Error("expected an advisory note about the oversized bump")
	}
}

func TestExpectMatchingSeverityPasses(t *testing.T) {
	v, err := Decide(result(diff.Major, true), "major", "major")
	if err != nil {
		t.Fatal(err)
	}
	if v.Exit != 0 {
		t.Errorf("exit %d (%s), want 0: a declared major may contain a major change", v.Exit, v.Reason)
	}
}

func TestDecideRejectsUnknownWords(t *testing.T) {
	if _, err := Decide(result(diff.Patch, false), "breaking", ""); err == nil {
		t.Error("--fail-on breaking should fail")
	}
	if _, err := Decide(result(diff.Patch, false), "major", "huge"); err == nil {
		t.Error("--expect huge should fail")
	}
}
