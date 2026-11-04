package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/jsonc"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/yaml"
)

const agentTargets = "targets: [claude-code, codex, ona, devin]\n"

func withTargets(t *testing.T, name, targets string) map[string]File {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoDir(name), "preconfig.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if i := strings.Index(src, "\ntargets:"); i >= 0 {
		src = src[:i+1]
	}
	files := map[string]File{}
	for _, f := range Build(load(t, src+targets)).Files {
		files[f.Path] = f
	}
	return files
}

// Every sample builds the four agent targets, each in a form its platform
// reads: JSON for Claude Code, YAML for Ona and Devin, bash for the hook.
func TestAgentTargets(t *testing.T) {
	needBash(t)
	for _, name := range repos {
		files := withTargets(t, name, agentTargets)
		for _, p := range []string{kb.ClaudeSettingsPath, kb.ClaudeScriptPath, kb.CodexPath, kb.OnaConfigPath, kb.DevinPath, kb.ScriptPath, kb.DevcontainerPath} {
			if _, ok := files[p]; !ok {
				t.Fatalf("%s: %s not written", name, p)
			}
		}
		root, err := jsonc.Parse(files[kb.ClaudeSettingsPath].Content, jsonc.Options{})
		if err != nil {
			t.Fatalf("%s: settings.json: %v", name, err)
		}
		hook := root.Path("hooks", "SessionStart").Items[0]
		if hook.Get("matcher").Str() != kb.ClaudeHookMatcher || hook.Get("hooks").Items[0].Get("command").Str() != ClaudeHookCommand {
			t.Errorf("%s: hook %s", name, files[kb.ClaudeSettingsPath].Content)
		}
		for _, p := range []string{kb.OnaConfigPath, kb.DevinPath} {
			if _, err := yaml.Parse(files[p].Content); err != nil {
				t.Errorf("%s: %s: %v", name, p, err)
			}
		}
		dir := t.TempDir()
		sp := filepath.Join(dir, "claude-code.sh")
		os.WriteFile(sp, []byte(files[kb.ClaudeScriptPath].Content), 0o755)
		if out, err := exec.Command("bash", "-n", sp).CombinedOutput(); err != nil {
			t.Errorf("%s: bash -n claude-code.sh: %v\n%s", name, err, out)
		}
		if !strings.Contains(files[kb.CodexPath].Content, "bash .preconfig/setup.sh machine") {
			t.Errorf("%s: codex.md lacks the install script", name)
		}
		if files[kb.ClaudeSettingsPath].Merge != MergeClaude {
			t.Errorf("%s: settings.json isn't merged", name)
		}
	}
}

// Ona needs host networking on every Compose service; other builds keep
// the services on app's network.
func TestOnaComposeNetwork(t *testing.T) {
	ona := withTargets(t, "orders-api", "targets: [ona]\n")
	c := ona[kb.ComposePath].Content
	if strings.Contains(c, "service:app") || strings.Count(c, "network_mode: host") != 3 {
		t.Errorf("compose with ona:\n%s", c)
	}
	plain := withTargets(t, "orders-api", "targets: [devcontainer]\n")
	if strings.Contains(plain[kb.ComposePath].Content, "network_mode: host") {
		t.Error("host networking without ona")
	}
}

