package check

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/jsonc"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
)

// readRepo reads a repository the way the command does: the known paths, then
// the files they point at.
func readRepo(t *testing.T, dir string) Input {
	t.Helper()
	files := map[string]string{}
	read := func(paths []string) {
		for _, p := range paths {
			if _, done := files[p]; done {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
				files[p] = string(b)
			}
		}
	}
	read(KnownPaths)
	read(Referenced(files))
	return Input{Files: files, Exists: func(p string) bool {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p)))
		return err == nil
	}}
}

// codes lists "CODE@line" for every finding, sorted, for comparisons.
func codes(r Report) []string {
	var out []string
	for _, d := range r.Findings {
		out = append(out, d.Code)
	}
	sort.Strings(out)
	return out
}

func find(r Report, code string) (spec.Diagnostic, bool) {
	for _, d := range r.Findings {
		if d.Code == code {
			return d, true
		}
	}
	return spec.Diagnostic{}, false
}

func TestSampleReposAreClean(t *testing.T) {
	for _, name := range []string{"orders-api", "web-shop", "ingest-worker"} {
		in := readRepo(t, filepath.Join("..", "..", "testdata", "repos", name))
		r := Check(in)
		if len(r.Findings) != 0 || len(r.Diffs) != 0 {
			t.Errorf("%s: want a clean report, got %v", name, r.Findings)
		}
		if !r.HasSpec || r.Checked[0] != kb.SpecPath || len(r.Checked) != 6 {
			t.Errorf("%s: checked %v", name, r.Checked)
		}
	}
}

// The broken fixture is the orders-api spec next to setup files written by
// hand, with the mistakes people make.
func TestBrokenRepo(t *testing.T) {
	r := Check(readRepo(t, filepath.Join("..", "..", "testdata", "check", "broken")))
	want := []string{
		"C002", "C004", "C005", "C007", "K002",
		"X001", "X001", "X001", "X001",
		"X002", "X002", "X002", "X003",
		"X004", "X004", "X004", "X004",
	}
	if got := codes(r); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("codes =\n  %v\nwant\n  %v", got, want)
	}
	if r.Errors != 15 || r.Warnings != 2 {
		t.Errorf("counted %d errors and %d warnings, want 15 and 2", r.Errors, r.Warnings)
	}
	lines := map[string]int{"C002": 4, "C005": 6, "C004": 7, "C007": 2, "K002": 2, "X003": 13}
	for code, line := range lines {
		if d, _ := find(r, code); d.Line != line {
			t.Errorf("%s at line %d, want %d", code, d.Line, line)
		}
	}
	if d, _ := find(r, "C002"); !strings.Contains(d.Hint, `Rename the job "setup"`) {
		t.Errorf("C002 hint = %q", d.Hint)
	}
	if d, _ := find(r, "X003"); !strings.Contains(d.Message, "Python 3.11") || !strings.Contains(d.Message, "3.12") {
		t.Errorf("X003 = %q", d.Message)
	}
	if len(r.Diffs) != 3 {
		t.Errorf("want 3 diffs, got %d", len(r.Diffs))
	}
	for _, d := range r.Diffs {
		if !strings.HasPrefix(d.Diff, "--- a/"+d.Path+"\n+++ b/"+d.Path+" (from preconfig.yaml)\n@@ ") {
			t.Errorf("diff of %s starts %q", d.Path, d.Diff[:min(80, len(d.Diff))])
		}
	}
	// Findings come file by file, preconfig.yaml first, then by line.
	for i := 1; i < len(r.Findings); i++ {
		a, b := r.Findings[i-1], r.Findings[i]
		if fileOrder(a.File) > fileOrder(b.File) || a.File == b.File && a.Line > b.Line {
			t.Errorf("findings out of order: %s:%d before %s:%d", a.File, a.Line, b.File, b.Line)
		}
	}
}

// ---- single files, no spec ----

const goodWorkflow = `name: Copilot setup steps
on:
  workflow_dispatch:
  push:
    paths: [.github/workflows/copilot-setup-steps.yml]
  pull_request:
    paths: [.github/workflows/copilot-setup-steps.yml]
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-24.04
    permissions:
      contents: read
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-node@v7
        with:
          node-version: "22"
`

