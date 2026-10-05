package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/check"
	"preconfiguration.com/preconfig/internal/kb"
)

func runCLI(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// copyRepo copies a sample repository into a temporary folder. With specOnly,
// it copies the repository without the generated files.
func copyRepo(t *testing.T, name string, specOnly bool) string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "repos", name)
	dst := t.TempDir()
	generated := map[string]bool{}
	for _, p := range []string{kb.DevcontainerPath, kb.ComposePath, kb.CopilotWorkflowPath, kb.CursorEnvPath, kb.CursorDocker, kb.CloudInitPath, kb.ScriptPath} {
		generated[p] = true
	}
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if specOnly && generated[filepath.ToSlash(rel)] {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, info.Mode())
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestUsage(t *testing.T) {
	if code, _, errOut := runCLI(); code != exitUsage || !strings.Contains(errOut, "Usage:") {
		t.Errorf("no arguments: %d %q", code, errOut)
	}
	if code, _, errOut := runCLI("compile"); code != exitUsage || !strings.Contains(errOut, `unknown command "compile"`) {
		t.Errorf("unknown command: %d %q", code, errOut)
	}
	if code, out, _ := runCLI("help"); code != exitOK || !strings.Contains(out, "preconfig verify") {
		t.Errorf("help: %d %q", code, out)
	}
	if code, out, _ := runCLI("version"); code != exitOK || out != "preconfig "+kb.Version+" (knowledge as of "+kb.Date+")\n" {
		t.Errorf("version: %d %q", code, out)
	}
	if code, _, _ := runCLI("build", "--no-such-flag"); code != exitUsage {
		t.Errorf("a bad flag gave %d", code)
	}
	code, out, _ := runCLI("targets")
	if code != exitOK || strings.Count(out, "\n") != 6 || !strings.Contains(out, kb.CopilotWorkflowPath) {
		t.Errorf("targets: %d %q", code, out)
	}
}

func TestBuild(t *testing.T) {
	dir := copyRepo(t, "orders-api", true)
	code, out, errOut := runCLI("build", "--dir", dir)
	if code != exitOK || errOut != "" {
		t.Fatalf("build: %d %q %q", code, out, errOut)
	}
	if strings.Count(out, "wrote ") != 7 {
		t.Errorf("want 7 files written:\n%s", out)
	}
	// Built files match the sample's committed files byte for byte.
	for _, p := range []string{kb.DevcontainerPath, kb.ComposePath, kb.CopilotWorkflowPath, kb.CursorEnvPath, kb.CursorDocker, kb.CloudInitPath, kb.ScriptPath} {
		got, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		want, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "repos", "orders-api", p))
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the sample's", p)
		}
	}
	if info, _ := os.Stat(filepath.Join(dir, kb.ScriptPath)); info.Mode().Perm() != 0o755 {
		t.Errorf("setup.sh mode = %v", info.Mode())
	}
	code, out, _ = runCLI("build", "--dir", dir)
	if code != exitOK || strings.Count(out, "unchanged ") != 7 || strings.Contains(out, "wrote") {
		t.Errorf("a second build should change nothing:\n%s", out)
	}
}

func TestBuildDryRunAndJSON(t *testing.T) {
	dir := copyRepo(t, "web-shop", true)
	code, out, _ := runCLI("build", "--dir", dir, "--dry-run")
	if code != exitOK || !strings.Contains(out, "would write "+kb.CopilotWorkflowPath) {
		t.Errorf("dry run: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, kb.ScriptPath)); err == nil {
		t.Error("a dry run wrote files")
	}
	code, out, _ = runCLI("build", "--dir", dir, "--dry-run", "--json")
	var res struct {
		Files []struct{ Path, Target, Content string }
		Notes []any
	}
	if code != exitOK || json.Unmarshal([]byte(out), &res) != nil || len(res.Files) != 7 {
		t.Errorf("json: %d %q", code, out[:min(200, len(out))])
	}
}

func TestBuildWithABrokenSpec(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, kb.SpecPath), []byte("version: 1\nruntimes:\n  nodejs: \"22\"\n"), 0o644)
	code, _, errOut := runCLI("build", "--dir", dir)
	if code != exitSpec || !strings.Contains(errOut, "preconfig.yaml:3:3: error S020") {
		t.Errorf("broken spec: %d %q", code, errOut)
	}
	code, out, _ := runCLI("build", "--dir", dir, "--json")
	if code != exitSpec || !strings.Contains(out, `"code": "S020"`) {
		t.Errorf("broken spec as JSON: %d %q", code, out)
	}
	code, _, errOut = runCLI("build", "--dir", t.TempDir())
	if code != exitSpec || !strings.Contains(errOut, "preconfig detect --write") {
		t.Errorf("no spec: %d %q", code, errOut)
	}
	// A spec given by path, outside the repository.
	other := filepath.Join(t.TempDir(), "other.yaml")
	os.WriteFile(other, []byte("version: 1\ntargets: [script]\nready: [\"true\"]\n"), 0o644)
	out2 := t.TempDir()
	if code, out, errOut := runCLI("build", "--dir", out2, "--spec", other); code != exitOK || !strings.Contains(out, kb.ScriptPath) {
		t.Errorf("--spec: %d %q %q", code, out, errOut)
	}
}