func TestMergeClaudeSettings(t *testing.T) {
	gen := withTargets(t, "orders-api", "targets: [claude-code]\n")[kb.ClaudeSettingsPath].Content
	if got, err := MergeClaudeSettings(gen, "", false); err != nil || got != gen {
		t.Fatalf("no file: %v", err)
	}
	existing := `{
  // a comment
  "permissions": {"allow": ["Bash(npm test)"]},
  "env": {"DATABASE_URL": "old", "KEEP": "1"},
  "hooks": {
    "SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "echo mine"}]},
      {"matcher": "startup", "hooks": [{"type": "command", "command": "bash .preconfig/claude-code.sh"}]}
    ],
    "Stop": [{"hooks": [{"type": "command", "command": "echo stop"}]}]
  },
}`
	got, err := MergeClaudeSettings(gen, existing, true)
	if err != nil {
		t.Fatal(err)
	}
	root, err := jsonc.Parse(got, jsonc.Options{})
	if err != nil {
		t.Fatalf("merged file isn't JSON: %v\n%s", err, got)
	}
	if root.Path("permissions", "allow").Strings()[0] != "Bash(npm test)" || root.Path("env", "KEEP").Str() != "1" || root.Path("hooks", "Stop") == nil {
		t.Errorf("the file's own settings weren't kept:\n%s", got)
	}
	// preconfig's env goes to cloud sessions only, through the hook, never
	// into settings.json, which local sessions read too.
	if root.Path("env", "DATABASE_URL").Str() != "old" || strings.Contains(gen, "DATABASE_URL") {
		t.Errorf("env in settings.json:\n%s", got)
	}
	ss := root.Path("hooks", "SessionStart").Items
	if len(ss) != 2 || ss[0].Get("hooks").Items[0].Get("command").Str() != "echo mine" || ss[1].Get("hooks").Items[0].Get("command").Str() != ClaudeHookCommand {
		t.Errorf("SessionStart: the old preconfig hook should be replaced, the other kept:\n%s", got)
	}
	again, _ := MergeClaudeSettings(gen, got, true)
	if again != got {
		t.Error("merging twice changed the file")
	}
	// A user's entry that also runs preconfig's script loses only that hook.
	mixed := `{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "bash .preconfig/claude-code.sh"}, {"type": "command", "command": "echo mine"}]}]}}`
	got, _ = MergeClaudeSettings(gen, mixed, true)
	if strings.Count(got, kb.ClaudeScriptPath) != 1 || !strings.Contains(got, "echo mine") {
		t.Errorf("mixed entry:\n%s", got)
	}
	// A key that appears twice keeps its last value, as Claude Code reads it.
	got, err = MergeClaudeSettings(gen, `{"model": "a", "model": "b"}`, true)
	if err != nil || !strings.Contains(got, `"model": "b"`) || strings.Contains(got, `"a"`) {
		t.Errorf("duplicate key: %v\n%s", err, got)
	}
	for _, bad := range []string{`[1]`, `{"hooks": 3}`, `{"hooks": {"SessionStart": {}}}`, `{"a":`} {
		if _, err := MergeClaudeSettings(gen, bad, true); err == nil {
			t.Errorf("merged into %s", bad)
		}
	}
}

// The hook does nothing outside Claude Code's cloud sessions; in them it
// runs machine and project once per version of the setup script, and the
// services every time.
func TestClaudeHook(t *testing.T) {
	needBash(t)
	files := withTargets(t, "orders-api", "targets: [claude-code]\n")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".preconfig"), 0o755)
	os.WriteFile(filepath.Join(dir, kb.ClaudeScriptPath), []byte(files[kb.ClaudeScriptPath].Content), 0o755)
	calls := filepath.Join(dir, "calls")
	stub := "echo \"$1\" >> " + calls + "\n"
	os.WriteFile(filepath.Join(dir, kb.ScriptPath), []byte(stub), 0o755)
	run := func(remote bool) (string, int) {
		cmd := exec.Command("bash", "-c", `bash "$CLAUDE_PROJECT_DIR"/`+kb.ClaudeScriptPath)
		env := []string{"CLAUDE_PROJECT_DIR=" + dir, "XDG_STATE_HOME=" + filepath.Join(dir, "state")}
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "CLAUDE_CODE_REMOTE=") {
				env = append(env, kv)
			}
		}
		if remote {
			env = append(env, "CLAUDE_CODE_REMOTE=true")
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return string(out), code
	}
	if out, code := run(false); code != 0 || out != "" {
		t.Fatalf("outside the cloud: %d %q", code, out)
	}
	if _, err := os.Stat(calls); err == nil {
		t.Fatal("outside the cloud the setup script ran")
	}
	if out, code := run(true); code != 0 || !strings.Contains(out, "orders-api is set up") {
		t.Fatalf("first session: %d %q", code, out)
	}
	run(true)
	b, _ := os.ReadFile(calls)
	if string(b) != "machine\nproject\nservices\nservices\n" {
		t.Errorf("calls after two sessions: %q", b)
	}
	// A new setup script runs machine and project again.
	os.WriteFile(filepath.Join(dir, kb.ScriptPath), []byte(stub+"# changed\n"), 0o755)
	run(true)
	b, _ = os.ReadFile(calls)
	if !strings.HasSuffix(string(b), "machine\nproject\nservices\n") {
		t.Errorf("after a change: %q", b)
	}
	// A failing step tells Claude, on standard output, which step and why;
	// the session still starts. The log holds this run only.
	os.WriteFile(filepath.Join(dir, kb.ScriptPath), []byte("echo boom-$1; exit 3\n"), 0o755)
	out, code := run(true)
	if code != 0 || !strings.Contains(out, "machine step") || !strings.Contains(out, "boom-machine") {
		t.Errorf("failing step: %d %q", code, out)
	}
	out, _ = run(true)
	if strings.Count(out, "boom-machine") != 1 {
		t.Errorf("the log kept an earlier run: %q", out)
	}
	// No setup script: a message, not a crash.
	os.Remove(filepath.Join(dir, kb.ScriptPath))
	if out, code := run(true); code != 0 || !strings.Contains(out, "missing") {
		t.Errorf("no setup script: %d %q", code, out)
	}
}

