package source

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFromDirReadsOnlyTopLevelTerraformFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "variables.tf"), `variable "a" { type = string }`)
	write(t, filepath.Join(dir, "README.md"), "not terraform")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "nested", "variables.tf"), `variable "b" { type = string }`)

	files, err := FromDir(dir)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v", keys(files))
	}
	if _, ok := files["variables.tf"]; !ok {
		t.Errorf("files = %v", keys(files))
	}
}

func TestFromDirFailsWithoutTerraformFiles(t *testing.T) {
	if _, err := FromDir(t.TempDir()); err == nil {
		t.Error("an empty directory should be an error, not an empty API")
	}
	if _, err := FromDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing directory should be an error")
	}
}

func TestFromRefReadsATaggedRevision(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "--quiet", "--initial-branch=main")
	runGit(t, repo, "config", "user.name", "Fixture")
	runGit(t, repo, "config", "user.email", "fixture@example.com")

	module := filepath.Join(repo, "modules", "vpc")
	if err := os.MkdirAll(filepath.Join(module, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(module, "variables.tf"), `variable "cidr" {
  type    = string
  default = "10.0.0.0/16"
}`)
	write(t, filepath.Join(module, "nested", "variables.tf"), `variable "ignored" { type = string }`)
	write(t, filepath.Join(repo, "root.tf"), `variable "root_level" { type = string }`)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "--quiet", "-m", "add the module")
	runGit(t, repo, "tag", "v1.0.0")

	write(t, filepath.Join(module, "variables.tf"), `variable "cidr" { type = string }`)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "--quiet", "-m", "drop the default")

	root, err := RepoRoot(module)
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	oldFiles, err := FromRef(root, "v1.0.0", "modules/vpc")
	if err != nil {
		t.Fatalf("FromRef v1.0.0: %v", err)
	}
	if len(oldFiles) != 1 || !strings.Contains(oldFiles["variables.tf"], "10.0.0.0/16") {
		t.Fatalf("old files = %v", oldFiles)
	}

	newFiles, err := FromRef(root, "HEAD", "modules/vpc")
	if err != nil {
		t.Fatalf("FromRef HEAD: %v", err)
	}
	if strings.Contains(newFiles["variables.tf"], "10.0.0.0/16") {
		t.Errorf("HEAD still carries the default: %q", newFiles["variables.tf"])
	}

	rootFiles, err := FromRef(root, "HEAD", ".")
	if err != nil {
		t.Fatalf("FromRef root: %v", err)
	}
	if len(rootFiles) != 1 {
		t.Errorf("root module files = %v, want only root.tf", keys(rootFiles))
	}
}

func TestFromRefRejectsUnknownRefAndEmptyDir(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "--quiet", "--initial-branch=main")
	runGit(t, repo, "config", "user.name", "Fixture")
	runGit(t, repo, "config", "user.email", "fixture@example.com")
	write(t, filepath.Join(repo, "main.tf"), `variable "a" { type = string }`)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "--quiet", "-m", "init")

	if _, err := FromRef(repo, "v9.9.9", "."); err == nil {
		t.Error("an unknown ref should fail")
	}
	if _, err := FromRef(repo, "HEAD", "modules/missing"); err == nil {
		t.Error("a module path with no .tf files should fail")
	}
}

func TestRepoRootOutsideARepository(t *testing.T) {
	dir := t.TempDir()
	// A bare temp dir is only outside a repo when no ancestor is one; skip if it is.
	if _, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output(); err == nil {
		t.Skip("temp dir sits inside a git repository")
	}
	if _, err := RepoRoot(dir); err == nil {
		t.Error("RepoRoot should fail outside a repository")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