type fileCase struct {
	name string
	path string
	text string
	code string // "" means no findings at all
	line int
	sev  string
	hint string // a part of the hint or message
	also map[string]string
}

func runFileCases(t *testing.T, cases []fileCase) {
	t.Helper()
	for _, c := range cases {
		files := map[string]string{c.path: c.text}
		for p, s := range c.also {
			files[p] = s
		}
		r := Check(Input{Files: files})
		if c.code == "" {
			if len(r.Findings) != 0 {
				t.Errorf("%s: want no findings, got %v", c.name, r.Findings)
			}
			continue
		}
		var hit *spec.Diagnostic
		for i, d := range r.Findings {
			if d.Code == c.code && (c.line == 0 || d.Line == c.line) {
				hit = &r.Findings[i]
				break
			}
		}
		if hit == nil {
			t.Errorf("%s: want %s at line %d, got %v", c.name, c.code, c.line, r.Findings)
			continue
		}
		if c.sev != "" && hit.Severity != c.sev {
			t.Errorf("%s: %s is %s, want %s", c.name, c.code, hit.Severity, c.sev)
		}
		if c.hint != "" && !strings.Contains(hit.Hint+" "+hit.Message, c.hint) {
			t.Errorf("%s: %s doesn't mention %q: %s / %s", c.name, c.code, c.hint, hit.Message, hit.Hint)
		}
		if r.HasSpec {
			t.Errorf("%s: HasSpec without a spec", c.name)
		}
	}
}

