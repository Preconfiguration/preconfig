// Package doctor reads the log of a setup that failed and says why: the step
// that broke, the lines that show it, the cause in plain words, and, when the
// fix belongs in preconfig.yaml, the change that makes it. It runs no model and
// reaches nothing outside the log: every answer comes from a rule that was
// written for a real kind of failure, and a log no rule explains is reported as
// unknown rather than guessed at.
package doctor

import (
	"fmt"
	"regexp"
	"strings"

	"preconfiguration.com/preconfig/internal/spec"
)

// Version of Doctor.
const Version = "0.1.0"

// Outcomes of reading a log.
const (
	Failed  = "failed"  // the log shows a failure
	Passed  = "passed"  // the log shows the setup, or the build, finishing
	Unclear = "unclear" // the log shows neither: no steps and no error Doctor knows
)

// Change is one edit to preconfig.yaml.
type Change struct {
	Op       string `json:"op"`                 // add-package, replace-package, add-service, set-runtime, add-tool, add-env, set-env, add-secret
	Key      string `json:"key,omitempty"`      // the service, runtime or variable
	Value    string `json:"value,omitempty"`    // the package, version, tool or value
	Old      string `json:"old,omitempty"`      // replace-package: the name it replaces
	User     string `json:"user,omitempty"`     // add-service postgres
	Password string `json:"password,omitempty"` // add-service postgres
	Database string `json:"database,omitempty"` // add-service postgres
}

// String says the change in words.
func (c Change) String() string {
	switch c.Op {
	case "add-package":
		return "add " + c.Value + " to packages"
	case "replace-package":
		return "replace " + c.Old + " with " + c.Value + " in packages"
	case "add-service":
		s := "add " + c.Key + " " + c.Value + " under services"
		if c.User != "" {
			s += fmt.Sprintf(", with user %s and database %s", c.User, c.Database)
		}
		return s
	case "set-runtime":
		return "set runtimes." + c.Key + " to " + c.Value
	case "add-tool":
		return "add " + c.Value + " to tools"
	case "add-env":
		return "add " + c.Key + " under env: " + Mask(c.Value)
	case "set-env":
		return "set env " + c.Key + " to " + Mask(c.Value)
	case "add-secret":
		return "add " + c.Key + " under secrets"
	}
	return c.Op
}

// Diagnosis is Doctor's answer for one log.
type Diagnosis struct {
	Log      string   `json:"log"`     // the kind of log: setup, verify, events, docker, actions, cloud-init, text
	Lines    int      `json:"lines"`   // lines in the log as given
	Outcome  string   `json:"outcome"` // failed, passed or unclear
	Step     string   `json:"step,omitempty"`
	Phase    string   `json:"phase,omitempty"`
	ExitCode int      `json:"exitCode,omitempty"`
	Rule     string   `json:"rule,omitempty"`     // the rule that explained it, such as D401
	Category string   `json:"category,omitempty"` // spec, network, repository, secret, code, platform or unknown
	Cause    string   `json:"cause,omitempty"`
	Evidence []Line   `json:"evidence,omitempty"`
	Changes  []Change `json:"changes,omitempty"`
	Advice   string   `json:"advice,omitempty"`
}

// Fixable reports whether the diagnosis comes with changes to preconfig.yaml.
func (d Diagnosis) Fixable() bool { return len(d.Changes) > 0 }

