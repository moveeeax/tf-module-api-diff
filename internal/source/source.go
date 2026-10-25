// Package source loads a module directory's .tf files, either from the working
// tree or from a git ref.
//
// The git path never touches the working tree: it reads blobs through
// `git ls-tree` and `git show`, so comparing two tags needs no checkout, no
// stash and no second clone, and the tool is safe to run on a dirty repo.
package source

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// FromDir reads the .tf files directly in dir, ignoring subdirectories: a
// module's own API is declared at its top level, and nested directories are
// separate modules.
func FromDir(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filepath.Join(dir, e.Name()), err)
		}
		files[e.Name()] = string(body)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .tf files in %s", dir)
	}
	return files, nil
}

// FromRef reads the .tf files of moduleDir as they exist at ref inside the
// repository rooted at repo.
func FromRef(repo, ref, moduleDir string) (map[string]string, error) {
	if _, err := git(repo, "rev-parse", "--verify", ref+"^{commit}"); err != nil {
		return nil, fmt.Errorf("ref %q not found in %s: %w", ref, repo, err)
	}

	prefix := path.Clean(filepath.ToSlash(moduleDir))
	if prefix == "." || prefix == "/" {
		prefix = ""
	}
	prefix = strings.TrimPrefix(prefix, "./")

	args := []string{"ls-tree", "-r", "--name-only", ref}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	listing, err := git(repo, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s at %s: %w", moduleDir, ref, err)
	}

	files := map[string]string{}
	for _, full := range strings.Split(strings.TrimSpace(listing), "\n") {
		full = strings.TrimSpace(full)
		if full == "" || !strings.HasSuffix(full, ".tf") {
			continue
		}
		rel := full
		if prefix != "" {
			if !strings.HasPrefix(full, prefix+"/") {
				continue
			}
			rel = strings.TrimPrefix(full, prefix+"/")
		}
		if strings.Contains(rel, "/") {
			continue // a nested module, not this one
		}
		body, err := git(repo, "show", ref+":"+full)
		if err != nil {
			return nil, fmt.Errorf("read %s at %s: %w", full, ref, err)
		}
		files[rel] = body
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .tf files under %q at ref %s", moduleDir, ref)
	}
	return files, nil
}

// RepoRoot resolves the repository that contains dir.
func RepoRoot(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository: %w", dir, err)
	}
	return strings.TrimSpace(out), nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}
