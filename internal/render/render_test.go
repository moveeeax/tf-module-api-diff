package render

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/moveeeax/tf-module-api-diff/internal/diff"
)

type parsedSuites struct {
	Tests    int `xml:"tests,attr"`
	Failures int `xml:"failures,attr"`
	Suites   []struct {
		Name     string `xml:"name,attr"`
		Tests    int    `xml:"tests,attr"`
		Failures int    `xml:"failures,attr"`
		Props    []struct {
			Name  string `xml:"name,attr"`
			Value string `xml:"value,attr"`
		} `xml:"properties>property"`
		Cases []struct {
			Classname string `xml:"classname,attr"`
			Name      string `xml:"name,attr"`
			Failure   *struct {
				Message string `xml:"message,attr"`
				Type    string `xml:"type,attr"`
			} `xml:"failure"`
			SystemOut string `xml:"system-out"`
		} `xml:"testcase"`
	} `xml:"testsuite"`
}

func junitOf(t *testing.T, r diff.Result, rejects func(diff.Change) bool) parsedSuites {
	t.Helper()
	var buf bytes.Buffer
	if err := JUnit(&buf, r, rejects); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "<?xml") {
		t.Errorf("missing XML header:\n%s", buf.String())
	}
	var got parsedSuites
	if err := xml.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	if len(got.Suites) != 1 {
		t.Fatalf("suites = %d, want 1", len(got.Suites))
	}
	return got
}

func TestJUnitFailsOnlyRejectedChanges(t *testing.T) {
	r := diff.Result{
		From: "api.contract.json", To: "working tree", Dir: "modules/vpc",
		Severity: diff.Major, Level: "major",
		Changes: []diff.Change{
			{Severity: diff.Major, Level: "major", Kind: "OUTPUT_REMOVED", Target: "output.vpc_id", Detail: "gone <now>"},
			{Severity: diff.Minor, Level: "minor", Kind: "OUTPUT_ADDED", Target: "output.id", Detail: "new output"},
		},
	}
	got := junitOf(t, r, func(c diff.Change) bool { return c.Severity == diff.Major })

	s := got.Suites[0]
	if got.Tests != 2 || got.Failures != 1 || s.Tests != 2 || s.Failures != 1 {
		t.Errorf("counts: suites %d/%d, suite %d/%d", got.Tests, got.Failures, s.Tests, s.Failures)
	}
	if !strings.HasPrefix(s.Name, "modules/vpc: ") {
		t.Errorf("suite name = %q", s.Name)
	}
	removed, added := s.Cases[0], s.Cases[1]
	if removed.Name != "output.vpc_id OUTPUT_REMOVED" || removed.Classname != "tf-module-api-diff.modules/vpc" {
		t.Errorf("first case = %+v", removed)
	}
	if removed.Failure == nil || removed.Failure.Type != "OUTPUT_REMOVED" || !strings.Contains(removed.Failure.Message, "gone <now>") {
		t.Errorf("rejected change should fail with its detail: %+v", removed.Failure)
	}
	if added.Failure != nil || added.SystemOut != "minor: new output" {
		t.Errorf("allowed change should pass with its detail: %+v", added)
	}
	props := map[string]string{}
	for _, p := range s.Props {
		props[p.Name] = p.Value
	}
	if props["severity"] != "major" || props["from"] != "api.contract.json" {
		t.Errorf("properties = %v", props)
	}
}

func TestJUnitUnchangedAPIIsOnePassingCase(t *testing.T) {
	r := diff.Result{From: "v1.0.0", To: "HEAD", Dir: ".", Severity: diff.Patch, Level: "patch"}
	got := junitOf(t, r, func(diff.Change) bool { return true })
	s := got.Suites[0]
	if s.Tests != 1 || s.Failures != 0 || s.Cases[0].Name != "public API unchanged" || s.Cases[0].Failure != nil {
		t.Errorf("suite = %+v", s)
	}
	if s.Cases[0].Classname != "tf-module-api-diff" {
		t.Errorf("classname = %q", s.Cases[0].Classname)
	}
}