func TestCopilotFindings(t *testing.T) {
	wf := kb.CopilotWorkflowPath
	rep := func(old, new string) string {
		if !strings.Contains(goodWorkflow, old) {
			t.Fatalf("the base workflow has no %q", old)
		}
		return strings.Replace(goodWorkflow, old, new, 1)
	}
	runFileCases(t, []fileCase{
		{name: "good", path: wf, text: goodWorkflow},
		{name: "not yaml", path: wf, text: "jobs: [\n", code: "C001", line: 1, sev: spec.Error},
		{name: "a list", path: wf, text: "- a\n", code: "C001", line: 1, hint: "mapping"},
		{name: "no jobs", path: wf, text: "on: push\n", code: "C002", hint: "copilot-setup-steps"},
		{name: "misnamed", path: wf, text: rep("  copilot-setup-steps:", "  setup:"), code: "C002", line: 9, hint: `Rename the job "setup"`},
		{name: "misnamed job still checked", path: wf, text: rep("  copilot-setup-steps:", "  setup:") + "    env:\n      A: b\n", code: "C004", line: 19},
		{name: "two other jobs", path: wf, text: rep("jobs:\n", "jobs:\n  build:\n    runs-on: x\n    steps: [{run: a}]\n") + "", code: "C003", line: 9, sev: spec.Warning},
		{name: "extra jobs, none right", path: wf, text: strings.Replace(rep("  copilot-setup-steps:", "  setup:"), "jobs:\n", "jobs:\n  lint:\n    steps: [{run: a}]\n", 1), code: "C002", line: 9, hint: "lint, setup"},
		{name: "env ignored", path: wf, text: rep("    permissions:", "    env:\n      A: b\n    permissions:"), code: "C004", line: 11, sev: spec.Warning, hint: "inside a step"},
		{name: "container ignored", path: wf, text: rep("    permissions:", "    container: node:22\n    permissions:"), code: "C004", line: 11, hint: "on the runner itself"},
		{name: "services allowed", path: wf, text: rep("    permissions:", "    services:\n      redis:\n        image: redis:7\n    permissions:")},
		{name: "timeout", path: wf, text: rep("timeout-minutes: 30", "timeout-minutes: 60"), code: "C005", line: 13, hint: "59"},
		{name: "timeout 59", path: wf, text: rep("timeout-minutes: 30", "timeout-minutes: 59")},
		{name: "arm runner", path: wf, text: rep("runs-on: ubuntu-24.04", "runs-on: ubuntu-24.04-arm"), code: "C006", line: 10},
		{name: "mac runner", path: wf, text: rep("runs-on: ubuntu-24.04", "runs-on: [macos-15]"), code: "C006", line: 10},
		{name: "no steps", path: wf, text: strings.SplitAfter(goodWorkflow, "timeout-minutes: 30\n")[0], code: "C008", line: 9},
		{name: "empty steps", path: wf, text: strings.SplitAfter(goodWorkflow, "timeout-minutes: 30\n")[0] + "    steps: []\n", code: "C008", line: 14},
		{name: "job is text", path: wf, text: "on: push\njobs:\n  copilot-setup-steps: hello\n", code: "C008", line: 3},
		{name: "fetch depth", path: wf, text: rep("      - uses: actions/checkout@v7\n", "      - uses: actions/checkout@v7\n        with:\n          fetch-depth: 0\n"), code: "C009", line: 17, sev: spec.Note},
		{name: "wrong place", path: ".github/copilot-setup-steps.yml", text: goodWorkflow, code: "C010", hint: kb.CopilotWorkflowPath},
		{name: "yaml extension", path: ".github/workflows/copilot-setup-steps.yaml", text: goodWorkflow, code: "C010"},
		{name: "no triggers", path: wf, text: rep("on:\n  workflow_dispatch:\n  push:\n    paths: [.github/workflows/copilot-setup-steps.yml]\n  pull_request:\n    paths: [.github/workflows/copilot-setup-steps.yml]\n", ""), code: "C007", sev: spec.Warning, hint: "no triggers"},
		{name: "manual only", path: wf, text: rep("on:\n  workflow_dispatch:\n  push:\n    paths: [.github/workflows/copilot-setup-steps.yml]\n  pull_request:\n    paths: [.github/workflows/copilot-setup-steps.yml]\n", "on: workflow_dispatch\n"), code: "C007", line: 2},
		{name: "push elsewhere", path: wf, text: rep("paths: [.github/workflows/copilot-setup-steps.yml]\n  pull_request:\n    paths: [.github/workflows/copilot-setup-steps.yml]", "paths: [src/**]"), code: "C007", line: 2},
		{name: "unfiltered push", path: wf, text: rep("  push:\n    paths: [.github/workflows/copilot-setup-steps.yml]\n", "  push:\n")},
		{name: "list of events", path: wf, text: rep("on:\n  workflow_dispatch:\n  push:\n    paths: [.github/workflows/copilot-setup-steps.yml]\n  pull_request:\n    paths: [.github/workflows/copilot-setup-steps.yml]\n", "on: [push, workflow_dispatch]\n")},
		{name: "workflows glob", path: wf, text: rep("paths: [.github/workflows/copilot-setup-steps.yml]\n  pull_request:\n    paths: [.github/workflows/copilot-setup-steps.yml]", "paths: [\".github/workflows/**\"]")},
	})
}

func TestCursorFindings(t *testing.T) {
	env := kb.CursorEnvPath
	docker := map[string]string{kb.CursorDocker: "FROM ubuntu:24.04\n"}
	runFileCases(t, []fileCase{
		{name: "good", path: env, text: "{\n  // built from the Dockerfile\n  \"build\": {\"dockerfile\": \"Dockerfile\", \"context\": \"..\"},\n  \"install\": \"npm ci\"\n}\n", also: docker},
		{name: "trailing comma", path: env, text: "{\n  \"install\": \"npm ci\",\n}\n", code: "K001", line: 2, hint: "trailing commas"},
		{name: "not an object", path: env, text: "[]", code: "K001", line: 1},
		{name: "update", path: env, text: "{\"update\": \"npm ci\"}", code: "K002", line: 1, hint: "\"install\""},
		{name: "typo", path: env, text: "{\n  \"instal\": \"npm ci\"\n}", code: "K002", line: 2, hint: `Did you mean "install"?`},
		{name: "dockerfile at top", path: env, text: "{\"dockerfile\": \"Dockerfile\"}", code: "K002", hint: "inside \"build\""},
		{name: "build without dockerfile", path: env, text: "{\"build\": {\"context\": \"..\"}}", code: "K003", line: 1, hint: "dockerfile"},
		{name: "unknown build key", path: env, text: "{\"build\": {\"dockerfile\": \"Dockerfile\", \"args\": {}}}", code: "K003", also: docker},
		{name: "dockerfile missing", path: env, text: "{\n  \"build\": {\n    \"dockerfile\": \"Dockerfile\"\n  }\n}", code: "K004", line: 3, hint: ".cursor/Dockerfile"},
		{name: "dockerfile in the root", path: env, text: "{\"build\": {\"dockerfile\": \"../Dockerfile\"}}", code: "K004", hint: "the Dockerfile Dockerfile"},
		{name: "dockerfile found in the root", path: env, text: "{\"build\": {\"dockerfile\": \"../Dockerfile\"}}", also: map[string]string{"Dockerfile": "FROM ubuntu:24.04\n"}},
		{name: "install as a list", path: env, text: "{\"install\": [\"npm ci\"]}", code: "K005", hint: "should be a string"},
		{name: "terminals as text", path: env, text: "{\"terminals\": \"npm run dev\"}", code: "K005", hint: "should be a list"},
		{name: "build as text", path: env, text: "{\"build\": \"Dockerfile\"}", code: "K005", hint: "object"},
		{name: "snapshot wins", path: env, text: "{\n  \"snapshot\": \"snap-1\",\n  \"build\": {\"dockerfile\": \"Dockerfile\"}\n}", code: "K006", line: 2, sev: spec.Warning, also: docker},
	})
}