func TestCheck(t *testing.T) {
	dir := copyRepo(t, "orders-api", false)
	code, out, _ := runCLI("check", "--dir", dir)
	if code != exitOK || out != "checked 6 files: 0 errors, 0 warnings\n" {
		t.Errorf("clean check: %d %q", code, out)
	}
	p := filepath.Join(dir, kb.CopilotWorkflowPath)
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    timeout-minutes: 90\n", 1)), 0o644)
	code, out, _ = runCLI("check", "--dir", dir)
	if code != exitFindings || !strings.Contains(out, "error C005") || !strings.Contains(out, "error X002") || !strings.Contains(out, "Run with --diff") {
		t.Errorf("edited workflow: %d\n%s", code, out)
	}
	code, out, _ = runCLI("check", "--dir", dir, "--diff")
	if code != exitFindings || !strings.Contains(out, " ubuntu-24.04\n-    timeout-minutes: 90\n     permissions:\n") {
		t.Errorf("--diff: %d\n%s", code, out)
	}
	code, out, _ = runCLI("check", "--dir", dir, "--json")
	var rep check.Report
	if code != exitFindings || json.Unmarshal([]byte(out), &rep) != nil || rep.Errors != 2 || len(rep.Diffs) != 1 {
		t.Errorf("--json: %d %+v", code, rep)
	}
}

func TestCheckExitCodes(t *testing.T) {
	code, out, _ := runCLI("check", "--dir", t.TempDir())
	if code != exitOK || !strings.HasPrefix(out, "No setup files and no preconfig.yaml") {
		t.Errorf("empty folder: %d %q", code, out)
	}
	dir := copyRepo(t, "orders-api", false)
	os.WriteFile(filepath.Join(dir, kb.SpecPath), []byte("version: 3\n"), 0o644)
	if code, out, _ := runCLI("check", "--dir", dir); code != exitSpec || !strings.Contains(out, "S003") {
		t.Errorf("broken spec: %d\n%s", code, out)
	}
	broken := filepath.Join("..", "..", "testdata", "check", "broken")
	code, out, _ = runCLI("check", "--dir", broken)
	if code != exitFindings || !strings.HasSuffix(out, "checked 4 files: 15 errors, 2 warnings\nRun with --diff to see the changes, or preconfig build to apply them.\n") {
		t.Errorf("broken fixture: %d\n%s", code, out)
	}
}

func TestDetect(t *testing.T) {
	dir := copyRepo(t, "ingest-worker", true)
	os.Remove(filepath.Join(dir, kb.SpecPath))
	code, out, _ := runCLI("detect", "--dir", dir)
	if code != exitOK || !strings.Contains(out, `go: "1.25"  # go directive in go.mod`) {
		t.Errorf("detect: %d\n%s", code, out)
	}
	code, out, _ = runCLI("detect", "--dir", dir, "--write")
	if code != exitOK || !strings.HasPrefix(out, "wrote preconfig.yaml from go.mod, compose.yaml, .env.example.") {
		t.Errorf("--write: %d %q", code, out)
	}
	if code, _, errOut := runCLI("detect", "--dir", dir, "--write"); code != exitFindings || !strings.Contains(errOut, "--force") {
		t.Errorf("a second --write should refuse: %d %q", code, errOut)
	}
	if code, _, _ := runCLI("detect", "--dir", dir, "--write", "--force"); code != exitOK {
		t.Errorf("--force: %d", code)
	}
	// The written draft builds.
	if code, _, errOut := runCLI("build", "--dir", dir); code != exitOK {
		t.Errorf("building the draft: %d %q", code, errOut)
	}
	code, out, _ = runCLI("detect", "--dir", dir, "--json")
	var res struct {
		Spec  string
		Notes []string
		Found []string
	}
	if code != exitOK || json.Unmarshal([]byte(out), &res) != nil || len(res.Found) != 3 {
		t.Errorf("--json: %d %q", code, out)
	}
}

