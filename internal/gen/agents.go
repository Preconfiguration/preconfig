package gen

import (
	"fmt"
	"sort"
	"strings"

	"preconfiguration.com/preconfig/internal/jsonc"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
)

// The targets for agent platforms whose machines are set up partly in the
// platform's own settings: Claude Code cloud sessions, Codex, Ona and Devin.
// Each runs the same .preconfig/setup.sh steps as the other targets.

// ---- Claude Code ----

// ClaudeHookCommand is the SessionStart hook preconfig adds to
// .claude/settings.json.
var ClaudeHookCommand = `bash "$CLAUDE_PROJECT_DIR"/` + kb.ClaudeScriptPath

// claudeCode writes .claude/settings.json (a SessionStart hook and the
// spec's environment) and the script the hook runs.
func claudeCode(s *spec.Spec) []File {
	root := tree.NewMap()
	root.Set("hooks", tree.NewMap().Set("SessionStart", seq(claudeHookEntry())))
	settings := File{Path: kb.ClaudeSettingsPath, Target: "claude-code", Content: tree.JSON(root, "  "), Merge: MergeClaude}

	var b sb
	b.line("#!/usr/bin/env bash")
	b.WriteString(Header("#"))
	b.line("# Run by the SessionStart hook in %s, at the start and resume of every", kb.ClaudeSettingsPath)
	b.line("# Claude Code session. It does nothing outside Claude Code's cloud sessions,")
	b.line("# so a session on your own machine is left alone. In the cloud it sets up the")
	b.line("# machine and the project once for each version of %s, and", kb.ScriptPath)
	b.line("# starts the services every session, since a session's VM keeps files but")
	b.line("# not running processes. Its output goes to a log; Claude reads one line, or")
	b.line("# the failure and the end of the log, which is what a SessionStart hook's")
	b.line("# standard output is for. A failure doesn't stop the session.")
	b.line("# Re-run project by hand after changing dependencies: bash %s project", kb.ScriptPath)
	b.line("set -Euo pipefail")
	b.line(`if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then`)
	b.line("  exit 0")
	b.line("fi")
	b.line(`project="${CLAUDE_PROJECT_DIR:-$(dirname "$0")/..}"`)
	b.line(`if ! cd "$project"; then`)
	b.line(`  echo "preconfig: can't enter the project folder $project, so the machine wasn't set up"`)
	b.line(`  exit 0`)
	b.line(`fi`)
	if envs := sessionEnv(s); len(envs) > 0 {
		b.line(`# The project's environment, for Claude's later commands in this session.`)
		b.line(`if [ -n "${CLAUDE_ENV_FILE:-}" ]; then`)
		b.line(`  {`)
		for _, e := range envs {
			b.line(`    echo %s`, sh("export "+e.Key+"="+sh(e.Value)))
		}
		b.line(`  } >>"$CLAUDE_ENV_FILE"`)
		b.line(`fi`)
	}
	b.line(`if [ ! -f %s ]; then`, kb.ScriptPath)
	b.line(`  echo "preconfig: %s is missing, so the machine wasn't set up; run preconfig build"`, kb.ScriptPath)
	b.line(`  exit 0`)
	b.line(`fi`)
	b.line(`sum=$(sha256sum %s | cut -d' ' -f1)`, kb.ScriptPath)
	b.line(`dirsum=$(pwd | sha256sum | cut -c1-16)`)
	b.line(`if [ -z "$sum" ] || [ -z "$dirsum" ]; then`)
	b.line(`  echo "preconfig: couldn't fingerprint %s (sha256sum failed), so the machine wasn't set up"`, kb.ScriptPath)
	b.line(`  exit 0`)
	b.line(`fi`)
	b.line(`state="${XDG_STATE_HOME:-${HOME:-/tmp}/.local/state}/preconfig/$dirsum"`)
	b.line(`log="$state/claude-code.log"`)
	b.line(`mkdir -p "$state"`)
	b.line(`: >"$log"`)
	b.line(`step() {`)
	b.line(`  if ! bash %s "$1" >>"$log" 2>&1; then`, kb.ScriptPath)
	b.line(`    echo "preconfig: the $1 step of %s failed, so the machine isn't fully set up. The end of $log:"`, kb.ScriptPath)
	b.line(`    tail -n 20 "$log"`)
	b.line(`    exit 0`)
	b.line(`  fi`)
	b.line(`}`)
	b.line(`for phase in machine project; do`)
	b.line(`  if [ "$(cat "$state/$phase" 2>/dev/null)" != "$sum" ]; then`)
	b.line(`    step "$phase"`)
	b.line(`    echo "$sum" >"$state/$phase"`)
	b.line(`  fi`)
	b.line(`done`)
	if len(s.Services) > 0 {
		b.line(`step services`)
	}
	b.line(`echo "preconfig: %s is set up (%s); ready check: %s"`, shellEscapeDouble(s.Name), shellEscapeDouble(Summary(s)), shellEscapeDouble(readyText(s)))
	script := File{Path: kb.ClaudeScriptPath, Target: "claude-code", Content: b.String(), Mode: "0755"}
	return []File{settings, script}
}