func TestDevcontainerFindings(t *testing.T) {
	dc := kb.DevcontainerPath
	runFileCases(t, []fileCase{
		{name: "good", path: dc, text: "{\n  // comments and trailing commas are fine here\n  \"image\": \"mcr.microsoft.com/devcontainers/base:ubuntu24.04\",\n}\n"},
		{name: "broken", path: dc, text: "{\n  \"image\": \"x\"\n  \"name\": \"y\"\n}", code: "D001", line: 3},
		{name: "not an object", path: dc, text: "\"x\"", code: "D001"},
		{name: "nothing to start", path: dc, text: "{\"name\": \"x\"}", code: "D002", line: 1},
		{name: "compose without service", path: dc, text: "{\n  \"dockerComposeFile\": \"compose.yaml\"\n}", code: "D003", line: 2},
		{name: "unknown key", path: dc, text: "{\"image\": \"x\", \"postCreateCommands\": \"a\"}", code: "D004", sev: spec.Warning, hint: `Did you mean "postCreateCommand"?`},
		{name: "old keys still read", path: dc, text: "{\"dockerFile\": \"Dockerfile\", \"extensions\": []}"},
		{name: "root file", path: ".devcontainer.json", text: "{}", code: "D002"},
	})
}

func TestCloudInitFindings(t *testing.T) {
	ci := kb.CloudInitPath
	runFileCases(t, []fileCase{
		{name: "good", path: ci, text: "#cloud-config\npackages: [git]\n"},
		{name: "no header", path: ci, text: "packages: [git]\n", code: "I001", line: 1},
		{name: "header later", path: ci, text: "# my machine\n#cloud-config\n", code: "I001"},
		{name: "not yaml", path: ci, text: "#cloud-config\npackages: [git\n", code: "I002", line: 2},
	})
}

// ---- files that disagree ----

func TestCrossCheck(t *testing.T) {
	files := map[string]string{
		kb.CopilotWorkflowPath: strings.Replace(goodWorkflow, `node-version: "22"`, `node-version: "20"`, 1),
		kb.DevcontainerPath:    "{\"image\": \"mcr.microsoft.com/devcontainers/base:ubuntu24.04\", \"features\": {\"ghcr.io/devcontainers/features/node:2\": {\"version\": \"22\"}}}",
	}
	r := Check(Input{Files: files})
	d, ok := find(r, "X010")
	if !ok || d.Severity != spec.Warning {
		t.Fatalf("want an X010 warning, got %v", r.Findings)
	}
	if !strings.Contains(d.Message, "Node.js") || !strings.Contains(d.Message, "copilot-setup-steps.yml 20") || !strings.Contains(d.Message, "devcontainer.json 22") {
		t.Errorf("X010 = %q", d.Message)
	}
	// 22 and 22.11 are the same line of Node.js.
	files[kb.CopilotWorkflowPath] = strings.Replace(goodWorkflow, `node-version: "22"`, `node-version: "22.11"`, 1)
	if r := Check(Input{Files: files}); len(r.Findings) != 0 {
		t.Errorf("22 and 22.11 were reported as different: %v", r.Findings)
	}
}