// Diagnose reads a log and explains it. s, the spec the setup was built from,
// is optional; with it, Doctor can fill in values such as a database's user
// from the spec, and tell a secret from a missing variable.
func Diagnose(text string, s *spec.Spec) Diagnosis {
	lg := Read(text)
	d := Diagnosis{Log: lg.Kind, Lines: lg.Total}
	var st *Step
	for i := len(lg.Steps) - 1; i >= 0; i-- {
		if lg.Steps[i].Failed {
			st = &lg.Steps[i]
			break
		}
	}
	lines := lg.Lines
	switch {
	case st != nil:
		lines = lg.Lines[st.From:st.To]
		d.Step, d.Phase, d.ExitCode = st.Name, st.Phase, st.Code
	case lg.Passed && !lg.Failed:
		d.Outcome = Passed
		return d
	case lg.Failed:
		// verify said NOT READY without naming a step: read everything.
	}
	c := newCtx(lines, st, s, lg)
	for _, r := range rules {
		f := r.fn(c)
		if f == nil {
			continue
		}
		d.Outcome = Failed
		d.Rule = r.id
		d.Category, d.Cause, d.Advice, d.Changes = f.category, Mask(f.cause), Mask(f.advice), f.changes
		d.Evidence = pick(c, f.evidence)
		return d
	}
	if st == nil && !lg.Failed {
		d.Outcome = Unclear
		d.Category = CatUnknown
		d.Cause = "Doctor found no setup steps in this log, and no error it knows."
		if n := len(lg.Steps); n > 0 {
			d.Step = lg.Steps[n-1].Name
			d.Cause = fmt.Sprintf("The log stops during the step %q, with no error and no result: it may have been cut off.", d.Step)
		}
		d.Advice = "Give Doctor the whole log of the run: preconfig verify --log FILE writes it, and so does a platform's setup log."
		return d
	}
	d.Outcome = Failed
	d.Category = CatUnknown
	d.Rule = ""
	where := "The setup failed"
	if st != nil {
		where = fmt.Sprintf("The step %q failed", st.Name)
		if st.Code != 0 {
			where += fmt.Sprintf(" with exit code %d", st.Code)
		}
	}
	d.Cause = where + ", and no rule of Doctor's explains it."
	d.Evidence = pick(c, errorLines(c))
	if lg.Kind == KindEvents {
		d.Advice = "verify --json names the step but not what it printed. Run verify again with --log FILE and give Doctor that file."
	} else {
		d.Advice = "These are the lines that look like errors. Doctor doesn't guess: nothing in preconfig.yaml is changed."
	}
	return d
}

// pick turns indexes into lines, and adds the step's own failure line.
func pick(c *ctx, idx []int) []Line {
	var out []Line
	seen := map[int]bool{}
	for _, i := range idx {
		if i >= 0 && i < len(c.lines) && !seen[i] {
			seen[i] = true
			out = append(out, masked(c.lines[i]))
		}
	}
	for i := len(c.lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(c.lines[i].Text, ">>> preconfig: FAILED") {
			if !seen[i] {
				out = append(out, masked(c.lines[i]))
			}
			break
		}
	}
	if len(out) > 6 {
		out = append(out[:5], out[len(out)-1])
	}
	return out
}

func masked(l Line) Line { return Line{N: l.N, Text: Mask(l.Text)} }

var errWordRe = regexp.MustCompile(`(?i)\b(error|fatal|failed|failure|cannot|can't|not found|no such file|denied|refused|unable to)\b|^E: |^E\s{2,}`)

// errorLines finds the lines that look like errors, for a log no rule
// explains: the first two and the last three.
func errorLines(c *ctx) []int {
	var hits []int
	for i, l := range c.lines {
		if c.noise[i] || strings.HasPrefix(l.Text, ">>> preconfig:") {
			continue
		}
		if errWordRe.MatchString(l.Text) {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		return lastLines(c, 5)
	}
	if len(hits) > 5 {
		hits = append(hits[:2], hits[len(hits)-3:]...)
	}
	return hits
}

// Text renders a diagnosis for a terminal.
func Text(d Diagnosis) string {
	var b strings.Builder
	switch d.Outcome {
	case Passed:
		fmt.Fprintf(&b, "doctor: the log shows the setup finishing (%s log, %d lines). Nothing to fix.\n", d.Log, d.Lines)
		return b.String()
	case Unclear:
		fmt.Fprintf(&b, "doctor: %s\n%s\n", d.Cause, d.Advice)
		return b.String()
	}
	fmt.Fprintf(&b, "doctor: read a %s log of %d lines\n", d.Log, d.Lines)
	if d.Step != "" {
		fmt.Fprintf(&b, "  step     %s", d.Step)
		if d.ExitCode != 0 {
			fmt.Fprintf(&b, "  (exit code %d)", d.ExitCode)
		}
		b.WriteString("\n")
	}
	cat := d.Category
	if d.Rule != "" {
		cat += ", rule " + d.Rule
	}
	fmt.Fprintf(&b, "  cause    %s (%s)\n", d.Cause, cat)
	if len(d.Evidence) > 0 {
		b.WriteString("  evidence\n")
		for _, l := range d.Evidence {
			fmt.Fprintf(&b, "    %5d | %s\n", l.N, clip(l.Text, 150))
		}
	}
	for _, c := range d.Changes {
		fmt.Fprintf(&b, "  fix      %s\n", c.String())
	}
	if d.Advice != "" {
		fmt.Fprintf(&b, "  advice   %s\n", d.Advice)
	}
	return b.String()
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
