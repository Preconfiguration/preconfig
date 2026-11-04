// Package gen compiles a spec into the files each platform reads: a dev
// container, the Copilot setup workflow, Cursor's environment, cloud-init and a
// plain setup script. The output depends only on the spec, so building twice
// gives the same bytes, and a file that differs from a fresh build has drifted.
package gen

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
)

// File is one generated file.
type File struct {
	Path    string `json:"path"`
	Target  string `json:"target"`
	Content string `json:"content"`
	Mode    string `json:"mode,omitempty"` // "0755" for scripts
	// Merge, when set, combines the generated content with a file that is
	// already there (preconfig owns only part of it, as with
	// .claude/settings.json). Build writes, and check expects, its result.
	Merge string `json:"merge,omitempty"` // "claude-settings"
}

// MergeClaude is the Merge kind of .claude/settings.json.
const MergeClaude = "claude-settings"

// Expected is what f's path should hold, given what it holds now.
func (f File) Expected(existing string, exists bool) (string, error) {
	switch f.Merge {
	case MergeClaude:
		return MergeClaudeSettings(f.Content, existing, exists)
	}
	return f.Content, nil
}

// Result is the output of a build.
type Result struct {
	Files []File            `json:"files"`
	Notes []spec.Diagnostic `json:"notes"`
}

// Build writes every target the spec lists.
func Build(s *spec.Spec) Result {
	var r Result
	for _, t := range s.Targets {
		switch t {
		case "devcontainer":
			r.Files = append(r.Files, devcontainer(s)...)
		case "copilot":
			r.Files = append(r.Files, copilot(s))
		case "cursor":
			r.Files = append(r.Files, cursor(s)...)
		case "cloud-init":
			f := cloudInit(s)
			r.Files = append(r.Files, f)
			if len(f.Content) > 16384 {
				r.Notes = append(r.Notes, spec.Diagnostic{File: f.Path, Severity: spec.Warning, Code: "B010",
					Message: fmt.Sprintf("cloud-init.yaml is %d bytes; AWS EC2 accepts user data up to 16,384 bytes", len(f.Content))})
			}
		case "script":
			r.Files = append(r.Files, Script(s))
		case "claude-code":
			r.Files = append(r.Files, claudeCode(s)...)
		case "codex":
			r.Files = append(r.Files, codex(s))
		case "ona":
			r.Files = append(r.Files, ona(s))
		case "devin":
			r.Files = append(r.Files, devin(s))
		}
	}
	r.Notes = append(r.Notes, notes(s)...)
	sort.SliceStable(r.Files, func(i, j int) bool { return r.Files[i].Path < r.Files[j].Path })
	if r.Notes == nil {
		r.Notes = []spec.Diagnostic{}
	}
	return r
}