func TestComposeServicesCount(t *testing.T) {
	files := map[string]string{
		kb.DevcontainerPath: "{\"dockerComposeFile\": \"compose.yaml\", \"service\": \"app\"}",
		kb.ComposePath:      "services:\n  app:\n    image: mcr.microsoft.com/devcontainers/base:ubuntu24.04\n  db:\n    image: postgres:15\n",
		kb.CloudInitPath:    "#cloud-config\nwrite_files:\n  - path: /opt/setup.sh\n    content: |\n      apt-get install -y postgresql-16\n",
	}
	r := Check(Input{Files: files})
	d, ok := find(r, "X010")
	if !ok || !strings.Contains(d.Message, "PostgreSQL") || !strings.Contains(d.Message, "devcontainer.json 15") || !strings.Contains(d.Message, "cloud-init.yaml 16") {
		t.Errorf("want PostgreSQL 15 against 16, got %v", r.Findings)
	}
}

// ---- files against the spec ----

func sampleFiles(t *testing.T) map[string]string {
	t.Helper()
	return readRepo(t, filepath.Join("..", "..", "testdata", "repos", "orders-api")).Files
}

func TestDrift(t *testing.T) {
	files := sampleFiles(t)
	script := files[kb.ScriptPath]
	lines := strings.Split(script, "\n")
	n := 0
	for i, l := range lines {
		if strings.Contains(l, "pytest -q") {
			lines[i] = strings.Replace(l, "pytest -q", "pytest -q -x", 1)
			n = i + 1
			break
		}
	}
	if n == 0 {
		t.Fatal("the script has no pytest line")
	}
	files[kb.ScriptPath] = strings.Join(lines, "\n")
	r := Check(Input{Files: files})
	if got := codes(r); strings.Join(got, " ") != "X002" {
		t.Fatalf("want only X002, got %v", r.Findings)
	}
	d := r.Findings[0]
	if d.File != kb.ScriptPath || d.Line != n || !strings.Contains(d.Message, "1 line to add, 1 to remove") || !strings.Contains(d.Message, "edited by hand") {
		t.Errorf("X002 = %+v", d)
	}
	if len(r.Diffs) != 1 || !strings.Contains(r.Diffs[0].Diff, "-") || !strings.Contains(r.Diffs[0].Diff, "pytest -q -x") {
		t.Errorf("diff = %+v", r.Diffs)
	}
}

// An edit to environment.json, which carries no generated-by line, is still
// told apart from a file preconfig never wrote.
func TestDriftInEnvironmentJSON(t *testing.T) {
	files := sampleFiles(t)
	files[kb.CursorEnvPath] = strings.Replace(files[kb.CursorEnvPath], `"name": "orders-api"`, `"name": "orders"`, 1)
	r := Check(Input{Files: files})
	d, ok := find(r, "X002")
	if !ok || d.File != kb.CursorEnvPath || !strings.Contains(d.Message, "edited by hand") {
		t.Errorf("want X002 saying the file was edited, got %+v", r.Findings)
	}
	files[kb.CursorEnvPath] = "{\n  \"install\": \"pip install -r requirements.txt\"\n}\n"
	r = Check(Input{Files: files})
	if d, ok := find(r, "X002"); !ok || !strings.Contains(d.Message, "not written by preconfig") {
		t.Errorf("want X002 saying preconfig didn't write it, got %+v", r.Findings)
	}
}

func TestMissingFileThatExists(t *testing.T) {
	files := sampleFiles(t)
	delete(files, kb.CloudInitPath)
	if d, ok := find(Check(Input{Files: files}), "X001"); !ok || d.File != kb.CloudInitPath {
		t.Errorf("want X001 for cloud-init.yaml, got %+v", d)
	}
	// A file that exists but couldn't be read is not reported as missing.
	r := Check(Input{Files: files, Exists: func(p string) bool { return p == kb.CloudInitPath }})
	if _, ok := find(r, "X001"); ok {
		t.Errorf("a file that exists was reported missing: %v", r.Findings)
	}
}

