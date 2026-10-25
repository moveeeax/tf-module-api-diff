// Command tf-module-api-diff compares the public API of a Terraform module at
// two revisions and reports the semver severity of the change.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/moveeeax/tf-module-api-diff/internal/diff"
	"github.com/moveeeax/tf-module-api-diff/internal/modapi"
	"github.com/moveeeax/tf-module-api-diff/internal/render"
	"github.com/moveeeax/tf-module-api-diff/internal/source"
	"github.com/moveeeax/tf-module-api-diff/internal/verdict"
)

const version = "0.1.0"

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
	flag.StringVar(&opts.from, "from", "", "old git ref (tag, branch or commit)")
	flag.StringVar(&opts.to, "to", "", "new git ref (tag, branch or commit)")
	flag.StringVar(&opts.fromDir, "from-dir", "", "old module directory, instead of a git ref")
	flag.StringVar(&opts.toDir, "to-dir", "", "new module directory, instead of a git ref")
	flag.StringVar(&opts.format, "format", "table", "output format: table, json or markdown")
	flag.StringVar(&opts.expect, "expect", "", "the bump you intend to publish: major, minor or patch")
	flag.StringVar(&opts.failOn, "fail-on", "major", "fail at this severity or above: major, minor, patch or never")
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
  tf-module-api-diff --from-dir <path> --to-dir <path> [flags]

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
	oldFiles, newFiles, from, to, err := load(opts)
	if err != nil {
		return exitError, err
	}

	oldAPI, err := modapi.FromFiles(oldFiles)
	if err != nil {
		return exitError, fmt.Errorf("parsing the old module: %w", err)
	}
	newAPI, err := modapi.FromFiles(newFiles)
	if err != nil {
		return exitError, fmt.Errorf("parsing the new module: %w", err)
	}

	result := diff.Diff(oldAPI, newAPI)
	result.From, result.To, result.Dir = from, to, opts.dir

	switch opts.format {
	case "table":
		err = render.Table(stdout, result)
	case "markdown":
		err = render.Markdown(stdout, result)
	case "json":
		err = render.JSON(stdout, result)
	default:
		return exitError, fmt.Errorf("unknown --format %q (want table, json or markdown)", opts.format)
	}
	if err != nil {
		return exitError, err
	}

	v, err := verdict.Decide(result, opts.failOn, opts.expect)
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

// load resolves the two sides of the comparison from either directory pair or
// git refs, rejecting a half-specified request rather than guessing.
func load(opts options) (oldFiles, newFiles map[string]string, from, to string, err error) {
	usingDirs := opts.fromDir != "" || opts.toDir != ""
	usingRefs := opts.from != "" || opts.to != ""

	switch {
	case usingDirs && usingRefs:
		return nil, nil, "", "", fmt.Errorf("use either --from/--to or --from-dir/--to-dir, not both")
	case usingDirs:
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
		if oldFiles, err = source.FromRef(repo, opts.from, opts.dir); err != nil {
			return nil, nil, "", "", err
		}
		if newFiles, err = source.FromRef(repo, opts.to, opts.dir); err != nil {
			return nil, nil, "", "", err
		}
		return oldFiles, newFiles, opts.from, opts.to, nil
	}
	return nil, nil, "", "", fmt.Errorf("nothing to compare: pass --from/--to or --from-dir/--to-dir")
}