func claudeHookEntry() *tree.Node {
	hook := tree.NewMap().
		Set("type", tree.NewStr("command")).
		Set("command", tree.NewStr(ClaudeHookCommand)).
		Set("timeout", &tree.Node{Kind: tree.Scalar, Value: fmt.Sprint(kb.ClaudeHookTimeout), Type: tree.Number})
	return tree.NewMap().
		Set("matcher", tree.NewStr(kb.ClaudeHookMatcher)).
		Set("hooks", seq(hook))
}

// sessionEnv is the environment a session's own commands need: the spec's
// env, and GOTOOLCHAIN for Go, as the setup script exports them.
func sessionEnv(s *spec.Spec) []spec.EnvVar {
	var out []spec.EnvVar
	if s.Go != "" {
		out = append(out, spec.EnvVar{Key: "GOTOOLCHAIN", Value: goToolchain(s) + "+auto"})
	}
	return append(out, s.Env...)
}

func readyText(s *spec.Spec) string {
	if len(s.Ready) == 0 {
		return "none"
	}
	return strings.Join(s.Ready, " && ")
}

// MergeClaudeSettings puts preconfig's SessionStart hook into an existing
// .claude/settings.json, in place of any earlier one. Everything else in the
// file is kept (comments aside: the file is written back as plain JSON).
// With no existing file, the generated one is the result.
func MergeClaudeSettings(generated, existing string, exists bool) (string, error) {
	if !exists || strings.TrimSpace(existing) == "" {
		return generated, nil
	}
	root, err := parseJSON(existing)
	if err != nil {
		return "", fmt.Errorf("%s can't be read, so preconfig can't add its hook: %v", kb.ClaudeSettingsPath, err)
	}
	if root.Kind != tree.Map {
		return "", fmt.Errorf("%s should hold an object", kb.ClaudeSettingsPath)
	}
	hooks := root.Get("hooks")
	if hooks == nil {
		hooks = tree.NewMap()
		root.Set("hooks", hooks)
	} else if hooks.Kind != tree.Map {
		return "", fmt.Errorf("%s: hooks should be an object", kb.ClaudeSettingsPath)
	}
	var entries []*tree.Node
	if ss := hooks.Get("SessionStart"); ss != nil {
		if ss.Kind != tree.Seq {
			return "", fmt.Errorf("%s: hooks.SessionStart should be a list", kb.ClaudeSettingsPath)
		}
		for _, e := range ss.Items {
			if e = withoutPreconfigHooks(e); e != nil {
				entries = append(entries, e)
			}
		}
	}
	entries = append(entries, claudeHookEntry())
	hooks.Set("SessionStart", seq(entries...))
	return tree.JSON(root, "  "), nil
}

// withoutPreconfigHooks returns a SessionStart entry without the hooks that
// run preconfig's script, or nil when none are left.
func withoutPreconfigHooks(e *tree.Node) *tree.Node {
	if e == nil || e.Kind != tree.Map {
		return e
	}
	hs := e.Get("hooks")
	if hs == nil || hs.Kind != tree.Seq {
		return e
	}
	var keep []*tree.Node
	for _, h := range hs.Items {
		if !strings.Contains(h.Get("command").Str(), kb.ClaudeScriptPath) {
			keep = append(keep, h)
		}
	}
	if len(keep) == len(hs.Items) {
		return e
	}
	if len(keep) == 0 {
		return nil
	}
	out := tree.NewMap()
	for i, k := range e.Keys {
		out.Set(k.Value, e.Values[i])
	}
	return out.Set("hooks", seq(keep...))
}