func TestVersionAgainstSpec(t *testing.T) {
	files := sampleFiles(t)
	// The spec moves to Python 3.13; every file still installs 3.12.
	files[kb.SpecPath] = strings.Replace(files[kb.SpecPath], `python: "3.12"`, `python: "3.13"`, 1)
	r := Check(Input{Files: files})
	x003 := 0
	for _, d := range r.Findings {
		if d.Code == "X003" {
			x003++
			if !strings.Contains(d.Message, "Python 3.12") || !strings.Contains(d.Message, "asks for 3.13") {
				t.Errorf("X003 = %q", d.Message)
			}
		}
	}
	// Copilot, the Cursor environment (through its Dockerfile and script),
	// cloud-init, the script and the dev container each say 3.12.
	if x003 != 5 {
		t.Errorf("want 5 X003, got %d: %v", x003, r.Findings)
	}
	// Each finding points at the line that says 3.12 in the file it names,
	// or names the other file the version came from.
	for _, d := range r.Findings {
		if d.Code != "X003" {
			continue
		}
		text := files[d.File]
		switch d.File {
		case kb.CursorEnvPath:
			if d.Line != 0 || !strings.Contains(d.Message, "(set in .preconfig/setup.sh, line ") {
				t.Errorf("the Cursor finding should name setup.sh: %v", d)
			}
		default:
			lines := strings.Split(text, "\n")
			if d.Line < 1 || d.Line > len(lines) || !strings.Contains(lines[d.Line-1], "3.12") {
				t.Errorf("%s: line %d doesn't say 3.12", d.File, d.Line)
			}
		}
	}
}

func TestExtraRuntime(t *testing.T) {
	files := map[string]string{
		kb.SpecPath:            "version: 1\ntargets: [copilot]\nready: [\"true\"]\n",
		kb.CopilotWorkflowPath: goodWorkflow,
	}
	r := Check(Input{Files: files})
	d, ok := find(r, "X005")
	if !ok || d.Severity != spec.Warning || !strings.Contains(d.Message, "Node.js 22") || d.Line != 18 {
		t.Errorf("want an X005 warning for Node.js 22 at line 18, got %v", r.Findings)
	}
}

func TestSpecErrorsStopTheComparison(t *testing.T) {
	files := sampleFiles(t)
	files[kb.SpecPath] = "version: 1\nruntimes:\n  node: \"21\"\n"
	r := Check(Input{Files: files})
	if d, ok := find(r, "S021"); !ok || d.File != kb.SpecPath {
		t.Fatalf("want S021 in preconfig.yaml, got %v", r.Findings)
	}
	if r.Findings[0].File != kb.SpecPath {
		t.Errorf("preconfig.yaml's findings should come first: %v", r.Findings)
	}
	for _, d := range r.Findings {
		if strings.HasPrefix(d.Code, "X00") {
			t.Errorf("a broken spec was still compared: %v", d)
		}
	}
}

func TestReferenced(t *testing.T) {
	cases := []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{kb.CursorEnvPath: `{"build": {"dockerfile": "Dockerfile"}}`}, ".cursor/Dockerfile"},
		{map[string]string{kb.CursorEnvPath: `{"build": {"dockerfile": "../docker/Dockerfile"}}`}, "docker/Dockerfile"},
		{map[string]string{kb.CursorEnvPath: `{"build": {"dockerfile": ".."}}`}, "."},
		{map[string]string{kb.CursorEnvPath: "{\n // c\n \"build\": {\"dockerfile\": \"Dockerfile\",},\n}"}, ".cursor/Dockerfile"},
		{map[string]string{kb.DevcontainerPath: `{"dockerComposeFile": ["compose.yaml", "../compose.override.yaml"]}`}, ".devcontainer/compose.yaml ../compose.override.yaml"},
		{map[string]string{".devcontainer.json": `{"dockerComposeFile": "docker-compose.yml"}`}, "docker-compose.yml"},
		{map[string]string{kb.DevcontainerPath: `{"dockerComposeFile": `}, ""},
	}
	for _, c := range cases {
		got := strings.Join(Referenced(c.files), " ")
		want := strings.Replace(c.want, "../compose.override.yaml", "compose.override.yaml", 1)
		if got != want {
			t.Errorf("Referenced(%v) = %q, want %q", c.files, got, want)
		}
	}
}

