// Command tf-module-api-diff compares the public API of a Terraform module at
// two revisions and reports the semver severity of the change.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/moveeeax/tf-module-api-diff/internal/contract"
	"github.com/moveeeax/tf-module-api-diff/internal/diff"
	"github.com/moveeeax/tf-module-api-diff/internal/modapi"
	"github.com/moveeeax/tf-module-api-diff/internal/render"
	"github.com/moveeeax/tf-module-api-diff/internal/semver"
	"github.com/moveeeax/tf-module-api-diff/internal/source"
	"github.com/moveeeax/tf-module-api-diff/internal/verdict"
)

const version = "0.2.0"

// latestTag is the --from value that asks for the previous release tag.
const latestTag = "latest-tag"

// Exit codes: 0 clean, 1 a gate rejected the change, 2 a usage or parse error.
const (
	exitOK    = 0
	exitGate  = 1
	exitError = 2
)

type options struct {
	dir         string
	from        string
	to          string
	fromDir     string
	toDir       string
	format      string
	expect      string
	failOn      string
	tagPrefix   string
	suggest     bool
	contract    string
	writeTo     string
	showVersion bool
}

func main() {
	opts := parseFlags()
	if opts.showVersion {
		fmt.Println("tf-module-api-diff " + version)
		return
	}
	code, err := run(opts, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tf-module-api-diff: "+err.Error())
	}
	os.Exit(code)
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.dir, "dir", ".", "module path inside the repository")
	flag.StringVar(&opts.from, "from", "", "old git ref (tag, branch or commit), or latest-tag for the highest release tag reachable from --to")
	flag.StringVar(&opts.to, "to", "", "new git ref (tag, branch or commit)")
	flag.StringVar(&opts.fromDir, "from-dir", "", "old module directory, instead of a git ref")
	flag.StringVar(&opts.toDir, "to-dir", "", "new module directory, instead of a git ref")
	flag.StringVar(&opts.contract, "contract", "", "contract file to compare the module against, instead of --from or --from-dir")
	flag.StringVar(&opts.writeTo, "write-contract", "", "write the module's API as a contract file (- for stdout) and exit")
	flag.StringVar(&opts.format, "format", "table", "output format: table, json, markdown or junit")
	flag.StringVar(&opts.expect, "expect", "", "the bump you intend to publish: major, minor or patch")
	flag.StringVar(&opts.failOn, "fail-on", "major", "fail at this severity or above: major, minor, patch or never")
	flag.StringVar(&opts.tagPrefix, "tag-prefix", "", "release tag prefix for latest-tag and --suggest-version, such as vpc/")
	flag.BoolVar(&opts.suggest, "suggest-version", false, "print the next release tag after applying the computed severity")
	flag.BoolVar(&opts.showVersion, "version", false, "print the version and exit")
	flag.Usage = usage
	flag.Parse()
	return opts
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, `tf-module-api-diff %s - semver severity of a Terraform module API change

Usage:
  tf-module-api-diff --from <ref> --to <ref> [--dir <module>] [flags]
  tf-module-api-diff --from latest-tag --to HEAD [--tag-prefix vpc/] [--suggest-version] [flags]
  tf-module-api-diff --from-dir <path> --to-dir <path> [flags]
  tf-module-api-diff --contract <file> [--dir <module> | --to <ref> | --to-dir <path>] [flags]
  tf-module-api-diff --write-contract <file> [--dir <module> | --to <ref> | --to-dir <path>]

Flags:
`, version)
	flag.PrintDefaults()
	fmt.Fprintf(out, `
Exit codes:
  0  the change is within what --fail-on and --expect allow
  1  a gate rejected the change
  2  usage error, unreadable module or unparseable HCL
`)
}

