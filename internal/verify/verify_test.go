package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"preconfiguration.com/preconfig/internal/gen"
	"preconfiguration.com/preconfig/internal/spec"
)

const ordersSpec = `version: 1
name: orders-api
runtimes:
  python: "3.12"
services:
  postgres:
    version: "16"
    user: orders
    password: orders
    database: orders
  redis: "7"
env:
  DATABASE_URL: postgres://orders:orders@localhost:5432/orders
  REDIS_URL: redis://localhost:6379/0
secrets:
  - PAYMENTS_API_KEY
setup:
  - python3 -m venv .venv
  - .venv/bin/pip install -r requirements.txt
ready:
  - .venv/bin/pytest -q
`

func mustSpec(t *testing.T, src string) *spec.Spec {
	t.Helper()
	s, ds := spec.Load("", src)
	if s == nil {
		t.Fatalf("spec rejected: %v", ds)
	}
	return s
}

// clock gives times a tenth of a second apart, so that step times are known.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(100 * time.Millisecond)
	return c.t
}

type fake struct {
	dir    string
	events []Event
	mu     sync.Mutex
}

// setup puts the fake docker in place and returns options that use it.
func setup(t *testing.T, output string, exit string) (*fake, Options) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	f := &fake{dir: t.TempDir()}
	docker, err := filepath.Abs(filepath.Join("testdata", "fake-docker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_DIR", f.dir)
	t.Setenv("FAKE_EXIT", exit)
	t.Setenv("FAKE_OUTPUT", "")
	t.Setenv("FAKE_SLEEP", "")
	t.Setenv("FAKE_DOCKER_DOWN", "")
	if output != "" {
		p := filepath.Join(f.dir, "output")
		if err := os.WriteFile(p, []byte(output), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("FAKE_OUTPUT", p)
	}
	c := &clock{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	return f, Options{
		Dir:    t.TempDir(),
		Docker: docker,
		Now:    c.now,
		Events: func(e Event) {
			f.mu.Lock()
			f.events = append(f.events, e)
			f.mu.Unlock()
		},
	}
}

func (f *fake) args(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "args"))
	if err != nil {
		t.Fatalf("docker run was never called: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func (f *fake) types() string {
	var out []string
	for _, e := range f.events {
		out = append(out, e.Type)
	}
	return strings.Join(out, " ")
}

func recorded(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The recorded runs are what the machine printed in real verify runs on a
// clean Ubuntu 24.04 container: pass.log for the orders-api sample, fail.log
// for the same sample with Redis left out of the spec.
func TestRecordedPass(t *testing.T) {
	f, opts := setup(t, recorded(t, "pass.log"), "0")
	res := Run(context.Background(), mustSpec(t, ordersSpec), opts)
	if res.Code != Ready || !res.Ready {
		t.Fatalf("want ready, got %+v", res)
	}
	if res.Versions != "python 3.12.3, postgres 16.15, redis 7.0.15" {
		t.Errorf("versions = %q", res.Versions)
	}
	if !strings.HasSuffix(res.Message, "passed in "+strings.Split(strings.Split(recorded(t, "pass.log"), "4 passed in ")[1], "\n")[0]) {
		t.Errorf("message = %q", res.Message)
	}
	want := []string{
		"machine 1/5: system packages", "machine 2/5: python 3.12", "machine 3/5: postgres 16", "machine 4/5: redis 7",
		"machine 5/5: environment for login shells", "services 1/2: postgres 16", "services 2/2: redis 7",
		"project 1/2: python3 -m venv .venv", "project 2/2: .venv/bin/pip install -r requirements.txt", "ready 1/1: .venv/bin/pytest -q",
	}
	if len(res.Steps) != len(want) {
		t.Fatalf("steps = %+v", res.Steps)
	}
	for i, s := range res.Steps {
		if s.Name != want[i] || !s.OK || s.Seconds <= 0 {
			t.Errorf("step %d = %+v, want %q ok", i, s, want[i])
		}
	}
	if len(res.Tail) != 0 {
		t.Errorf("a ready run has no tail: %v", res.Tail)
	}
	types := f.types()
	if !strings.HasPrefix(types, "verify.start step.start step.end") || !strings.HasSuffix(types, "step.end verify.end") ||
		!strings.Contains(types, "machine.ready") || !strings.Contains(types, "warning") {
		t.Errorf("events = %s", types)
	}
	for i := 1; i < len(f.events); i++ {
		if f.events[i].T < f.events[i-1].T {
			t.Errorf("event %d goes back in time", i)
		}
	}
	for _, e := range f.events {
		if e.Type == "warning" && e.Fields["message"] != "PAYMENTS_API_KEY is not set" {
			t.Errorf("warning = %v", e.Fields)
		}
	}
	// The script the machine ran is the one gen writes.
	got, err := os.ReadFile(filepath.Join(f.dir, "setup.sh"))
	if err != nil || string(got) != gen.ScriptText(mustSpec(t, ordersSpec)) {
		t.Errorf("the machine was given another script (%v)", err)
	}
}

func TestRecordedFail(t *testing.T) {
	_, opts := setup(t, recorded(t, "fail.log"), "1")
	res := Run(context.Background(), mustSpec(t, ordersSpec), opts)
	if res.Code != NotReady || res.Ready {
		t.Fatalf("want not ready, got %+v", res)
	}
	if res.FailedStep != "ready 1/1: .venv/bin/pytest -q" || res.FailedCode != 1 {
		t.Errorf("failed step %q with code %d", res.FailedStep, res.FailedCode)
	}
	if res.Versions != "python 3.12.3, postgres 16.15" {
		t.Errorf("versions = %q", res.Versions)
	}
	joined := strings.Join(res.Tail, "\n")
	if !strings.Contains(joined, "E           redis.exceptions.ConnectionError: Error 111 connecting to localhost:6379. Connection refused.") {
		t.Errorf("the tail lacks the error line:\n%s", joined)
	}
	if !strings.Contains(joined, "3 failed, 1 passed") {
		t.Errorf("the tail lacks the summary:\n%s", joined)
	}
	if len(res.Tail) > 9 {
		t.Errorf("the tail is %d lines, want at most 9", len(res.Tail))
	}
	last := res.Steps[len(res.Steps)-1]
	if last.Name != res.FailedStep || last.OK {
		t.Errorf("the last step = %+v", last)
	}
}

func TestSetupStepFails(t *testing.T) {
	out := `>>> preconfig: machine 1/3: system packages
Reading package lists...
>>> preconfig: machine 2/3: python 3.13
E: Unable to locate package python3.13
>>> preconfig: FAILED: machine 2/3: python 3.13 (exit code 100)
`
	f, opts := setup(t, out, "100")
	res := Run(context.Background(), mustSpec(t, "version: 1\nruntimes:\n  python: \"3.13\"\nready: [\"true\"]\n"), opts)
	if res.Code != SetupFailed || res.FailedStep != "machine 2/3: python 3.13" || res.FailedCode != 100 {
		t.Fatalf("got %+v", res)
	}
	if strings.Join(res.Tail, "|") != "E: Unable to locate package python3.13" {
		t.Errorf("tail = %q", res.Tail)
	}
	if len(res.Steps) != 2 || !res.Steps[0].OK || res.Steps[1].OK {
		t.Errorf("steps = %+v", res.Steps)
	}
	if !strings.Contains(f.types(), "step.fail") {
		t.Errorf("no step.fail event: %s", f.types())
	}
}

func TestFailedWithAReason(t *testing.T) {
	out := ">>> preconfig: services 1/1: postgres 16\n>>> preconfig: FAILED: postgres didn't start within 60 seconds\n"
	_, opts := setup(t, out, "1")
	res := Run(context.Background(), mustSpec(t, "version: 1\nservices:\n  postgres: \"16\"\nready: [\"true\"]\n"), opts)
	if res.Code != SetupFailed || res.FailedStep != "services 1/1: postgres 16" {
		t.Fatalf("got %+v", res)
	}
	if !strings.Contains(strings.Join(res.Tail, "\n"), "postgres didn't start within 60 seconds") {
		t.Errorf("tail = %q", res.Tail)
	}
}

func TestKilledWithoutAMarker(t *testing.T) {
	out := ">>> preconfig: project 1/1: npm ci\nnpm warn deprecated\n"
	_, opts := setup(t, out, "137")
	res := Run(context.Background(), mustSpec(t, "version: 1\nruntimes:\n  node: \"22\"\nsetup: [npm ci]\nready: [npm test]\n"), opts)
	if res.Code != SetupFailed || res.FailedStep != "project 1/1: npm ci" || res.FailedCode != 137 {
		t.Errorf("got %+v", res)
	}
}

// An exit code of 125 to 127 means docker couldn't start the machine only when
// no step has started; after that it is the setup's own exit code.
func TestCommandNotFoundInAStep(t *testing.T) {
	out := ">>> preconfig: project 1/1: make deps\nbash: line 1: make: command not found\n"
	_, opts := setup(t, out, "127")
	res := Run(context.Background(), mustSpec(t, "version: 1\nsetup: [make deps]\nready: [\"true\"]\n"), opts)
	if res.Code != SetupFailed || res.FailedStep != "project 1/1: make deps" || res.FailedCode != 127 {
		t.Errorf("got %+v", res)
	}
}

func TestReadyKilledWithoutAMarker(t *testing.T) {
	out := ">>> preconfig: ready 1/1: npm test\n"
	_, opts := setup(t, out, "137")
	res := Run(context.Background(), mustSpec(t, "version: 1\nruntimes:\n  node: \"22\"\nready: [npm test]\n"), opts)
	if res.Code != NotReady || res.FailedStep != "ready 1/1: npm test" {
		t.Errorf("got %+v", res)
	}
}

func TestDockerMissing(t *testing.T) {
	_, opts := setup(t, "", "0")
	opts.Docker = filepath.Join(t.TempDir(), "docker")
	res := Run(context.Background(), mustSpec(t, ordersSpec), opts)
	if res.Code != CouldNotRun || !strings.Contains(res.Message, "docker isn't installed") {
		t.Errorf("got %+v", res)
	}
}

func TestDaemonDown(t *testing.T) {
	_, opts := setup(t, "", "0")
	t.Setenv("FAKE_DOCKER_DOWN", "1")
	res := Run(context.Background(), mustSpec(t, ordersSpec), opts)
	if res.Code != CouldNotRun || !strings.Contains(res.Message, "daemon isn't answering: Cannot connect to the Docker daemon") {
		t.Errorf("got %+v", res)
	}
}

func TestImageMissing(t *testing.T) {
	out := "Unable to find image 'ubuntu:99.04' locally\ndocker: Error response from daemon: manifest for ubuntu:99.04 not found.\n"
	_, opts := setup(t, out, "125")
	opts.Image = "ubuntu:99.04"
	res := Run(context.Background(), mustSpec(t, ordersSpec), opts)
	if res.Code != CouldNotRun || !strings.Contains(res.Message, "docker couldn't start the machine: Unable to find image") {
		t.Errorf("got %+v", res)
	}
}

func TestTimeout(t *testing.T) {
	f, opts := setup(t, ">>> preconfig: machine 1/1: system packages\n", "0")
	t.Setenv("FAKE_SLEEP", "30")
	opts.Timeout = time.Second
	start := time.Now()
	res := Run(context.Background(), mustSpec(t, "version: 1\nready: [\"true\"]\n"), opts)
	if time.Since(start) > 15*time.Second {
		t.Errorf("the timeout didn't stop the run: it took %s", time.Since(start))
	}
	if res.Code != SetupFailed || res.FailedStep != "machine 1/1: system packages" || res.Message != "stopped after 1s without finishing" {
		t.Errorf("got %+v", res)
	}
	name, err := os.ReadFile(filepath.Join(f.dir, "killed"))
	if err != nil || !strings.HasPrefix(string(name), "preconfig-verify-") {
		t.Errorf("docker kill wasn't called with the machine's name: %q %v", name, err)
	}
}

func TestArguments(t *testing.T) {
	f, opts := setup(t, ">>> preconfig: ready: passed\n", "0")
	ca := filepath.Join(t.TempDir(), "ca.crt")
	os.WriteFile(ca, []byte("x"), 0o644)
	opts.CAFile = ca
	opts.Network = "host"
	opts.PassEnv = []string{"PAYMENTS_API_KEY", "NOT_SET_ANYWHERE"}
	t.Setenv("PAYMENTS_API_KEY", "secret-value")
	os.Unsetenv("NOT_SET_ANYWHERE")
	res := Run(context.Background(), mustSpec(t, ordersSpec), opts)
	if res.Code != Ready {
		t.Fatalf("got %+v", res)
	}
	args := f.args(t)
	joined := strings.Join(args, " ")
	dir, _ := filepath.Abs(opts.Dir)
	for _, want := range []string{
		"run --rm -i --name preconfig-verify-",
		"--network host",
		"-v " + dir + ":/src:ro",
		":/preconfig:ro",
		"-v " + ca + ":/etc/preconfig/ca.crt:ro",
		"-e SSL_CERT_FILE=/etc/preconfig/ca.crt",
		"-e PIP_CERT=/etc/preconfig/ca.crt",
		"-e NODE_EXTRA_CA_CERTS=/etc/preconfig/ca.crt",
		"-e PAYMENTS_API_KEY ubuntu:24.04 bash -c",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the arguments lack %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "NOT_SET_ANYWHERE") || strings.Contains(joined, "secret-value") {
		t.Errorf("an unset variable or a secret's value reached the arguments:\n%s", joined)
	}
	var wrapper string
	for i, a := range args {
		if a == "-c" {
			wrapper = strings.Join(args[i+1:], "\n")
		}
	}
	for _, want := range []string{"--exclude=./.git", "--exclude=./node_modules", "--exclude=./.venv", "exec bash /preconfig/setup.sh all ready 2>&1", "Acquire::https::CAInfo"} {
		if !strings.Contains(wrapper, want) {
			t.Errorf("the wrapper lacks %q", want)
		}
	}
}

func TestCAFileMissing(t *testing.T) {
	_, opts := setup(t, "", "0")
	opts.CAFile = filepath.Join(t.TempDir(), "none.crt")
	res := Run(context.Background(), mustSpec(t, ordersSpec), opts)
	if res.Code != CouldNotRun || !strings.Contains(res.Message, "the CA file can't be read") {
		t.Errorf("got %+v", res)
	}
}

func TestLogAndJSON(t *testing.T) {
	out := ">>> preconfig: machine 1/1: system packages\nhello\n>>> preconfig: machine: done (no runtimes)\n>>> preconfig: ready 1/1: true\n>>> preconfig: ready: passed\n"
	f, opts := setup(t, out, "0")
	var log bytes.Buffer
	opts.Log = &log
	res := Run(context.Background(), mustSpec(t, "version: 1\nready: [\"true\"]\n"), opts)
	if res.Code != Ready || res.Message != "ready" {
		t.Errorf("got %+v", res)
	}
	if log.String() != out {
		t.Errorf("log = %q", log.String())
	}
	want := "verify.start step.start step.end machine.ready step.start step.end verify.end"
	if f.types() != want {
		t.Errorf("events = %s, want %s", f.types(), want)
	}
	for _, e := range f.events {
		b, err := json.Marshal(e)
		if err != nil || !bytes.HasPrefix(b, []byte(`{"v":1,"ts":"2026-09-29T12:00:0`)) {
			t.Errorf("event JSON = %s (%v)", b, err)
		}
	}
	end := f.events[len(f.events)-1]
	if end.Fields["code"] != 0 || end.Fields["steps"] != 2 {
		t.Errorf("verify.end = %v", end.Fields)
	}
}

func TestTail(t *testing.T) {
	lines := []string{"a", "E   first", "b", "E   second", "E   third", "E   fourth", "c", "d", "e", "f", "g", "h", "", ""}
	got := strings.Join(tail(lines), "|")
	want := "E   second|E   third|E   fourth|c|d|e|f|g|h"
	if got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
	if got := tail([]string{"E   x", "E   x", "y"}); strings.Join(got, "|") != "E   x|y" {
		t.Errorf("repeated error lines: %q", got)
	}
}