// notes are reminders a build prints: what the files can't carry.
func notes(s *spec.Spec) []spec.Diagnostic {
	var ns []spec.Diagnostic
	if len(s.Secrets) > 0 {
		names := andList(s.Secrets)
		one := len(s.Secrets) == 1
		pick := func(single, several string) string {
			if one {
				return single
			}
			return several
		}
		if s.Has("copilot") {
			ns = append(ns, spec.Diagnostic{File: kb.CopilotWorkflowPath, Severity: spec.Note, Code: "B001",
				Message: "set " + names + pick(" as a secret", " as secrets") + " in the repository's copilot environment (Settings, Environments, copilot)"})
		}
		if s.Has("cursor") {
			ns = append(ns, spec.Diagnostic{File: kb.CursorEnvPath, Severity: spec.Note, Code: "B002",
				Message: "set " + names + " in the Secrets tab of Cursor's cloud agent settings"})
		}
		if s.Has("devcontainer") {
			ns = append(ns, spec.Diagnostic{File: kb.DevcontainerPath, Severity: spec.Note, Code: "B003",
				Message: "devcontainer.json lists " + names + pick(" as a recommended secret; Codespaces asks for it, local containers read it", " as recommended secrets; Codespaces asks for them, local containers read them") + " from the environment"})
		}
		if s.Has("claude-code") {
			ns = append(ns, spec.Diagnostic{File: kb.ClaudeSettingsPath, Severity: spec.Note, Code: "B012",
				Message: "set " + names + " in the Claude Code environment's environment variables (anyone using the environment can read them), or as a network secret when only an HTTPS API needs " + pick("it", "them")})
		}
		if s.Has("codex") {
			ns = append(ns, spec.Diagnostic{File: kb.CodexPath, Severity: spec.Note, Code: "B013",
				Message: "set " + names + pick(" as a secret", " as secrets") + " in the Codex environment's settings"})
		}
		if s.Has("ona") {
			ns = append(ns, spec.Diagnostic{File: kb.OnaConfigPath, Severity: spec.Note, Code: "B014",
				Message: "set " + names + pick(" as an Ona secret", " as Ona secrets") + " (project or user, as environment variables)"})
		}
		if s.Has("devin") {
			ns = append(ns, spec.Diagnostic{File: kb.DevinPath, Severity: spec.Note, Code: "B015",
				Message: "set " + names + " in Devin's Secrets settings; the blueprint's build steps read " + pick("it", "them") + " as $" + s.Secrets[0] + pick("", " and so on")})
		}
		if s.Has("script") || s.Has("cloud-init") {
			ns = append(ns, spec.Diagnostic{File: kb.ScriptPath, Severity: spec.Note, Code: "B004",
				Message: "the setup script expects " + names + " in its environment and warns when " + pick("it is", "one is") + " missing"})
		}
	}
	if s.Has("claude-code") {
		msg := "the first cloud session sets up the machine from the SessionStart hook; the setup script reaches nothing outside the Trusted network level's list that preconfig knows of"
		if hosts := extraHosts(s); len(hosts) > 0 {
			msg = "the first cloud session sets up the machine from the SessionStart hook; set the environment's network to Custom, with the default list included, and add " + andList(hosts)
		}
		ns = append(ns, spec.Diagnostic{File: kb.ClaudeSettingsPath, Severity: spec.Note, Code: "B016", Message: msg})
	}
	if s.Has("codex") {
		ns = append(ns, spec.Diagnostic{File: kb.CodexPath, Severity: spec.Note, Code: "B017",
			Message: "Codex has no setup file; enter what " + kb.CodexPath + " lists in the environment's settings"})
	}
	if s.Has("devin") {
		ns = append(ns, spec.Diagnostic{File: kb.DevinPath, Severity: spec.Note, Code: "B018",
			Message: "after committing it, sync the blueprint in Devin (Settings, Environment, Blueprints, or the API) and build"})
	}
	if s.Has("ona") && len(s.Services) > 0 {
		ns = append(ns, spec.Diagnostic{File: kb.ComposePath, Severity: spec.Note, Code: "B019",
			Message: "compose.yaml puts every service on the host's network, as Ona requires; on Docker Desktop that needs host networking turned on (Settings, Resources, Network)"})
	}
	if s.Has("cloud-init") && s.Repo == "" {
		ns = append(ns, spec.Diagnostic{File: kb.CloudInitPath, Severity: spec.Note, Code: "B011",
			Message: "preconfig.yaml has no repo, so cloud-init prepares the machine but doesn't clone or set up the project"})
	}
	return ns
}

// andList joins names as "A", "A and B" or "A, B and C".
func andList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// Header is the first comment line of every generated file that can hold one.
func Header(comment string) string {
	return fmt.Sprintf("%s Generated by preconfig %s from preconfig.yaml. Edit preconfig.yaml, not this file.\n", comment, kb.Version)
}

// Summary describes what a spec sets up, such as "python 3.12, postgres 16".
func Summary(s *spec.Spec) string {
	var parts []string
	if s.Node != "" {
		parts = append(parts, "node "+s.Node)
	}
	if s.Python != "" {
		parts = append(parts, "python "+s.Python)
	}
	if s.Go != "" {
		parts = append(parts, "go "+s.Go)
	}
	for _, t := range s.Tools {
		parts = append(parts, t.String())
	}
	for _, sv := range s.Services {
		parts = append(parts, sv.Name+" "+sv.Version)
	}
	if len(parts) == 0 {
		return "no runtimes"
	}
	return strings.Join(parts, ", ")
}

// ---- quoting ----

// sh quotes s for a POSIX shell.
func sh(s string) string {
	if s != "" && safeShell.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var safeShell = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// yq renders s as a YAML scalar that reads back as the same string.
func yq(s string) string { return tree.YAMLString(s) }

// block renders a multi-line script as a YAML literal block at the given indent.
func block(lines []string, indent string) string {
	var b strings.Builder
	b.WriteString("|\n")
	for _, l := range lines {
		if l == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(indent)
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

// joinCommands chains commands so that the first failure stops the rest.
func joinCommands(cmds []string) string {
	parts := make([]string, len(cmds))
	for i, c := range cmds {
		if strings.Contains(c, "||") || strings.Contains(c, ";") {
			parts[i] = "(" + c + ")"
		} else {
			parts[i] = c
		}
	}
	return strings.Join(parts, " && ")
}
