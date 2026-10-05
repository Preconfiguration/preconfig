package gen

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/jsonc"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
	"preconfiguration.com/preconfig/internal/yaml"
)

var update = flag.Bool("update", false, "rewrite the generated files in testdata/repos")

var repos = []string{"orders-api", "web-shop", "ingest-worker"}

func repoDir(name string) string { return filepath.Join("..", "..", "testdata", "repos", name) }

func load(t *testing.T, src string) *spec.Spec {
	t.Helper()
	s, ds := spec.Load("", src)
	if s == nil {
		t.Fatalf("spec rejected: %v", ds)
	}
	return s
}

func loadRepo(t *testing.T, name string) *spec.Spec {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoDir(name), "preconfig.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return load(t, string(b))
}

// TestGolden compares a fresh build with the files committed in each sample
// repository. Run with -update after an intended change.
func TestGolden(t *testing.T) {
	for _, name := range repos {
		s := loadRepo(t, name)
		for _, f := range Build(s).Files {
			p := filepath.Join(repoDir(name), filepath.FromSlash(f.Path))
			if *update {
				os.MkdirAll(filepath.Dir(p), 0o755)
				os.WriteFile(p, []byte(f.Content), 0o644)
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			if string(b) != f.Content {
				t.Errorf("%s/%s differs from a fresh build (run go test -update if intended)", name, f.Path)
			}
		}
	}
}

func TestDeterministic(t *testing.T) {
	for _, name := range repos {
		s := loadRepo(t, name)
		a, b := Build(s), Build(s)
		if len(a.Files) != len(b.Files) {
			t.Fatal("different number of files")
		}
		for i := range a.Files {
			if a.Files[i] != b.Files[i] {
				t.Errorf("%s: %s differs between two builds", name, a.Files[i].Path)
			}
		}
	}
}

func files(r Result) map[string]File {
	m := map[string]File{}
	for _, f := range r.Files {
		m[f.Path] = f
	}
	return m
}

func TestCopilotWorkflow(t *testing.T) {
	for _, name := range repos {
		s := loadRepo(t, name)
		f := files(Build(s))[kb.CopilotWorkflowPath]
		root, err := yaml.Parse(f.Content)
		if err != nil {
			t.Fatalf("%s: the workflow doesn't parse: %v", name, err)
		}
		jobs := root.Get("jobs")
		if len(jobs.Keys) != 1 || jobs.Keys[0].Value != kb.CopilotJobName {
			t.Fatalf("%s: jobs = %v", name, jobs.Keys)
		}
		job := jobs.Get(kb.CopilotJobName)
		for _, k := range job.Keys {
			if !contains(kb.CopilotJobKeys, k.Value) {
				t.Errorf("%s: job setting %q is one Copilot ignores", name, k.Value)
			}
		}
		if job.Get("runs-on").Value != "ubuntu-24.04" {
			t.Errorf("%s: runs-on = %q", name, job.Get("runs-on").Value)
		}
		// Changes to the workflow or the spec run it.
		for _, ev := range []string{"push", "pull_request"} {
			paths := strings.Join(root.Path("on", ev, "paths").Strings(), ",")
			if !strings.Contains(paths, kb.CopilotWorkflowPath) || !strings.Contains(paths, kb.SpecPath) {
				t.Errorf("%s: %s paths = %s", name, ev, paths)
			}
		}
		var ready *tree.Node
		uses := map[string]*tree.Node{}
		for _, st := range job.Get("steps").Items {
			if u := st.Get("uses").Str(); u != "" {
				uses[strings.Split(u, "@")[0]] = st
			}
			if st.Get("name").Str() == "Ready check" {
				ready = st
			}
		}
		if _, ok := uses["actions/checkout"]; !ok {
			t.Errorf("%s: no checkout", name)
		}
		if ready == nil {
			t.Fatalf("%s: no ready check step", name)
		}
		cond := ready.Get("if").Str()
		for _, ev := range ValidationEvents {
			if !strings.Contains(cond, "'"+ev+"'") {
				t.Errorf("%s: the ready check doesn't run on %s: %q", name, ev, cond)
			}
		}
		if strings.Contains(cond, "dynamic") || cond == "" {
			t.Errorf("%s: the ready check condition is wrong: %q", name, cond)
		}
		if s.Python != "" && uses["actions/setup-python"].Path("with", "python-version").Value != s.Python {
			t.Errorf("%s: python version wrong", name)
		}
		if s.Node != "" && uses["actions/setup-node"].Path("with", "node-version").Value != s.Node {
			t.Errorf("%s: node version wrong", name)
		}
		if s.Go != "" && uses["actions/setup-go"].Path("with", "go-version").Value != s.Go {
			t.Errorf("%s: go version wrong", name)
		}
		for _, sv := range s.Services {
			img := job.Path("services", sv.Name, "image").Str()
			if img != sv.Name+":"+sv.Version {
				t.Errorf("%s: service image %q for %s %s", name, img, sv.Name, sv.Version)
			}
			if ports := job.Path("services", sv.Name, "ports").Strings(); len(ports) != 1 {
				t.Errorf("%s: %s ports = %v", name, sv.Name, ports)
			}
		}
	}
}

func TestCopilotToolsAndCaches(t *testing.T) {
	s := load(t, "version: 1\nruntimes: {node: \"22\", python: \"3.13\"}\ntools: [pnpm@10, uv@0.12.20, poetry]\nsetup: [pnpm install --frozen-lockfile, \".venv/bin/pip install -r requirements/dev.txt\"]\nready: [pnpm test]\n")
	c := files(Build(s))[kb.CopilotWorkflowPath].Content
	for _, want := range []string{
		kb.ActionSetupPnpm, "version: \"10\"", "cache: pnpm", kb.ActionSetupUV, "version: \"0.12.20\"",
		"pipx install poetry", "cache: pip", "cache-dependency-path: requirements/dev.txt",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("the workflow lacks %q", want)
		}
	}
	// pnpm must be set up before setup-node, or setup-node's pnpm cache fails.
	if strings.Index(c, kb.ActionSetupPnpm) > strings.Index(c, kb.ActionSetupNode) {
		t.Error("pnpm is set up after Node.js")
	}
	// No lockfile install, no cache.
	s = load(t, "version: 1\nruntimes: {node: \"22\"}\nsetup: [npm install]\nready: [npm test]\n")
	if c := files(Build(s))[kb.CopilotWorkflowPath].Content; strings.Contains(c, "cache:") {
		t.Error("a cache was set without a lockfile install")
	}
}