func seq(items ...*tree.Node) *tree.Node { return &tree.Node{Kind: tree.Seq, Items: items} }

// parseJSON reads a JSON settings file as Claude Code would: a key that
// appears twice has its last value. Comments and trailing commas are
// tolerated, and dropped when the file is written back.
func parseJSON(text string) (*tree.Node, error) {
	return jsonc.Parse(text, jsonc.Options{Comments: true, TrailingCommas: true, LastDuplicate: true})
}

// ---- Codex ----

// codex writes .preconfig/codex.md: what to put in the Codex environment's
// settings, since no repository file configures it. Codex has two kinds of
// environment: the current one, where Codex records an install script and a
// start step, and Codex Cloud (Legacy), with a setup and a maintenance script.
func codex(s *spec.Spec) File {
	var b sb
	b.line("<!-- Generated by preconfig %s from preconfig.yaml. Edit preconfig.yaml, not this file. -->", kb.Version)
	b.line("")
	b.line("# Codex cloud environment for %s", s.Name)
	b.line("")
	b.line("Codex sets environments up in its own settings, not from a file in the")
	b.line("repository. These are the values for %s: %s.", s.Name, Summary(s))
	b.line("")
	b.line("## Install script")
	b.line("")
	b.line("In Codex Cloud (Legacy), the setup script. Runs from the repository's root")
	b.line("folder when the environment is prepared:")
	b.line("")
	b.line("```bash")
	b.line("bash %s machine", kb.ScriptPath)
	b.line("bash %s project", kb.ScriptPath)
	b.line("```")
	b.line("")
	b.line("## Maintenance script (Codex Cloud, Legacy)")
	b.line("")
	b.line("Runs when a cached container is resumed on a newer commit:")
	b.line("")
	b.line("```bash")
	b.line("bash %s project", kb.ScriptPath)
	b.line("```")
	b.line("")
	if len(s.Services) > 0 {
		b.line("## Starting the services")
		b.line("")
		b.line("A prepared environment keeps files, not running processes. This is the")
		b.line("start step; in Codex Cloud (Legacy), add it to the end of both scripts:")
		b.line("")
		b.line("```bash")
		b.line("bash %s services", kb.ScriptPath)
		b.line("```")
		b.line("")
	}
	b.line("## Ready check")
	b.line("")
	if len(s.Ready) == 0 {
		b.line("preconfig.yaml has no ready check.")
	} else {
		b.line("Codex finds test commands in AGENTS.md. The setup is ready when this passes:")
		b.line("")
		b.line("```bash")
		b.line("bash %s ready", kb.ScriptPath)
		b.line("```")
	}
	b.line("")
	b.line("## Environment variables")
	b.line("")
	if envs := sessionEnv(s); len(envs) == 0 {
		b.line("None.")
	} else {
		b.line("Codex passes these to every command, setup included:")
		b.line("")
		b.line("```")
		for _, e := range envs {
			b.line("%s=%s", e.Key, e.Value)
		}
		b.line("```")
	}
	b.line("")
	if len(s.Secrets) > 0 {
		b.line("## Secrets")
		b.line("")
		b.line("%s. Never put them in this file. A key that only goes to an HTTPS API can", andList(s.Secrets))
		b.line("be a network secret: commands get a placeholder, and Codex's proxy puts the")
		b.line("real value in on the way out. In Codex Cloud (Legacy), secrets reach only the")
		b.line("setup script and are removed before the agent works, so the ready check and")
		b.line("the tests don't see them.")
		b.line("")
	}
	b.line("## Internet access")
	b.line("")
	b.line("The install script needs to reach Ubuntu's archives and the package")
	if hosts := extraHosts(s); len(hosts) > 0 {
		b.line("registries (the package-manager preset), plus %s.", strings.Join(hosts, ", "))
	} else {
		b.line("registries (the package-manager preset).")
	}
	b.line("In Codex Cloud (Legacy), setup always has internet access, and the agent's")
	b.line("access is a separate setting.")
	return File{Path: kb.CodexPath, Target: "codex", Content: b.String()}
}