func run(opts options, stdout io.Writer) (int, error) {
	if opts.writeTo != "" {
		return writeContract(opts, stdout)
	}

	oldAPI, newAPI, from, to, err := load(opts)
	if err != nil {
		return exitError, err
	}

	result := diff.Diff(oldAPI, newAPI)
	result.From, result.To, result.Dir = from, to, opts.dir

	if opts.suggest {
		prev, err := semver.Parse(from, opts.tagPrefix)
		if err != nil {
			return exitError, fmt.Errorf("--suggest-version needs --from to be a release tag: %w", err)
		}
		result.NextVersion = prev.Bump(result.Severity).String()
	}

	// The verdict comes first because the JUnit report marks the changes the
	// gate rejects; a bad --fail-on or --expect is a usage error either way.
	v, err := verdict.Decide(result, opts.failOn, opts.expect)
	if err != nil {
		return exitError, err
	}

	switch opts.format {
	case "table":
		err = render.Table(stdout, result)
	case "markdown":
		err = render.Markdown(stdout, result)
	case "json":
		err = render.JSON(stdout, result)
	case "junit":
		err = render.JUnit(stdout, result, v.Rejects)
	default:
		return exitError, fmt.Errorf("unknown --format %q (want table, json, markdown or junit)", opts.format)
	}
	if err != nil {
		return exitError, err
	}

	if v.Note != "" {
		fmt.Fprintln(os.Stderr, "note: "+v.Note)
	}
	if v.Exit != 0 {
		return exitGate, fmt.Errorf("%s", v.Reason)
	}
	return exitOK, nil
}

// load resolves the two sides of the comparison from a contract file, a
// directory pair or git refs, rejecting a half-specified request rather than
// guessing.
func load(opts options) (oldAPI, newAPI *modapi.API, from, to string, err error) {
	if opts.contract != "" {
		return loadContract(opts)
	}
	oldFiles, newFiles, from, to, err := loadFiles(opts)
	if err != nil {
		return nil, nil, "", "", err
	}
	if oldAPI, err = modapi.FromFiles(oldFiles); err != nil {
		return nil, nil, "", "", fmt.Errorf("parsing the old module: %w", err)
	}
	if newAPI, err = modapi.FromFiles(newFiles); err != nil {
		return nil, nil, "", "", fmt.Errorf("parsing the new module: %w", err)
	}
	return oldAPI, newAPI, from, to, nil
}

// loadContract reads the contract file as the old side and the module as it
// stands as the new side.
func loadContract(opts options) (oldAPI, newAPI *modapi.API, from, to string, err error) {
	switch {
	case opts.from != "" || opts.fromDir != "":
		return nil, nil, "", "", fmt.Errorf("--contract is the old side; drop --from and --from-dir")
	case opts.suggest:
		return nil, nil, "", "", fmt.Errorf("--suggest-version needs --from to be a release tag, not --contract")
	}
	if oldAPI, err = contract.Load(opts.contract); err != nil {
		return nil, nil, "", "", fmt.Errorf("reading the contract: %w", err)
	}
	newAPI, to, err = current(opts)
	if err != nil {
		return nil, nil, "", "", err
	}
	return oldAPI, newAPI, opts.contract, to, nil
}

// current reads the module a contract is checked against or written from:
// --to-dir, else --dir at --to, else --dir in the working tree.
func current(opts options) (*modapi.API, string, error) {
	var (
		files map[string]string
		label string
		err   error
	)
	switch {
	case opts.toDir != "" && opts.to != "":
		return nil, "", fmt.Errorf("use either --to or --to-dir, not both")
	case opts.toDir != "":
		files, err = source.FromDir(opts.toDir)
		label = opts.toDir
	case opts.to != "":
		var repo string
		if repo, err = source.RepoRoot("."); err == nil {
			files, err = source.FromRef(repo, opts.to, opts.dir)
		}
		label = opts.to
	default:
		files, err = source.FromDir(opts.dir)
		label = "working tree"
	}
	if err != nil {
		return nil, "", err
	}
	api, err := modapi.FromFiles(files)
	if err != nil {
		return nil, "", fmt.Errorf("parsing the module: %w", err)
	}
	return api, label, nil
}