func TestDevcontainer(t *testing.T) {
	for _, name := range repos {
		s := loadRepo(t, name)
		fs := files(Build(s))
		dc := fs[kb.DevcontainerPath]
		root, err := jsonc.Parse(dc.Content, jsonc.JSONC)
		if err != nil {
			t.Fatalf("%s: devcontainer.json doesn't parse: %v", name, err)
		}
		if len(s.Services) > 0 {
			if root.Get("dockerComposeFile").Str() != "compose.yaml" || root.Get("service").Str() != "app" {
				t.Errorf("%s: compose settings wrong", name)
			}
			cf, ok := fs[kb.ComposePath]
			if !ok {
				t.Fatalf("%s: no compose.yaml", name)
			}
			croot, err := yaml.Parse(cf.Content)
			if err != nil {
				t.Fatalf("%s: compose.yaml doesn't parse: %v", name, err)
			}
			for _, sv := range s.Services {
				svc := croot.Path("services", sv.Name)
				if svc.Get("network_mode").Str() != "service:app" {
					t.Errorf("%s: %s doesn't share app's network", name, sv.Name)
				}
				if svc.Get("image").Str() != sv.Name+":"+sv.Version {
					t.Errorf("%s: %s image = %q", name, sv.Name, svc.Get("image").Str())
				}
			}
		} else if root.Get("image").Str() != kb.Bases["ubuntu-24.04"].DevcontainerImage {
			t.Errorf("%s: image = %q", name, root.Get("image").Str())
		}
		if s.Node != "" {
			f := root.Path("features", kb.FeatureNode)
			if f.Get("version").Str() != s.Node {
				t.Errorf("%s: node feature version wrong", name)
			}
		}
		if len(s.Setup) > 0 && root.Get("postCreateCommand").Str() != strings.Join(s.Setup, " && ") {
			t.Errorf("%s: postCreateCommand = %q", name, root.Get("postCreateCommand").Str())
		}
	}
}