func TestDuplicateFindingsAreMerged(t *testing.T) {
	rep := &Report{Findings: []spec.Diagnostic{
		{File: "a", Line: 2, Code: "X", Message: "m", Severity: spec.Warning},
		{File: "a", Line: 2, Code: "X", Message: "m", Severity: spec.Warning},
		{File: kb.SpecPath, Line: 9, Code: "S", Message: "n", Severity: spec.Error},
		{File: "a", Line: 1, Code: "Y", Message: "o", Severity: spec.Note},
	}}
	finish(rep)
	if len(rep.Findings) != 3 || rep.Findings[0].File != kb.SpecPath || rep.Findings[1].Line != 1 {
		t.Errorf("finish gave %v", rep.Findings)
	}
	if rep.Errors != 1 || rep.Warnings != 1 {
		t.Errorf("counts %d/%d", rep.Errors, rep.Warnings)
	}
}

// ---- reading versions back ----

func TestSameVersion(t *testing.T) {
	cases := []struct {
		want, got string
		same      bool
	}{
		{"22", "22", true}, {"22", "22.11.0", true}, {"22", "v22.1", true}, {"22", "22.x", true},
		{"3.12", "3.12.4", true}, {"3.12", "3.12", true}, {"3.12", "3.1", false}, {"3.12", "3.11", false},
		{"22", "latest", false}, {"22", "lts", false}, {"22", "", false}, {"1.25", "1.25.3", true}, {"16", "16", true},
		{"1.25.3", "1.25", false}, {"7", "8", false},
	}
	for _, c := range cases {
		if got := sameVersion(c.want, c.got); got != c.same {
			t.Errorf("sameVersion(%q, %q) = %v", c.want, c.got, got)
		}
	}
}

func TestImageFacts(t *testing.T) {
	cases := []struct {
		image, thing, want string
	}{
		{"postgres:16.4-alpine", "postgres", "16"},
		{"docker.io/library/postgres", "postgres", "latest"},
		{"bitnami/postgresql:17", "postgres", "17"},
		{"redis:7-alpine", "redis", "7"},
		{"redis/redis-stack-server:7.4.0-v1", "redis", "7"},
		{"valkey/valkey:8", "redis", ""},
		{"node:22-bookworm", "node", "22"},
		{"python:3.12-slim", "python", "3.12"},
		{"golang:1.25", "go", "1.25"},
		{"ghcr.io/acme/app:1.0", "node", ""},
	}
	for _, c := range cases {
		var f Facts
		imageFact(&f, c.image, 1)
		if got := f.get(c.thing).Version; got != c.want {
			t.Errorf("%s: %s = %q, want %q", c.image, c.thing, got, c.want)
		}
	}
}

func TestScriptFacts(t *testing.T) {
	hand := strings.Join([]string{
		"curl -fsSL https://deb.nodesource.com/setup_20.x | bash -",
		"apt-get install -y postgresql-15 python3.11",
		"export GOTOOLCHAIN=go1.25.0+auto",
	}, "\n")
	f := scriptFacts("s.sh", hand)
	if f.Node != (Fact{Version: "20", Line: 1}) || f.Postgres != (Fact{Version: "15", Line: 2}) || f.Python != (Fact{Version: "3.11", Line: 2}) || f.Go != (Fact{Version: "1.25", Line: 3}) {
		t.Errorf("facts from a hand-written script: %+v", f)
	}
	f = scriptFacts("s.sh", "uv python install --default 3.13\n")
	if f.Python.Version != "3.13" {
		t.Errorf("uv python install: %+v", f.Python)
	}
	// preconfig's own script is read from its step markers only, so the
	// PGDG and NodeSource lines inside it don't count twice.
	own := "# " + generatedTag + "\n  step \"machine 2/3: node 22\"\n  echo setup_20.x\n"
	if f := scriptFacts("s.sh", own); f.Node != (Fact{Version: "22", Line: 2}) {
		t.Errorf("markers: %+v", f.Node)
	}
	df := dockerfileFacts("Dockerfile", "FROM --platform=linux/amd64 python:3.11-slim AS base\nRUN true\nFROM redis:8\n")
	if df.Python != (Fact{Version: "3.11", Line: 1}) || df.Redis != (Fact{Version: "8", Line: 3}) {
		t.Errorf("Dockerfile facts: %+v", df)
	}
}