// writeContract snapshots the module's API into the --write-contract file.
func writeContract(opts options, stdout io.Writer) (int, error) {
	switch {
	case opts.contract != "":
		return exitError, fmt.Errorf("--write-contract and --contract cannot be combined")
	case opts.from != "" || opts.fromDir != "":
		return exitError, fmt.Errorf("--write-contract reads one module; drop --from and --from-dir")
	case opts.suggest:
		return exitError, fmt.Errorf("--suggest-version does not apply to --write-contract")
	}
	api, _, err := current(opts)
	if err != nil {
		return exitError, err
	}
	if opts.writeTo == "-" {
		if err := contract.Write(stdout, api); err != nil {
			return exitError, err
		}
		return exitOK, nil
	}

	var buf bytes.Buffer
	if err := contract.Write(&buf, api); err != nil {
		return exitError, err
	}
	if err := os.WriteFile(opts.writeTo, buf.Bytes(), 0o644); err != nil {
		return exitError, err
	}
	fmt.Fprintf(stdout, "wrote %s: %d variables, %d outputs\n",
		opts.writeTo, len(api.Variables), len(api.Outputs))
	return exitOK, nil
}

// loadFiles reads the .tf files of both sides from a directory pair or git
// refs.
func loadFiles(opts options) (oldFiles, newFiles map[string]string, from, to string, err error) {
	usingDirs := opts.fromDir != "" || opts.toDir != ""
	usingRefs := opts.from != "" || opts.to != ""

	switch {
	case usingDirs && usingRefs:
		return nil, nil, "", "", fmt.Errorf("use either --from/--to or --from-dir/--to-dir, not both")
	case usingDirs:
		if opts.suggest {
			return nil, nil, "", "", fmt.Errorf("--suggest-version needs git refs, not --from-dir/--to-dir")
		}
		if opts.fromDir == "" || opts.toDir == "" {
			return nil, nil, "", "", fmt.Errorf("--from-dir and --to-dir must be given together")
		}
		if oldFiles, err = source.FromDir(opts.fromDir); err != nil {
			return nil, nil, "", "", err
		}
		if newFiles, err = source.FromDir(opts.toDir); err != nil {
			return nil, nil, "", "", err
		}
		return oldFiles, newFiles, opts.fromDir, opts.toDir, nil
	case usingRefs:
		if opts.from == "" || opts.to == "" {
			return nil, nil, "", "", fmt.Errorf("--from and --to must be given together")
		}
		// --dir is a path inside the repository, so the module need not exist
		// in the working tree at all - only at the two refs.
		repo, err := source.RepoRoot(".")
		if err != nil {
			return nil, nil, "", "", err
		}
		from := opts.from
		if from == latestTag {
			if from, err = previousTag(repo, opts.to, opts.tagPrefix); err != nil {
				return nil, nil, "", "", err
			}
		}
		if oldFiles, err = source.FromRef(repo, from, opts.dir); err != nil {
			return nil, nil, "", "", err
		}
		if newFiles, err = source.FromRef(repo, opts.to, opts.dir); err != nil {
			return nil, nil, "", "", err
		}
		return oldFiles, newFiles, from, opts.to, nil
	}
	return nil, nil, "", "", fmt.Errorf("nothing to compare: pass --from/--to, --from-dir/--to-dir or --contract")
}

// previousTag resolves --from latest-tag: the highest release tag under prefix
// that is reachable from to, not counting tags on to itself.
func previousTag(repo, to, prefix string) (string, error) {
	tags, err := source.TagsBefore(repo, to, prefix)
	if err != nil {
		return "", err
	}
	_, tag, ok := semver.Highest(tags, prefix)
	if !ok {
		return "", fmt.Errorf("no release tag matching %q is reachable from %s", prefix+"[v]MAJOR.MINOR.PATCH", to)
	}
	return tag, nil
}