func TestDevcontainerDetails(t *testing.T) {
	s := load(t, "version: 1\nruntimes: {node: \"22\"}\nservices:\n  postgres: \"18\"\nsetup: [\"npm ci || npm install\", \"npm run build\"]\nready: [\"true\"]\n")
	fs := files(Build(s))
	if !strings.Contains(fs[kb.ComposePath].Content, "postgres-data:/var/lib/postgresql\n") {
		t.Error("postgres 18 keeps its data in /var/lib/postgresql")
	}
	root, _ := jsonc.Parse(fs[kb.DevcontainerPath].Content, jsonc.JSONC)
	if got := root.Path("features", kb.FeatureNode, "pnpmVersion").Str(); got != "none" {
		t.Errorf("pnpm should be off without the tool, got %q", got)
	}
	if got := root.Get("postCreateCommand").Str(); got != "(npm ci || npm install) && npm run build" {
		t.Errorf("postCreateCommand = %q", got)
	}
	s = load(t, "version: 1\nservices:\n  postgres: \"17\"\nready: [\"true\"]\n")
	if !strings.Contains(files(Build(s))[kb.ComposePath].Content, "postgres-data:/var/lib/postgresql/data\n") {
		t.Error("postgres 17 keeps its data in /var/lib/postgresql/data")
	}
}

func TestCursor(t *testing.T) {
	for _, name := range repos {
		s := loadRepo(t, name)
		fs := files(Build(s))
		env := fs[kb.CursorEnvPath]
		root, err := jsonc.Parse(env.Content, jsonc.Options{})
		if err != nil {
			t.Fatalf("%s: environment.json isn't strict JSON: %v", name, err)
		}
		for _, k := range root.Keys {
			if !contains(kb.CursorKeys, k.Value) {
				t.Errorf("%s: key %q is not in Cursor's schema", name, k.Value)
			}
		}
		if root.Path("build", "dockerfile").Str() != "Dockerfile" || root.Path("build", "context").Str() != ".." {
			t.Errorf("%s: build = %v", name, root.Get("build"))
		}
		docker := fs[kb.CursorDocker].Content
		// The Dockerfile copies the script from the repository root, which is
		// the build context, and the script is one of the generated files.
		m := regexp.MustCompile(`(?m)^COPY (\S+) `).FindStringSubmatch(docker)
		if m == nil || m[1] != kb.ScriptPath {
			t.Fatalf("%s: the Dockerfile doesn't copy the script", name)
		}
		if _, ok := fs[m[1]]; !ok {
			t.Errorf("%s: the Dockerfile copies %s, which the build doesn't write", name, m[1])
		}
		if !strings.Contains(docker, "FROM ubuntu:24.04") || !strings.Contains(docker, "RUN bash /opt/preconfig/setup.sh machine") {
			t.Errorf("%s: Dockerfile:\n%s", name, docker)
		}
		if root.Get("install").Str() != "bash .preconfig/setup.sh project" {
			t.Errorf("%s: install = %q", name, root.Get("install").Str())
		}
		if (len(s.Services) > 0) != (root.Get("start") != nil) {
			t.Errorf("%s: start should be set exactly when there are services", name)
		}
	}
}

