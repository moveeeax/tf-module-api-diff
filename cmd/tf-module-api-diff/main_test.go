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

// exampleContract is the checked-in contract of examples/vpc-v1.1.0.
var exampleContract = filepath.Join(examples, "vpc-v1.1.0", "api.contract.json")

func contractOpts(contract, toDir string) options {
	return options{dir: ".", contract: contract, toDir: toDir, format: "table", failOn: "major"}
}

func TestCheckedInExampleContractMatchesItsModule(t *testing.T) {
	var buf bytes.Buffer
	opts := options{dir: ".", toDir: filepath.Join(examples, "vpc-v1.1.0"), writeTo: "-"}
	if code, err := run(opts, &buf); code != exitOK {
		t.Fatalf("exit %d (%v)", code, err)
	}
	want, err := os.ReadFile(exampleContract)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != string(want) {
		t.Errorf("%s is stale; regenerate it with --write-contract:\n%s", exampleContract, buf.String())
	}
}

func TestContractClassifiesTheExampleRevisions(t *testing.T) {
	cases := []struct {
		to       string
		severity string
		exit     int
		kind     string
	}{
		{"vpc-v1.1.0", "patch", 0, "no public API changes"},
		{"vpc-v1.1.1", "patch", 0, "VARIABLE_DEFAULT_CHANGED"},
		{"vpc-v2.0.0", "major", 1, "OUTPUT_SENSITIVE_ADDED"},
		// Going back to 1.0.0 drops an output and an input: a break.
		{"vpc-v1.0.0", "major", 1, "OUTPUT_REMOVED"},
	}
	for _, c := range cases {
		code, out, err := runOut(t, contractOpts(exampleContract, filepath.Join(examples, c.to)))
		if code != c.exit {
			t.Errorf("contract -> %s: exit %d, want %d (%v)", c.to, code, c.exit, err)
		}
		for _, want := range []string{"api.contract.json -> ", "severity: " + c.severity, c.kind} {
			if !strings.Contains(out, want) {
				t.Errorf("contract -> %s: output missing %q:\n%s", c.to, want, out)
			}
		}
	}
}

func TestContractGatesAndJUnit(t *testing.T) {
	// --fail-on patch turns the contract into an exact match.
	opts := contractOpts(exampleContract, filepath.Join(examples, "vpc-v1.1.1"))
	opts.failOn = "patch"
	if code, _, _ := runOut(t, opts); code != exitGate {
		t.Errorf("fail-on patch with patch drift: exit %d, want %d", code, exitGate)
	}

	opts = contractOpts(exampleContract, filepath.Join(examples, "vpc-v2.0.0"))
	opts.format = "junit"
	code, out, _ := runOut(t, opts)
	if code != exitGate {
		t.Errorf("exit %d, want %d", code, exitGate)
	}
	for _, want := range []string{`<testsuites name="tf-module-api-diff" tests="8" failures="4">`,
		`name="output.flow_log_role_arn OUTPUT_SENSITIVE_ADDED"`, `<system-out>patch: description text only</system-out>`} {
		if !strings.Contains(out, want) {
			t.Errorf("junit missing %q:\n%s", want, out)
		}
	}

	// A declared major lets the same change through, and no case fails.
	opts.expect = "major"
	code, out, _ = runOut(t, opts)
	if code != exitOK || !strings.Contains(out, `failures="0"`) {
		t.Errorf("expect major: exit %d\n%s", code, out)
	}
}

func TestWriteContractThenCheckTheWorkingTree(t *testing.T) {
	mod := t.TempDir()
	copyDir(t, filepath.Join(examples, "vpc-v1.1.0"), mod)
	path := filepath.Join(t.TempDir(), "api.contract.json")

	code, out, err := runOut(t, options{dir: mod, writeTo: path})
	if code != exitOK || !strings.Contains(out, "7 variables, 4 outputs") {
		t.Fatalf("write: exit %d (%v)\n%s", code, err, out)
	}

	opts := options{dir: mod, contract: path, format: "json", failOn: "major"}
	if code, out, err := runOut(t, opts); code != exitOK || !strings.Contains(out, `"to": "working tree"`) {
		t.Errorf("unchanged module: exit %d (%v)\n%s", code, err, out)
	}

	// Rename an output in place: the contract sees a removal and an addition.
	outputs := filepath.Join(mod, "outputs.tf")
	body, err := os.ReadFile(outputs)
	if err != nil {
		t.Fatal(err)
	}
	renamed := strings.Replace(string(body), `output "vpc_id"`, `output "id"`, 1)
	if err := os.WriteFile(outputs, []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.format = "table"
	code, out, err = runOut(t, opts)
	if code != exitGate || !strings.Contains(out, "OUTPUT_REMOVED") || !strings.Contains(out, "output.vpc_id") {
		t.Errorf("renamed output: exit %d (%v)\n%s", code, err, out)
	}
	if err == nil || !strings.Contains(err.Error(), "OUTPUT_REMOVED on output.vpc_id") {
		t.Errorf("error = %v", err)
	}
}

func TestContractAgainstAGitRef(t *testing.T) {
	repo := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		gitCmd(t, repo, args...)
	}
	g("init", "--quiet", "--initial-branch=main")
	g("config", "user.name", "Fixture")
	g("config", "user.email", "fixture@example.com")
	mod := filepath.Join(repo, "modules", "vpc")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	copyDir(t, filepath.Join(examples, "vpc-v2.0.0"), mod)
	g("add", "-A")
	g("commit", "--quiet", "-m", "vpc 2.0.0")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	contractPath, err := filepath.Abs(exampleContract)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	opts := options{dir: "modules/vpc", contract: contractPath, to: "HEAD", format: "table", failOn: "major"}
	code, out, err := runOut(t, opts)
	if code != exitGate || !strings.Contains(out, "modules/vpc: ") || !strings.Contains(out, "-> HEAD") {
		t.Errorf("exit %d (%v)\n%s", code, err, out)
	}
}

func TestContractUsageErrorsExitTwo(t *testing.T) {
	v110 := filepath.Join(examples, "vpc-v1.1.0")
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"contract": 1, "outputs": {"id": {"sensitve": true}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]options{
		"contract and from": func() options {
			o := contractOpts(exampleContract, v110)
			o.toDir, o.from, o.to = "", "v1.0.0", "HEAD"
			return o
		}(),
		"contract and from-dir": func() options { o := contractOpts(exampleContract, v110); o.fromDir = v110; return o }(),
		"contract and suggest":  func() options { o := contractOpts(exampleContract, v110); o.suggest = true; return o }(),
		"to and to-dir":         func() options { o := contractOpts(exampleContract, v110); o.to = "HEAD"; return o }(),
		"missing contract":      contractOpts(filepath.Join(t.TempDir(), "nope.json"), v110),
		"misspelt contract":     contractOpts(bad, v110),
		"write and contract":    {dir: ".", toDir: v110, writeTo: "-", contract: exampleContract},
		"write and from-dir":    {dir: ".", toDir: v110, writeTo: "-", fromDir: v110},
		"write without module":  {dir: t.TempDir(), writeTo: "-"},
	}
	for name, opts := range cases {
		code, _, err := runOut(t, opts)
		if code != exitError || err == nil {
			t.Errorf("%s: exit %d, err %v; want %d and an error", name, code, err, exitError)
		}
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