func TestDevcontainerImageFacts(t *testing.T) {
	cases := map[string][2]string{
		"mcr.microsoft.com/devcontainers/python:1-3.12-bookworm":        {"python", "3.12"},
		"mcr.microsoft.com/devcontainers/javascript-node:22":            {"node", "22"},
		"mcr.microsoft.com/devcontainers/typescript-node:1-20-bookworm": {"node", "20"},
		"mcr.microsoft.com/devcontainers/go:1-1.25-bookworm":            {"go", "1.25"},
		"python:3.13": {"python", "3.13"},
	}
	for img, want := range cases {
		root := &Facts{}
		*root = devcontainerFacts("d", mustJSONC(t, `{"image": "`+img+`"}`))
		if got := root.get(want[0]).Version; got != want[1] {
			t.Errorf("%s: %s = %q, want %q", img, want[0], got, want[1])
		}
	}
}

func mustJSONC(t *testing.T, s string) *tree.Node {
	t.Helper()
	n, err := jsonc.Parse(s, jsonc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ---- the fixtures on disk stay what the tests expect ----

func TestFixturesHaveNoGeneratedFiles(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "check", "broken")
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), generatedTag) {
				t.Errorf("%s was written by preconfig; the broken fixture must hold hand-written files only", p)
			}
		}
		return nil
	})
}

// FuzzCheck feeds any text to every kind of file, alone, all at once, and next
// to a valid spec. Check must never panic, and must count what it reports.
func FuzzCheck(f *testing.F) {
	f.Add(goodWorkflow)
	f.Add("{\"build\": {\"dockerfile\": \"Dockerfile\"}, \"snapshot\": 1}")
	f.Add("#cloud-config\nwrite_files:\n  - path: /x/setup.sh\n    content: step \"machine 1/2: node 22\"\n")
	f.Add("{\"dockerComposeFile\": [\"compose.yaml\"], \"service\": \"a\", \"features\": {\"ghcr.io/devcontainers/features/node:2\": {}}}")
	f.Add("services:\n  db:\n    image: postgres:16\n")
	f.Add("jobs:\n  a:\n    steps:\n  b: 1\non: [push]\n")
	specText := "version: 1\nname: x\nruntimes:\n  node: \"22\"\nservices:\n  postgres: \"16\"\nready: [\"true\"]\n"
	f.Fuzz(func(t *testing.T, text string) {
		all := map[string]string{}
		for _, p := range KnownPaths {
			one := Check(Input{Files: map[string]string{p: text}})
			countOK(t, one)
			withSpec := Check(Input{Files: map[string]string{p: text, kb.SpecPath: specText}})
			countOK(t, withSpec)
			all[p] = text
		}
		countOK(t, Check(Input{Files: all}))
		Referenced(all)
	})
}

func countOK(t *testing.T, r Report) {
	e, w := 0, 0
	for _, d := range r.Findings {
		switch d.Severity {
		case spec.Error:
			e++
		case spec.Warning:
			w++
		}
	}
	if e != r.Errors || w != r.Warnings {
		t.Fatalf("counted %d/%d, report says %d/%d", e, w, r.Errors, r.Warnings)
	}
}

func BenchmarkCheckClean(b *testing.B) {
	t := &testing.T{}
	in := readRepo(t, filepath.Join("..", "..", "testdata", "repos", "orders-api"))
	in.Exists = nil
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Check(in)
	}
}

func BenchmarkCheckBroken(b *testing.B) {
	t := &testing.T{}
	in := readRepo(t, filepath.Join("..", "..", "testdata", "check", "broken"))
	in.Exists = nil
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Check(in)
	}
}