func TestCloudInit(t *testing.T) {
	s := load(t, "version: 1\nname: api\nrepo: https://github.com/acme/api.git\nruntimes: {python: \"3.12\"}\nservices: {redis: \"7\"}\nready: [\"true\"]\n")
	f := files(Build(s))[kb.CloudInitPath]
	if !strings.HasPrefix(f.Content, "#cloud-config\n") {
		t.Fatal("the first line must be #cloud-config")
	}
	root, err := yaml.Parse(f.Content)
	if err != nil {
		t.Fatalf("cloud-init doesn't parse: %v", err)
	}
	wf := root.Get("write_files").Items[0]
	if wf.Get("path").Str() != "/opt/preconfig/setup.sh" || wf.Get("permissions").Str() != "0755" {
		t.Errorf("write_files = %+v", wf)
	}
	if wf.Get("content").Str() != ScriptText(s) {
		t.Error("cloud-init doesn't carry the same script as the script target")
	}
	var cmds []string
	for _, it := range root.Get("runcmd").Items {
		cmds = append(cmds, strings.Join(it.Strings(), " "))
	}
	want := []string{
		"bash /opt/preconfig/setup.sh machine",
		"bash /opt/preconfig/setup.sh services",
		"git clone --depth 1 https://github.com/acme/api.git /srv/api",
		"bash -c cd /srv/api && bash /opt/preconfig/setup.sh project ready",
	}
	if strings.Join(cmds, "\n") != strings.Join(want, "\n") {
		t.Errorf("runcmd =\n%s", strings.Join(cmds, "\n"))
	}
}

func TestNotes(t *testing.T) {
	s := load(t, "version: 1\nsecrets: [NPM_TOKEN]\nready: [\"true\"]\n")
	r := Build(s)
	codes := map[string]bool{}
	for _, n := range r.Notes {
		codes[n.Code] = true
	}
	for _, c := range []string{"B001", "B002", "B003", "B004", "B011"} {
		if !codes[c] {
			t.Errorf("note %s missing", c)
		}
	}
}