// The project's environment reaches a cloud session's later commands
// through CLAUDE_ENV_FILE, quoted so that any value survives.
func TestClaudeHookEnv(t *testing.T) {
	needBash(t)
	s := load(t, "version: 1\nname: app\nruntimes: {go: \"1.24\"}\nenv:\n  JAVA_OPTS: \"-Xmx1g -Dx=1\"\n  ODD: \"it's $HOME `x` \\\\n\"\ntargets: [claude-code]\n")
	var script string
	for _, f := range Build(s).Files {
		if f.Path == kb.ClaudeScriptPath {
			script = f.Content
		}
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".preconfig"), 0o755)
	os.WriteFile(filepath.Join(dir, kb.ClaudeScriptPath), []byte(script), 0o755)
	os.WriteFile(filepath.Join(dir, kb.ScriptPath), []byte("true\n"), 0o755)
	envFile := filepath.Join(dir, "env.sh")
	cmd := exec.Command("bash", filepath.Join(dir, kb.ClaudeScriptPath))
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_REMOTE=true", "CLAUDE_PROJECT_DIR="+dir, "XDG_STATE_HOME="+dir+"/state", "CLAUDE_ENV_FILE="+envFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	out, err := exec.Command("bash", "-c", `. "$1"; printf '%s|%s|%s' "$JAVA_OPTS" "$ODD" "$GOTOOLCHAIN"`, "_", envFile).Output()
	if err != nil || string(out) != "-Xmx1g -Dx=1|it's $HOME `x` \\n|go1.24.0+auto" {
		t.Errorf("env file read back as %q (%v)", out, err)
	}
}

// Devin's $ENVRC is read as shell: values with spaces, quotes and $ survive.
func TestDevinEnvrc(t *testing.T) {
	needBash(t)
	s := load(t, "version: 1\nname: app\nenv:\n  JAVA_OPTS: \"-Xmx1g -Dx=1\"\n  ODD: \"it's $HOME\"\ntargets: [devin]\n")
	var bp string
	for _, f := range Build(s).Files {
		if f.Path == kb.DevinPath {
			bp = f.Content
		}
	}
	root, err := yaml.Parse(bp)
	if err != nil {
		t.Fatal(err)
	}
	run := root.Get("initialize").Items[1].Get("run").Str()
	envrc := filepath.Join(t.TempDir(), "envrc")
	cmd := exec.Command("bash", "-c", run+"\n. \"$ENVRC\"; printf '%s|%s' \"$JAVA_OPTS\" \"$ODD\"")
	cmd.Env = append(os.Environ(), "ENVRC="+envrc)
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "-Xmx1g -Dx=1|it's $HOME" {
		t.Errorf("$ENVRC read back as %q (%v)", out, err)
	}
}

// The Claude Code hook only names hosts outside its Trusted list.
func TestExtraHosts(t *testing.T) {
	s := load(t, "version: 1\nname: x\nruntimes: {node: \"22\"}\nservices: {postgres: \"17\", redis: \"7\"}\n")
	if got := strings.Join(extraHosts(s), ","); got != "apt.postgresql.org,deb.nodesource.com,www.postgresql.org" {
		t.Errorf("hosts %s", got)
	}
}