// fakeDocker puts the verify package's stand-in for docker first on PATH.
func fakeDocker(t *testing.T, output, exit string) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	bin := t.TempDir()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "verify", "testdata", "fake-docker"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(bin, "docker"), b, 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	fd := t.TempDir()
	t.Setenv("FAKE_DIR", fd)
	t.Setenv("FAKE_EXIT", exit)
	t.Setenv("FAKE_SLEEP", "")
	t.Setenv("FAKE_DOCKER_DOWN", "")
	p := filepath.Join(fd, "output")
	os.WriteFile(p, []byte(output), 0o644)
	t.Setenv("FAKE_OUTPUT", p)
	return fd
}

func TestVerify(t *testing.T) {
	dir := copyRepo(t, "orders-api", false)
	pass, err := os.ReadFile(filepath.Join("..", "..", "internal", "verify", "testdata", "pass.log"))
	if err != nil {
		t.Fatal(err)
	}
	fakeDocker(t, string(pass), "0")
	code, out, _ := runCLI("verify", "--dir", dir)
	if code != 0 || !strings.Contains(out, "start  orders-api: python 3.12, postgres 16, redis 7, on a clean ubuntu:24.04") ||
		!strings.Contains(out, "have   python 3.12.3, postgres 16.15, redis 7.0.15") || !strings.Contains(out, "READY  4 passed in") {
		t.Errorf("verify: %d\n%s", code, out)
	}
	code, out, _ = runCLI("verify", "--dir", dir, "--json")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var last map[string]any
	if code != 0 || json.Unmarshal([]byte(lines[len(lines)-1]), &last) != nil || last["type"] != "verify.end" {
		t.Errorf("--json: %d %q", code, lines[len(lines)-1])
	}
	for _, l := range lines {
		if !json.Valid([]byte(l)) {
			t.Errorf("not a JSON line: %q", l)
		}
	}
}

func TestVerifyNotReady(t *testing.T) {
	dir := copyRepo(t, "orders-api", false)
	fail, err := os.ReadFile(filepath.Join("..", "..", "internal", "verify", "testdata", "fail.log"))
	if err != nil {
		t.Fatal(err)
	}
	fakeDocker(t, string(fail), "1")
	log := filepath.Join(t.TempDir(), "machine.log")
	code, out, _ := runCLI("verify", "--dir", dir, "--log", log)
	if code != 1 || !strings.Contains(out, "FAIL   ready 1/1: .venv/bin/pytest -q  (exit code 1)") || !strings.Contains(out, "NOT READY  the setup worked, but the ready check failed (exit 1)") ||
		!strings.Contains(out, "| E           redis.exceptions.ConnectionError") {
		t.Errorf("not ready: %d\n%s", code, out)
	}
	if b, _ := os.ReadFile(log); !bytes.Equal(b, fail) {
		t.Errorf("the log file doesn't hold what the machine printed")
	}
}

func TestVerifyExitCodes(t *testing.T) {
	dir := copyRepo(t, "orders-api", false)
	fakeDocker(t, ">>> preconfig: machine 1/1: system packages\n>>> preconfig: FAILED: machine 1/1: system packages (exit code 100)\n", "100")
	if code, _, _ := runCLI("verify", "--dir", dir); code != 2 {
		t.Errorf("a failed setup step gave %d", code)
	}
	t.Setenv("FAKE_DOCKER_DOWN", "1")
	if code, out, _ := runCLI("verify", "--dir", dir); code != 3 || !strings.Contains(out, "ERROR  docker is installed but its daemon isn't answering") {
		t.Errorf("docker down gave %d\n%s", code, out)
	}
	if code, _, errOut := runCLI("verify", "--dir", t.TempDir()); code != exitSpec || !strings.Contains(errOut, "doesn't exist") {
		t.Errorf("no spec gave %d %q", code, errOut)
	}
	if code, _, _ := runCLI("verify", "--dir", dir, "--log", filepath.Join(t.TempDir(), "no", "such", "dir", "x.log")); code != exitUsage {
		t.Errorf("an unwritable log gave %d", code)
	}
}

func TestVerifyPassesSecrets(t *testing.T) {
	dir := copyRepo(t, "orders-api", false)
	fd := fakeDocker(t, ">>> preconfig: ready: passed\n", "0")
	t.Setenv("PAYMENTS_API_KEY", "value-never-printed")
	t.Setenv("EXTRA_VAR", "x")
	if code, _, _ := runCLI("verify", "--dir", dir, "--pass-env", "EXTRA_VAR, ,", "--network", "host", "--image", "ubuntu:24.04"); code != 0 {
		t.Fatalf("code %d", code)
	}
	b, _ := os.ReadFile(filepath.Join(fd, "args"))
	args := string(b)
	if !strings.Contains(args, "-e\nEXTRA_VAR\n") || !strings.Contains(args, "-e\nPAYMENTS_API_KEY\n") || strings.Contains(args, "value-never-printed") {
		t.Errorf("args:\n%s", args)
	}
}
