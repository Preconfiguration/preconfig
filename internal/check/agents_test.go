package check

import (
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/gen"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
)

func checkOne(path, text string) Report {
	return Check(Input{Files: map[string]string{path: text}})
}

func hasCode(r Report, code string) bool {
	_, ok := find(r, code)
	return ok
}

func TestOnaRules(t *testing.T) {
	good := "tasks:\n  install:\n    name: Install\n    command: npm ci\n    triggeredBy: [postDevcontainerStart]\n  test:\n    name: Test\n    command: npm test\n    dependsOn: [install]\nservices:\n  db:\n    name: DB\n    commands:\n      start: postgres\n"
	if r := checkOne(kb.OnaConfigPath, good); len(r.Findings) != 0 {
		t.Errorf("good config: %v", codes(r))
	}
	cases := map[string]string{
		"O001": "tasks: [\n",
		"O002": "steps: {}\n",
		"O003": "tasks:\n  \"bad id\":\n    name: X\n    command: x\n",
		"O004": "tasks:\n  a:\n    name: A\n",
		"O005": "tasks:\n  a:\n    name: A\n    command: x\n    triggeredBy: [postMachineStart]\n",
		"O006": "tasks:\n  a:\n    name: A\n    command: x\n    dependsOn: [b]\n  b:\n    name: B\n    command: y\n    dependsOn: [a]\n",
	}
	for code, src := range cases {
		if r := checkOne(kb.OnaConfigPath, src); !hasCode(r, code) {
			t.Errorf("%s: got %v", code, codes(r))
		}
	}
	// A missing dependency is O006 too.
	if r := checkOne(kb.OnaConfigPath, "tasks:\n  a:\n    name: A\n    command: x\n    dependsOn: [nope]\n"); !hasCode(r, "O006") {
		t.Errorf("missing dependency: %v", codes(r))
	}
	// Ona reads only the first of its config files.
	r := Check(Input{Files: map[string]string{kb.OnaConfigPath: good, ".gitpod/automations.yaml": good}})
	if d, ok := find(r, "O007"); !ok || d.File != ".gitpod/automations.yaml" {
		t.Errorf("two config files: %v", codes(r))
	}
	if r := checkOne(".gitpod.yml", "tasks: []\n"); !hasCode(r, "O008") {
		t.Errorf(".gitpod.yml: %v", codes(r))
	}
}

func TestDevinRules(t *testing.T) {
	good := "initialize:\n  - name: tools\n    run: sudo apt-get install -y jq\n  - uses: github.com/actions/setup-node@v4\n    with:\n      node-version: 22\nmaintenance: |\n  npm ci\nknowledge:\n  - name: test\n    contents: npm test\n"
	if r := checkOne(kb.DevinPath, good); len(r.Findings) != 0 {
		t.Errorf("good blueprint: %v", codes(r))
	}
	cases := map[string]string{
		"V001": "initialize: [\n",
		"V002": "post-build: echo hi\n",
		"V003": "initialize:\n  - run: a\n    uses: b\n",
		"V004": "maintenance:\n  - uses: github.com/actions/setup-node@v4\n",
		"V005": "knowledge:\n  - name: test\n",
	}
	for code, src := range cases {
		if r := checkOne(kb.DevinPath, src); !hasCode(r, code) {
			t.Errorf("%s: got %v", code, codes(r))
		}
	}
	if r := checkOne(kb.DevinPath, "initialize:\n  - name: x\n    with: {a: 1}\n    run: y\n"); !hasCode(r, "V003") {
		t.Errorf("with without uses: %v", codes(r))
	}
	if r := checkOne(kb.DevinPath, "initalize: x\n"); !hasCode(r, "V002") {
		t.Errorf("misspelt key: %v", codes(r))
	}
}

func TestClaudeSettingsRules(t *testing.T) {
	if r := checkOne(kb.ClaudeSettingsPath, "{\"a\": "); !hasCode(r, "A001") {
		t.Errorf("unreadable: %v", codes(r))
	}
	if r := checkOne(kb.ClaudeSettingsPath, `{"env": {"PORT": 8080}}`); !hasCode(r, "A002") {
		t.Errorf("number in env: %v", codes(r))
	}
	if r := checkOne(kb.ClaudeSettingsPath, `{"hooks": {"SessionStart": {}}}`); !hasCode(r, "A003") {
		t.Errorf("SessionStart not a list: %v", codes(r))
	}
	hook := `{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "bash \"$CLAUDE_PROJECT_DIR\"/.preconfig/claude-code.sh"}]}]}}`
	if r := checkOne(kb.ClaudeSettingsPath, hook); !hasCode(r, "A004") {
		t.Errorf("hook without its script: %v", codes(r))
	}
	r := Check(Input{Files: map[string]string{kb.ClaudeSettingsPath: hook, kb.ClaudeScriptPath: "#!/bin/sh\n"}})
	if hasCode(r, "A004") {
		t.Errorf("script present: %v", codes(r))
	}
}

// A settings.json with the user's own settings beside preconfig's hook isn't
// drift; an older form of the hook is.
func TestClaudeSettingsDrift(t *testing.T) {
	src := "version: 1\nname: app\nruntimes:\n  python: \"3.12\"\nenv:\n  MODE: test\nready:\n  - python3 -c pass\ntargets: [claude-code]\n"
	s, ds := spec.Load(kb.SpecPath, src)
	if s == nil {
		t.Fatal(ds)
	}
	files := map[string]string{kb.SpecPath: src}
	for _, f := range gen.Build(s).Files {
		files[f.Path] = f.Content
	}
	own := `{"permissions": {"allow": ["Bash(make)"]}, "env": {"MINE": "1"}}`
	merged, err := gen.MergeClaudeSettings(files[kb.ClaudeSettingsPath], own, true)
	if err != nil {
		t.Fatal(err)
	}
	files[kb.ClaudeSettingsPath] = merged
	if r := Check(Input{Files: files}); r.Errors != 0 {
		t.Errorf("merged settings: %v", codes(r))
	}
	files[kb.ClaudeSettingsPath] = strings.Replace(merged, `"timeout": 1800`, `"timeout": 600`, 1)
	r := Check(Input{Files: files})
	if d, ok := find(r, "X002"); !ok || d.File != kb.ClaudeSettingsPath || strings.Contains(d.Message, "not written by preconfig") {
		t.Errorf("older hook: %v", r.Findings)
	}
	files[kb.ClaudeSettingsPath] = "[]"
	if r := Check(Input{Files: files}); !hasCode(r, "X020") {
		t.Errorf("settings.json that can't take the hook: %v", codes(r))
	}
	// A key twice is a warning, not an unreadable file (build would write
	// it back once, so it's drift too).
	files[kb.ClaudeSettingsPath] = strings.Replace(merged, "{", `{"model": "a", "model": "b",`, 1)
	r = Check(Input{Files: files})
	if !hasCode(r, "A005") || hasCode(r, "A001") || hasCode(r, "X020") {
		t.Errorf("duplicate key: %v", codes(r))
	}
	// Every repeated key is named, not only the first.
	r = checkOne(kb.ClaudeSettingsPath, `{"model": "a", "hooks": {}, "model": "b", "hooks": {}}`)
	n := 0
	for _, d := range r.Findings {
		if d.Code == "A005" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("two repeated keys gave %d warnings: %v", n, r.Findings)
	}
}
