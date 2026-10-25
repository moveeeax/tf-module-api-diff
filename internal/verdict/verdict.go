// Package verdict turns a diff result plus the user's gates into an exit code.
//
// Two gates. --fail-on is the blunt one: block anything at or above a level.
// --expect is the honest-tag one: state the bump you are about to publish and
// the tool refuses when the code changed more than that. Declaring --expect
// supersedes --fail-on, because the declared bump already says what is
// allowed; keeping both active would reject the very major release you just
// told it about.
package verdict

import (
	"fmt"

	"github.com/moveeeax/tf-module-api-diff/internal/diff"
)

// Verdict is the decision about a result.
type Verdict struct {
	Exit   int
	Reason string // why the run failed, empty when it passed
	Note   string // advisory text that does not affect the exit code
}

// Decide evaluates failOn ("major", "minor", "patch" or "never") and expect
// ("" when unset) against the result.
func Decide(r diff.Result, failOn, expect string) (Verdict, error) {
	var v Verdict

	if expect != "" {
		want, err := diff.ParseSeverity(expect)
		if err != nil {
			return v, fmt.Errorf("--expect: %w", err)
		}
		switch {
		case r.Severity > want:
			v.Exit = 1
			v.Reason = fmt.Sprintf("the change is %s but --expect %s was declared: %s",
				r.Severity, want, firstReason(r, r.Severity))
			return v, nil
		case r.Severity < want:
			v.Note = fmt.Sprintf("the change is only %s, so the declared %s bump is larger than needed",
				r.Severity, want)
		}
	}

	if expect != "" || failOn == "never" {
		return v, nil
	}
	threshold, err := diff.ParseSeverity(failOn)
	if err != nil {
		return v, fmt.Errorf("--fail-on: %w", err)
	}
	if r.Changed() && r.Severity >= threshold {
		v.Exit = 1
		v.Reason = fmt.Sprintf("%s change exceeds --fail-on %s: %s",
			r.Severity, threshold, firstReason(r, r.Severity))
	}
	return v, nil
}

// firstReason names a representative change at the deciding severity so the
// failure message says what actually happened.
func firstReason(r diff.Result, sev diff.Severity) string {
	for _, c := range r.Changes {
		if c.Severity == sev {
			return fmt.Sprintf("%s on %s", c.Kind, c.Target)
		}
	}
	return "no changes"
}
