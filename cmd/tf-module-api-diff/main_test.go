package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const examples = "../../examples"

func dirs(from, to string) options {
	return options{
		dir:     ".",
		fromDir: filepath.Join(examples, from),
		toDir:   filepath.Join(examples, to),
		format:  "table",
		failOn:  "major",
	}
}

func runOut(t *testing.T, opts options) (int, string, error) {
	t.Helper()
	var buf bytes.Buffer
	code, err := run(opts, &buf)
	return code, buf.String(), err
}

func TestExampleRevisionsClassifyAsDocumented(t *testing.T) {
	cases := []struct {
		from, to string
		severity string
		exit     int
	}{
		{"vpc-v1.0.0", "vpc-v1.0.0", "patch", 0},
		{"vpc-v1.0.0", "vpc-v1.1.0", "minor", 0},
		{"vpc-v1.1.0", "vpc-v1.1.1", "patch", 0},
		{"vpc-v1.1.0", "vpc-v2.0.0", "major", 1},
	}
	for _, c := range cases {
		code, out, err := runOut(t, dirs(c.from, c.to))
		if code != c.exit {
			t.Errorf("%s -> %s: exit %d, want %d (%v)", c.from, c.to, code, c.exit, err)
		}
		if !strings.Contains(out, "severity: "+c.severity) {
			t.Errorf("%s -> %s: output missing %q severity:\n%s", c.from, c.to, c.severity, out)
		}
	}
}

func TestIdenticalRevisionSaysNothingChanged(t *testing.T) {
	_, out, _ := runOut(t, dirs("vpc-v1.0.0", "vpc-v1.0.0"))
	if !strings.Contains(out, "no public API changes") {
		t.Errorf("output:\n%s", out)
	}
}

