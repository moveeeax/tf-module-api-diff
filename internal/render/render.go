// Package render turns a diff result into the three output formats.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/moveeeax/tf-module-api-diff/internal/diff"
)

// Table writes an aligned plain-text report.
func Table(w io.Writer, r diff.Result) error {
	header := fmt.Sprintf("%s -> %s", label(r.From), label(r.To))
	if r.Dir != "" && r.Dir != "." {
		header = r.Dir + ": " + header
	}
	fmt.Fprintf(w, "%s\nseverity: %s\n", header, r.Severity)
	if !r.Changed() {
		fmt.Fprintln(w, "\nno public API changes")
		return nil
	}

	rows := [][3]string{{"SEVERITY", "CHANGE", "TARGET"}}
	for _, c := range r.Changes {
		rows = append(rows, [3]string{strings.ToUpper(c.Level), c.Kind, c.Target})
	}
	widths := [3]int{}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	fmt.Fprintln(w)
	for i, row := range rows {
		fmt.Fprintf(w, "%-*s  %-*s  %-*s", widths[0], row[0], widths[1], row[1], widths[2], row[2])
		if i == 0 {
			fmt.Fprintln(w, "  DETAIL")
			continue
		}
		fmt.Fprintf(w, "  %s\n", r.Changes[i-1].Detail)
	}
	fmt.Fprintf(w, "\n%d major, %d minor, %d patch\n",
		r.Count(diff.Major), r.Count(diff.Minor), r.Count(diff.Patch))
	return nil
}

// Markdown writes a report suited to a pull-request comment or job summary.
func Markdown(w io.Writer, r diff.Result) error {
	fmt.Fprintf(w, "### Module API diff: `%s` -> `%s`\n\n", label(r.From), label(r.To))
	if r.Dir != "" && r.Dir != "." {
		fmt.Fprintf(w, "Module: `%s`\n\n", r.Dir)
	}
	fmt.Fprintf(w, "**Severity: %s**\n\n", r.Severity)
	if !r.Changed() {
		fmt.Fprintln(w, "No public API changes.")
		return nil
	}
	fmt.Fprintln(w, "| Severity | Change | Target | Detail |")
	fmt.Fprintln(w, "| --- | --- | --- | --- |")
	for _, c := range r.Changes {
		fmt.Fprintf(w, "| %s | `%s` | `%s` | %s |\n", c.Level, c.Kind, c.Target, escapePipes(c.Detail))
	}
	fmt.Fprintf(w, "\n%d major, %d minor, %d patch.\n",
		r.Count(diff.Major), r.Count(diff.Minor), r.Count(diff.Patch))
	return nil
}

// JSON writes the machine-readable form.
func JSON(w io.Writer, r diff.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func label(s string) string {
	if s == "" {
		return "(dir)"
	}
	return s
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
