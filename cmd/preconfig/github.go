package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"preconfiguration.com/preconfig/internal/check"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
)

// now is the clock the knowledge's age is measured against; tests set it.
var now = time.Now

// DefaultMaxAge is how many days old preconfig's knowledge of the platforms
// may be before check warns: their setup formats change every few months.
const DefaultMaxAge = 60

// knowledgeAge is a warning when kb.Date is more than maxDays old, or nil.
func knowledgeAge(maxDays int, hasSpec bool) *spec.Diagnostic {
	if maxDays <= 0 {
		return nil
	}
	d, err := time.Parse("2006-01-02", kb.Date)
	if err != nil {
		return nil
	}
	days := int(now().Sub(d).Hours() / 24)
	if days <= maxDays {
		return nil
	}
	file := ""
	if hasSpec {
		file = kb.SpecPath
	}
	return &spec.Diagnostic{File: file, Severity: spec.Warning, Code: "P001",
		Hint:    "Update preconfig: a newer release may know of format changes. --max-age 0 turns this off.",
		Message: fmt.Sprintf("preconfig %s knows the platforms' setup formats as of %s, %d days ago (more than %d)", kb.Version, kb.Date, days, maxDays)}
}

// ghEscape escapes text for a GitHub workflow command's message.
func ghEscape(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// ghProp escapes a workflow command's property value.
func ghProp(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

// writeGitHub prints a finding per workflow command: errors as errors,
// warnings as warnings, notes as notices.
func writeGitHub(w io.Writer, rep check.Report, prefix string) {
	for _, d := range rep.Findings {
		cmd := "notice"
		switch d.Severity {
		case spec.Error:
			cmd = "error"
		case spec.Warning:
			cmd = "warning"
		}
		props := ""
		if d.File != "" {
			props = "file=" + ghProp(path.Join(prefix, d.File)) + ","
			if d.Line > 0 {
				props += fmt.Sprintf("line=%d,", d.Line)
				if d.Col > 0 {
					props += fmt.Sprintf("col=%d,", d.Col)
				}
			}
		}
		props += "title=" + ghProp("preconfig "+d.Code)
		msg := d.Message
		if d.Hint != "" {
			msg += "\n" + d.Hint
		}
		fmt.Fprintf(w, "::%s %s::%s\n", cmd, props, ghEscape(msg))
	}
	fmt.Fprintf(w, "preconfig check: %s, %s, %s\n", plural(len(rep.Checked), "file"), plural(rep.Errors, "error"), plural(rep.Warnings, "warning"))
}

// mdCell makes text safe in a Markdown table cell.
func mdCell(s string) string {
	s = strings.NewReplacer(`\`, `\\`, "|", `\|`, "\r", " ", "\n", " ", "<", "&lt;", ">", "&gt;", "`", "'").Replace(s)
	return s
}

// appendSummary appends a Markdown report to path, as $GITHUB_STEP_SUMMARY
// wants.
func appendSummary(path string, rep check.Report) error {
	var b strings.Builder
	verdict := "passed"
	if rep.Errors > 0 {
		verdict = "failed"
	}
	fmt.Fprintf(&b, "### preconfig check %s\n\n", verdict)
	fmt.Fprintf(&b, "%s checked: %s, %s.\n\n", plural(len(rep.Checked), "file"), plural(rep.Errors, "error"), plural(rep.Warnings, "warning"))
	if len(rep.Findings) > 0 {
		b.WriteString("| File | Line | | Code | Finding |\n|---|---:|---|---|---|\n")
		for _, d := range rep.Findings {
			line := ""
			if d.Line > 0 {
				line = fmt.Sprint(d.Line)
			}
			msg := d.Message
			if d.Hint != "" {
				msg += " " + d.Hint
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", mdCell(d.File), line, d.Severity, d.Code, mdCell(msg))
		}
		b.WriteString("\n")
	}
	for _, d := range rep.Diffs {
		fmt.Fprintf(&b, "<details><summary>%s differs from a fresh build</summary>\n\n```diff\n%s```\n\n</details>\n\n", mdCell(d.Path), strings.ReplaceAll(d.Diff, "```", "'''"))
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(b.String())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