func TestMajorRevisionListsTheBreakingChanges(t *testing.T) {
	opts := dirs("vpc-v1.1.0", "vpc-v2.0.0")
	opts.format = "json"
	code, out, err := runOut(t, opts)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (%v)", code, err)
	}

	var got struct {
		Severity string `json:"severity"`
		Changes  []struct {
			Severity string `json:"severity"`
			Kind     string `json:"kind"`
			Target   string `json:"target"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode json: %v\n%s", err, out)
	}
	if got.Severity != "major" {
		t.Errorf("severity = %q", got.Severity)
	}

	want := map[string]string{
		"VARIABLE_REMOVED":         "var.enable_nat",
		"VARIABLE_BECAME_REQUIRED": "var.cidr_block",
		"VARIABLE_TYPE_NARROWED":   "var.flow_log_destination",
		"OUTPUT_SENSITIVE_ADDED":   "output.flow_log_role_arn",
	}
	seen := map[string]string{}
	for _, c := range got.Changes {
		seen[c.Kind] = c.Target
	}
	for kind, target := range want {
		if seen[kind] != target {
			t.Errorf("%s: target %q, want %q", kind, seen[kind], target)
		}
	}
	if got.Changes[0].Severity != "major" {
		t.Errorf("first change = %+v, want a major first", got.Changes[0])
	}
}

func TestExpectRejectsAnUnderstatedBump(t *testing.T) {
	opts := dirs("vpc-v1.0.0", "vpc-v1.1.0")
	opts.expect = "patch"
	code, _, err := runOut(t, opts)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if err == nil || !strings.Contains(err.Error(), "--expect patch") {
		t.Errorf("error = %v", err)
	}
}

func TestFailOnNeverAcceptsAMajor(t *testing.T) {
	opts := dirs("vpc-v1.1.0", "vpc-v2.0.0")
	opts.failOn = "never"
	if code, _, err := runOut(t, opts); code != 0 {
		t.Errorf("exit %d, want 0 (%v)", code, err)
	}
}

func TestFailOnMinorCatchesAMinor(t *testing.T) {
	opts := dirs("vpc-v1.0.0", "vpc-v1.1.0")
	opts.failOn = "minor"
	if code, _, _ := runOut(t, opts); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestMarkdownFormatRendersATable(t *testing.T) {
	opts := dirs("vpc-v1.0.0", "vpc-v1.1.0")
	opts.format = "markdown"
	_, out, _ := runOut(t, opts)
	for _, want := range []string{"### Module API diff", "**Severity: minor**", "| Severity | Change |"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	cases := map[string]options{
		"nothing to compare": {dir: ".", format: "table", failOn: "major"},
		"half a dir pair":    {dir: ".", fromDir: examples, format: "table", failOn: "major"},
		"mixed selectors": {
			dir: ".", from: "HEAD", fromDir: examples, toDir: examples,
			format: "table", failOn: "major",
		},
		"unknown format": func() options {
			o := dirs("vpc-v1.0.0", "vpc-v1.0.0")
			o.format = "yaml"
			return o
		}(),
		"unknown fail-on": func() options {
			o := dirs("vpc-v1.0.0", "vpc-v1.1.0")
			o.failOn = "breaking"
			return o
		}(),
	}
	for name, opts := range cases {
		code, _, err := runOut(t, opts)
		if code != exitError {
			t.Errorf("%s: exit %d, want %d", name, code, exitError)
		}
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestUnparseableModuleExitsTwo(t *testing.T) {
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "variables.tf"), []byte(`variable "a" {`), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := dirs("vpc-v1.0.0", "vpc-v1.0.0")
	opts.toDir = bad
	code, _, err := runOut(t, opts)
	if code != exitError {
		t.Errorf("exit %d, want %d", code, exitError)
	}
	if err == nil || !strings.Contains(err.Error(), "parsing the new module") {
		t.Errorf("error = %v", err)
	}
}

func TestGitRefsResolveInARepository(t *testing.T) {
	repo := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		gitCmd(t, repo, args...)
	}
	g("init", "--quiet", "--initial-branch=main")
	g("config", "user.name", "Fixture")
	g("config", "user.email", "fixture@example.com")

	copyDir(t, filepath.Join(examples, "vpc-v1.1.0"), repo)
	g("add", ".")
	g("commit", "--quiet", "-m", "release v1.1.0")
	g("tag", "v1.1.0")

	removeTF(t, repo)
	copyDir(t, filepath.Join(examples, "vpc-v2.0.0"), repo)
	g("add", "-A")
	g("commit", "--quiet", "-m", "break the API")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	opts := options{dir: ".", from: "v1.1.0", to: "HEAD", format: "table", failOn: "major"}
	code, out, err := runOut(t, opts)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (%v)\n%s", code, err, out)
	}
	if !strings.Contains(out, "severity: major") || !strings.Contains(out, "VARIABLE_REMOVED") {
		t.Errorf("output:\n%s", out)
	}

	opts.failOn = "never"
	opts.to = "v1.1.0"
	if code, out, err := runOut(t, opts); code != 0 || !strings.Contains(out, "no public API changes") {
		t.Errorf("a ref compared with itself: exit %d (%v)\n%s", code, err, out)
	}
}

func TestLatestTagPicksThePreviousReleaseAndSuggestsTheNext(t *testing.T) {
	repo := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		gitCmd(t, repo, args...)
	}
	g("init", "--quiet", "--initial-branch=main")
	g("config", "user.name", "Fixture")
	g("config", "user.email", "fixture@example.com")

	commit := func(example, msg string, tags ...string) {
		t.Helper()
		removeTF(t, repo)
		copyDir(t, filepath.Join(examples, example), repo)
		g("add", "-A")
		g("commit", "--quiet", "--allow-empty", "-m", msg)
		for _, tag := range tags {
			g("tag", tag)
		}
	}
	commit("vpc-v1.0.0", "vpc 1.0.0", "vpc/v1.0.0", "v7.0.0")
	commit("vpc-v1.0.0", "unrelated module", "eks/v9.0.0", "vpc/v1.1.0-rc.1")
	commit("vpc-v1.1.0", "vpc 1.1.0", "vpc/v1.1.0")
	commit("vpc-v1.1.1", "vpc 1.1.1")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	opts := options{
		dir: ".", from: latestTag, to: "HEAD", tagPrefix: "vpc/", suggest: true,
		format: "table", failOn: "major",
	}
	code, out, err := runOut(t, opts)
	if code != 0 {
		t.Fatalf("exit %d (%v)\n%s", code, err, out)
	}
	for _, want := range []string{"vpc/v1.1.0 -> HEAD", "severity: patch", "next version: vpc/v1.1.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// Once HEAD carries its own tag, the previous release is still v1.1.0.
	g("tag", "vpc/v1.1.1")
	if _, out, _ := runOut(t, opts); !strings.Contains(out, "vpc/v1.1.0 -> HEAD") {
		t.Errorf("a tag on --to itself was picked as the previous release:\n%s", out)
	}

	// From the 1.1.0 commit back to 1.0.0, the change is minor.
	opts.to = "vpc/v1.1.0"
	opts.format = "json"
	_, out, err = runOut(t, opts)
	var got struct {
		From        string `json:"from"`
		Severity    string `json:"severity"`
		NextVersion string `json:"next_version"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode json: %v\n%s", err, out)
	}
	if got.From != "vpc/v1.0.0" || got.Severity != "minor" || got.NextVersion != "vpc/v1.1.0" {
		t.Errorf("json = %+v", got)
	}

	// No prefix: the bare tags v7.0.0 count, the prefixed ones do not parse.
	opts = options{dir: ".", from: latestTag, to: "HEAD", suggest: true, format: "table", failOn: "never"}
	if _, out, err := runOut(t, opts); !strings.Contains(out, "v7.0.0 -> HEAD") || !strings.Contains(out, "next version: v7.1.0") {
		t.Errorf("without a prefix (%v):\n%s", err, out)
	}

	// A prefix with no release under it is a usage error.
	opts = options{dir: ".", from: latestTag, to: "HEAD", tagPrefix: "rds/", format: "table", failOn: "major"}
	if code, _, err := runOut(t, opts); code != exitError || err == nil || !strings.Contains(err.Error(), "rds/") {
		t.Errorf("missing prefix: exit %d, err %v", code, err)
	}

	// --suggest-version cannot bump a branch name.
	opts = options{dir: ".", from: "main", to: "HEAD", suggest: true, format: "table", failOn: "major"}
	if code, _, err := runOut(t, opts); code != exitError || err == nil {
		t.Errorf("suggest from a branch: exit %d, err %v", code, err)
	}
}

func TestSuggestVersionRejectsDirectories(t *testing.T) {
	opts := dirs("vpc-v1.0.0", "vpc-v1.1.0")
	opts.suggest = true
	if code, _, err := runOut(t, opts); code != exitError || err == nil {
		t.Errorf("exit %d, err %v", code, err)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func removeTF(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			t.Fatal(err)
		}
	}
}
