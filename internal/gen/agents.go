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
	if envs := sessionEnv(s); len(envs) > 0 {
		env := tree.NewMap()
		for _, e := range envs {
			env.Set(e.Key, tree.NewStr(e.Value))
		}
		root.Set("env", env)
	}
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
	b.line("# not running processes. The steps' output goes to a log; Claude sees one line.")
	b.line("set -Eeuo pipefail")
	b.line(`if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then`)
	b.line("  exit 0")
	b.line("fi")
	b.line(`cd "${CLAUDE_PROJECT_DIR:-$(dirname "$0")/..}"`)
	b.line(`state="${XDG_STATE_HOME:-$HOME/.local/state}/preconfig"`)
	b.line(`log="$state/claude-code.log"`)
	b.line(`mkdir -p "$state"`)
	b.line(`sum=$(sha256sum %s | cut -d' ' -f1)`, kb.ScriptPath)
	b.line(`step() {`)
	b.line(`  if ! bash %s "$1" >>"$log" 2>&1; then`, kb.ScriptPath)
	b.line(`    echo "preconfig: the $1 step of %s failed; the end of $log says why" >&2`, kb.ScriptPath)
	b.line(`    tail -n 20 "$log" >&2`)
	b.line(`    exit 1`)
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

// MergeClaudeSettings puts preconfig's parts of .claude/settings.json into
// an existing one: the env values preconfig.yaml sets, and its SessionStart
// hook in place of any earlier one. Everything else in the file is kept.
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
	gen, err := parseJSON(generated)
	if err != nil {
		return "", err
	}
	if genv := gen.Get("env"); genv != nil {
		env := root.Get("env")
		if env == nil {
			env = tree.NewMap()
			root.Set("env", env)
		} else if env.Kind != tree.Map {
			return "", fmt.Errorf("%s: env should be an object", kb.ClaudeSettingsPath)
		}
		for i, k := range genv.Keys {
			env.Set(k.Value, genv.Values[i])
		}
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
			if !isPreconfigHook(e) {
				entries = append(entries, e)
			}
		}
	}
	entries = append(entries, claudeHookEntry())
	hooks.Set("SessionStart", seq(entries...))
	return tree.JSON(root, "  "), nil
}

// isPreconfigHook reports a SessionStart entry whose only hooks run
// preconfig's script.
func isPreconfigHook(e *tree.Node) bool {
	if e == nil || e.Kind != tree.Map {
		return false
	}
	hs := e.Get("hooks")
	if hs == nil || hs.Kind != tree.Seq || len(hs.Items) == 0 {
		return false
	}
	for _, h := range hs.Items {
		if !strings.Contains(h.Get("command").Str(), kb.ClaudeScriptPath) {
			return false
		}
	}
	return true
}

func seq(items ...*tree.Node) *tree.Node { return &tree.Node{Kind: tree.Seq, Items: items} }

// parseJSON reads a JSON settings file (comments and trailing commas are
// tolerated, as Claude Code's own reader does not promise; they are dropped).
func parseJSON(text string) (*tree.Node, error) {
	return jsonc.Parse(text, jsonc.Options{Comments: true, TrailingCommas: true})
}

// ---- Codex ----

// codex writes .preconfig/codex.md: what to put in the Codex environment's
// settings, since no repository file configures it.
func codex(s *spec.Spec) File {
	var b sb
	b.line("<!-- Generated by preconfig %s from preconfig.yaml. Edit preconfig.yaml, not this file. -->", kb.Version)
	b.line("")
	b.line("# Codex cloud environment for %s", s.Name)
	b.line("")
	b.line("Codex sets environments up in its own settings (Codex, Settings,")
	b.line("Environments), not from a file in the repository. These are the values for")
	b.line("%s: %s.", s.Name, Summary(s))
	b.line("")
	b.line("## Install script")
	b.line("")
	b.line("Runs from the repository's root folder when the environment is prepared:")
	b.line("")
	b.line("```bash")
	b.line("bash %s machine", kb.ScriptPath)
	b.line("bash %s project", kb.ScriptPath)
	b.line("```")
	b.line("")
	if len(s.Services) > 0 {
		b.line("## Starting the services")
		b.line("")
		b.line("A prepared environment keeps files, not running processes. Start the")
		b.line("services before the work begins (the start step, or ask Codex to run it):")
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
		b.line("%s. Add each as a secret in the environment's settings, never in this", andList(s.Secrets))
		b.line("file. A key that only goes to an HTTPS API can be a network secret, which")
		b.line("commands never see.")
		b.line("")
	}
	b.line("## Internet access")
	b.line("")
	if hosts := extraHosts(s); len(hosts) > 0 {
		b.line("Setup needs the package-manager preset plus: %s.", strings.Join(hosts, ", "))
	} else {
		b.line("Setup needs the package-manager preset; nothing else.")
	}
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
			lines = append(lines, fmt.Sprintf(`echo %s >> "$ENVRC"`, sh(e.Key+"="+e.Value)))
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
