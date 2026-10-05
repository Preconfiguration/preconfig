package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runDoctor(stdin string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := cmdDoctor(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestDoctorCommand(t *testing.T) {
	fail := filepath.Join("..", "..", "internal", "verify", "testdata", "fail.log")
	pass := filepath.Join("..", "..", "internal", "verify", "testdata", "pass.log")
	dir := copyRepo(t, "orders-api", false)
	spec := filepath.Join(dir, "preconfig.yaml")
	src, _ := os.ReadFile(spec)
	noRedis := strings.Replace(string(src), "  redis: \"7\"\n", "", 1)
	if err := os.WriteFile(spec, []byte(noRedis), 0o644); err != nil {
		t.Fatal(err)
	}

	// A passing log: nothing to fix, exit 0.
	if code, out, _ := runDoctor("", "--dir", dir, pass); code != doctorNothing || !strings.Contains(out, "Nothing to fix") {
		t.Errorf("passing log: %d %q", code, out)
	}
	// The failed run: the cause, the fix, exit 1, and the spec untouched.
	code, out, _ := runDoctor("", "--dir", dir, fail)
	if code != doctorExplained || !strings.Contains(out, "add redis 7 under services") || !strings.Contains(out, "rule D401") {
		t.Errorf("failed log: %d %q", code, out)
	}
	if b, _ := os.ReadFile(spec); string(b) != noRedis {
		t.Errorf("doctor without --fix changed the spec")
	}
	// From standard input, as JSON; flags after the file work too.
	code, out, _ = runDoctor(mustRead(t, fail), "-", "--json", "--dir", dir)
	var v struct {
		Tool      string `json:"tool"`
		Diagnosis struct {
			Rule    string `json:"rule"`
			Changes []struct {
				Op, Key, Value string
			} `json:"changes"`
		} `json:"diagnosis"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.Diagnosis.Rule != "D401" || len(v.Diagnosis.Changes) != 1 || !strings.HasPrefix(v.Tool, "preconfig doctor ") {
		t.Errorf("json: %d %v %q", code, err, out)
	}
	// --fix writes the change, rebuilds the files, exits 0.
	code, out, errOut := runDoctor("", "--dir", dir, "--fix", fail)
	if code != doctorNothing || !strings.Contains(out, "+  redis: \"7\"") || errOut != "" {
		t.Errorf("--fix: %d %q %q", code, out, errOut)
	}
	if b, _ := os.ReadFile(spec); string(b) != string(src) {
		t.Errorf("--fix gave:\n%s", b)
	}
	if code, out, _ := runCLI("check", "--dir", dir); code != exitOK {
		t.Errorf("after --fix, check finds: %s", out)
	}
	// A failure no rule explains: exit 2, nothing changed.
	odd := ">>> preconfig: project 1/1: make\nsomething odd\n>>> preconfig: FAILED: project 1/1: make (exit code 2)\n"
	if code, out, _ := runDoctor(odd, "--dir", dir, "--fix"); code != doctorUnknown || !strings.Contains(out, "no rule") {
		t.Errorf("unknown: %d %q", code, out)
	}
	// Usage.
	if code, _, _ := runDoctor("", "a.log", "b.log"); code != exitUsage {
		t.Errorf("two logs: %d", code)
	}
	if code, _, _ := runDoctor("", "--dir", dir, filepath.Join(dir, "no-such.log")); code != exitUsage {
		t.Errorf("missing log: %d", code)
	}
	// --fix with no spec to change.
	empty := t.TempDir()
	if code, _, errOut := runDoctor(mustRead(t, fail), "--dir", empty, "--fix"); code != exitSpec || !strings.Contains(errOut, "doesn't exist") {
		t.Errorf("no spec: %d %q", code, errOut)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