// extraHosts are the hosts the setup script reaches beyond the common
// package registries and Ubuntu's own archives.
func extraHosts(s *spec.Spec) []string {
	set := map[string]bool{}
	if s.Node != "" {
		set["deb.nodesource.com"] = true
	}
	for _, sv := range s.Services {
		if needsVendorRepo(s, sv) {
			switch sv.Name {
			case "postgres":
				set["apt.postgresql.org"] = true
				set["www.postgresql.org"] = true
			case "redis":
				set["packages.redis.io"] = true
			}
		}
	}
	var out []string
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// needsVendorRepo reports a service whose version Ubuntu doesn't ship, so
// the setup script adds the vendor's apt repository.
func needsVendorRepo(s *spec.Spec, sv spec.Service) bool {
	switch sv.Name {
	case "postgres":
		return fmt.Sprint(sv.Major) != s.Base.DistroPostgres
	case "redis":
		return fmt.Sprint(sv.Major) != s.Base.DistroRedis
	}
	return false
}

// ---- Ona ----

// ona writes .ona/config.yaml: the ready check as a task Ona's agent and
// editor can run. The machine itself is the dev container.
func ona(s *spec.Spec) File {
	var b sb
	b.WriteString(Header("#"))
	b.line("# Ona builds the dev container from %s, which installs the", kb.DevcontainerPath)
	b.line("# runtimes and runs the setup commands. This file adds Ona's tasks.")
	if len(s.Services) > 0 {
		b.line("# The services run in %s, on the host network, as Ona requires.", kb.ComposePath)
	}
	if len(s.Secrets) > 0 {
		b.line("# Secrets (%s) come from Ona's project or user secrets.", strings.Join(s.Secrets, ", "))
	}
	b.line("tasks:")
	b.line("  ready-check:")
	b.line("    name: Ready check")
	b.line("    description: %s", yq("Proves the setup: "+readyText(s)))
	if len(s.Ready) == 0 {
		b.line("    command: %s", yq("echo 'preconfig.yaml has no ready check'"))
	} else {
		b.line("    command: %s", yq(joinCommands(s.Ready)))
	}
	b.line("    triggeredBy:")
	b.line("      - manual")
	return File{Path: kb.OnaConfigPath, Target: "ona", Content: b.String()}
}

// ---- Devin ----

// devin writes .devin/blueprint.yaml. Devin builds a snapshot from it after
// it is synced in Devin's settings; initialize and maintenance run then, from
// the repository's root, and nothing runs at session start, so the services
// and the ready check are knowledge Devin reads.
func devin(s *spec.Spec) File {
	var b sb
	b.WriteString(Header("#"))
	b.line("# Devin builds its machine from this blueprint once it is synced (Settings,")
	b.line("# Environment, Blueprints, or the API). The build runs initialize and")
	b.line("# maintenance from the repository's root; nothing runs at session start.")
	if len(s.Secrets) > 0 {
		b.line("# Secrets (%s) come from Devin's Secrets settings; the build reads them as $NAME.", strings.Join(s.Secrets, ", "))
	}
	b.line("initialize:")
	b.line("  - name: Machine (preconfig)")
	b.line("    run: %s", yq("bash "+kb.ScriptPath+" machine"))
	if envs := sessionEnv(s); len(envs) > 0 {
		var lines []string
		for _, e := range envs {
			lines = append(lines, fmt.Sprintf(`echo %s >> "$ENVRC"`, sh("export "+e.Key+"="+sh(e.Value))))
		}
		b.line("  - name: Environment for later steps and sessions")
		b.WriteString("    run: " + block(lines, "      "))
	}
	b.line("maintenance:")
	b.line("  - name: Project setup (preconfig)")
	b.line("    run: %s", yq("bash "+kb.ScriptPath+" project"))
	b.line("knowledge:")
	if len(s.Services) > 0 {
		var names []string
		for _, sv := range s.Services {
			names = append(names, sv.Name+" "+sv.Version)
		}
		b.line("  - name: services")
		b.line("    contents: %s", yq(fmt.Sprintf("Start the services (%s) at the start of each session, before running tests: bash %s services", strings.Join(names, ", "), kb.ScriptPath)))
	}
	b.line("  - name: test")
	if len(s.Ready) == 0 {
		b.line("    contents: %s", yq("preconfig.yaml has no ready check."))
	} else {
		b.line("    contents: %s", yq("bash "+kb.ScriptPath+" ready (runs: "+readyText(s)+")"))
	}
	return File{Path: kb.DevinPath, Target: "devin", Content: b.String()}
}