func TestQuoting(t *testing.T) {
	cases := map[string]string{
		"22": `"22"`, "3.10": `"3.10"`, "true": `"true"`, "on": `"on"`, "5432:5432": `"5432:5432"`,
		"postgres:16": "postgres:16", "a: b": `"a: b"`, "": `""`, "-x": `"-x"`, "- x": `"- x"`,
		".venv/bin/pytest -q": ".venv/bin/pytest -q", "./x": "./x", ".5": `".5"`, "1_000": `"1_000"`, "2026-09-30": `"2026-09-30"`,
		"Yes": `"Yes"`, "N": `"N"`, "._9": `"._9"`, "a\u2028b": `"a\u2028b"`, "del\x7f": `"del\u007f"`,
		"@a": `"@a"`, "it's": `"it's"`, "say \"hi\"": `"say \"hi\""`, "x #y": `"x #y"`, ".inf": `".inf"`,
	}
	for in, want := range cases {
		if got := yq(in); got != want {
			t.Errorf("yq(%q) = %s, want %s", in, got, want)
		}
		// Whatever yq writes reads back as the same string.
		n, err := yaml.Parse("k: " + yq(in) + "\n")
		if err != nil {
			t.Errorf("yq(%q) doesn't parse: %v", in, err)
			continue
		}
		if got := n.Get("k"); got.Value != in || (in != "" && got.Type != tree.String) {
			t.Errorf("yq(%q) reads back as %q (type %d)", in, got.Value, got.Type)
		}
	}
	shCases := map[string]string{"abc": "abc", "a b": "'a b'", "it's": `'it'\''s'`, "": "''", "x=y": "x=y", "$HOME": "'$HOME'"}
	for in, want := range shCases {
		if got := sh(in); got != want {
			t.Errorf("sh(%q) = %s, want %s", in, got, want)
		}
	}
	dq := map[string]string{"plain": "plain", "a b": `"a b"`, `x"y`: `"x\"y"`, "$V": `"\$V"`}
	for in, want := range dq {
		if got := dockerQuote(in); got != want {
			t.Errorf("dockerQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

// ---- the setup script, run for real ----

func needBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
}

func writeScript(t *testing.T, s *spec.Spec) (dir, script string) {
	dir = t.TempDir()
	script = filepath.Join(dir, "setup.sh")
	if err := os.WriteFile(script, []byte(ScriptText(s)), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, script
}

func runScript(t *testing.T, dir, script string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestScriptSyntax(t *testing.T) {
	needBash(t)
	for _, name := range repos {
		_, script := writeScript(t, loadRepo(t, name))
		if out, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
			t.Errorf("%s: bash -n: %v\n%s", name, err, out)
		}
	}
}

func TestScriptRunsSetupAndReady(t *testing.T) {
	needBash(t)
	s := load(t, "version: 1\nsecrets: [DEMO_TOKEN]\nsetup:\n  - echo one >> order.txt\n  - echo two >> order.txt\nready:\n  - grep -q two order.txt\n")
	dir, script := writeScript(t, s)
	out, code := runScript(t, dir, script, []string{"DEMO_TOKEN="}, "project", "ready")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "order.txt"))
	if string(b) != "one\ntwo\n" {
		t.Errorf("the setup commands ran as %q", b)
	}
	for _, want := range []string{
		">>> preconfig: project 1/2: echo one >> order.txt",
		">>> preconfig: project 2/2: echo two >> order.txt",
		">>> preconfig: ready 1/1: grep -q two order.txt",
		">>> preconfig: ready: passed",
		">>> preconfig: warning: DEMO_TOKEN is not set",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestScriptReportsTheFailedStep(t *testing.T) {
	needBash(t)
	s := load(t, "version: 1\nsetup:\n  - \"true\"\n  - sh -c 'exit 7'\n  - touch never.txt\nready: [\"true\"]\n")
	dir, script := writeScript(t, s)
	out, code := runScript(t, dir, script, nil, "project")
	if code != 7 {
		t.Errorf("exit code %d, want 7:\n%s", code, out)
	}
	if !strings.Contains(out, ">>> preconfig: FAILED: project 2/3: sh -c 'exit 7' (exit code 7)") {
		t.Errorf("no failure marker:\n%s", out)
	}
	if strings.Count(out, "FAILED") != 1 {
		t.Errorf("the failure is reported more than once:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "never.txt")); err == nil {
		t.Error("the script went on after a failed step")
	}
}

func TestScriptEnvRoundTrip(t *testing.T) {
	needBash(t)
	val := `it's "quoted" $HOME and a;semicolon`
	s := load(t, "version: 1\nenv:\n  ODD: "+tree.Quote(val)+"\nready:\n  - printf '%s' \"$ODD\" > env.txt\n")
	dir, script := writeScript(t, s)
	if out, code := runScript(t, dir, script, nil, "ready"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "env.txt"))
	if string(b) != val {
		t.Errorf("ODD = %q, want %q", b, val)
	}
}

func TestScriptUsage(t *testing.T) {
	needBash(t)
	dir, script := writeScript(t, load(t, "version: 1\nready: [\"true\"]\n"))
	if out, code := runScript(t, dir, script, nil, "bogus"); code != 2 || !strings.Contains(out, "usage:") {
		t.Errorf("unknown step: exit %d\n%s", code, out)
	}
	if out, code := runScript(t, dir, script, nil, "help"); code != 0 || !strings.Contains(out, "usage:") {
		t.Errorf("help: exit %d\n%s", code, out)
	}
}

func TestShellcheck(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck is not installed")
	}
	for _, name := range repos {
		_, script := writeScript(t, loadRepo(t, name))
		if out, err := exec.Command("shellcheck", "-S", "style", script).CombinedOutput(); err != nil {
			t.Errorf("%s: shellcheck:\n%s", name, out)
		}
	}
	// Specs without services, and with every tool, take other branches.
	specs, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "specs", "*.yaml"))
	if len(specs) == 0 {
		t.Fatal("no specs in testdata/specs")
	}
	for _, p := range specs {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		_, script := writeScript(t, load(t, string(src)))
		if out, err := exec.Command("shellcheck", "-S", "style", script).CombinedOutput(); err != nil {
			t.Errorf("%s: shellcheck:\n%s", filepath.Base(p), out)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func BenchmarkBuild(b *testing.B) {
	src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "repos", "orders-api", "preconfig.yaml"))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		s, _ := spec.Load("", string(src))
		Build(s)
	}
}
