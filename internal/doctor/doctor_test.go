package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/kb"
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

func loadSpec(t testing.TB, src string) *spec.Spec {
	t.Helper()
	s, ds := spec.Load(kb.SpecPath, src)
	if s == nil {
		t.Fatalf("spec doesn't load: %v", ds)
	}
	return s
}

// setupLog wraps lines in the setup script's markers, as a failed step.
func setupLog(step string, code int, body ...string) string {
	lines := []string{
		">>> preconfig: machine 1/1: system packages",
		"Reading package lists...",
		">>> preconfig: machine: done (python 3.12.3)",
		">>> preconfig: " + step,
	}
	lines = append(lines, body...)
	lines = append(lines, ">>> preconfig: FAILED: "+step+" (exit code "+itoa(code)+")")
	return strings.Join(lines, "\n") + "\n"
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestKinds(t *testing.T) {
	cases := map[string]string{
		KindSetup:     ">>> preconfig: machine 1/2: system packages\nHit:1 http://archive.ubuntu.com noble InRelease\n",
		KindVerify:    "verify    0.0s  start  orders-api: python 3.12, on a clean ubuntu:24.04\nverify    2.5s  step   machine 1/5: system packages\n",
		KindEvents:    `{"v":1,"ts":"2026-09-29T21:00:00Z","t":0,"type":"verify.start"}` + "\n",
		KindDocker:    "#0 building with \"default\" instance using docker driver\n#7 [3/3] RUN bash /opt/preconfig/setup.sh machine\n#7 0.261 >>> preconfig: machine 1/5: system packages\n",
		KindActions:   "2026-09-30T10:00:00.1234567Z ##[group]Run actions/checkout@v7\n2026-09-30T10:00:00.2234567Z with:\n",
		KindCloudInit: "Cloud-init v. 26.1 running 'modules:final' at Wed, 30 Sep 2026 10:00:00 +0000.\n>>> preconfig: machine 1/5: system packages\n",
		KindText:      "Traceback (most recent call last):\n  File \"x.py\", line 1\n",
	}
	for want, text := range cases {
		if got := Read(text).Kind; got != want {
			t.Errorf("kind of %q = %s, want %s", firstLineOf(text), got, want)
		}
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func TestSteps(t *testing.T) {
	lg := Read(setupLog("project 2/2: pip install -r requirements.txt", 1, "ERROR: boom"))
	if len(lg.Steps) != 2 {
		t.Fatalf("steps = %+v", lg.Steps)
	}
	st := lg.Steps[1]
	if !st.Failed || st.Code != 1 || st.Phase != "project" || st.Name != "project 2/2: pip install -r requirements.txt" {
		t.Errorf("failed step = %+v", st)
	}
	if !lg.Failed || lg.Passed {
		t.Errorf("failed=%v passed=%v", lg.Failed, lg.Passed)
	}
	// A reason line from the script's own fail().
	lg = Read(">>> preconfig: services 2/2: redis 7\nnope\n>>> preconfig: FAILED: redis 7 is not answering on localhost:6379\n")
	if !lg.Steps[0].Failed || lg.Steps[0].Reason != "redis 7 is not answering on localhost:6379" {
		t.Errorf("reason step = %+v", lg.Steps[0])
	}
	// A passing run.
	lg = Read(">>> preconfig: ready 1/1: pytest -q\n4 passed\n>>> preconfig: ready: passed\n")
	if !lg.Passed || lg.Failed {
		t.Errorf("passing run read as failed: %+v", lg)
	}
	// The exit code is the step's own: 127 for a command that isn't there.
	if d := Diagnose(setupLog("ready 1/1: make test", 127, "/preconfig/setup.sh: line 105: make: command not found"), nil); d.ExitCode != 127 {
		t.Errorf("exit code = %d, want 127", d.ExitCode)
	}
}

// A log that stops in the middle of a step, with no error and no result, may
// have been cut off: it is neither passing nor failed.
func TestCutOffLogIsUnclear(t *testing.T) {
	cut := ">>> preconfig: machine 1/1: system packages\nReading package lists...\n>>> preconfig: machine: done (python 3.12.3)\n" +
		">>> preconfig: project 1/2: python3 -m venv .venv\n"
	d := Diagnose(cut, nil)
	if d.Outcome != Unclear || !strings.Contains(d.Cause, "cut off") || len(d.Changes) != 0 {
		t.Errorf("cut-off log: %+v", d)
	}
}

func TestPassingLogGetsNoDiagnosis(t *testing.T) {
	d := Diagnose(">>> preconfig: ready 1/1: pytest -q\nE   AssertionError in a docstring example\n>>> preconfig: ready: passed\n", nil)
	if d.Outcome != Passed || d.Cause != "" || len(d.Changes) != 0 {
		t.Errorf("passing log: %+v", d)
	}
}

// Each rule, on a few lines in the format of the program that prints them.
// These lines are written for the test; the corpus in testdata/doctor has the
// real logs.
func TestRules(t *testing.T) {
	s := loadSpec(t, ordersSpec)
	noRedis := loadSpec(t, strings.Replace(ordersSpec, "  redis: \"7\"\n", "", 1))
	noPg := loadSpec(t, strings.Replace(ordersSpec, "  postgres:\n    version: \"16\"\n    user: orders\n    password: orders\n    database: orders\n", "", 1))
	withTypo := loadSpec(t, strings.Replace(ordersSpec, "secrets:\n", "packages:\n  - libpq-devv\n\nsecrets:\n", 1))
	cases := []struct {
		name     string
		log      string
		spec     *spec.Spec
		rule     string
		category string
		change   string // Change.String(), or "" for none
	}{
		{"apt unknown package", setupLog("machine 1/5: system packages", 100, "E: Unable to locate package libpq-devv"), withTypo, "D101", CatSpec, "replace libpq-devv with libpq-dev in packages"},
		{"network nodesource", setupLog("machine 2/5: node 22", 56, "curl: (56) CONNECT tunnel failed, response 403"), nil, "D102", CatNetwork, ""},
		{"go toolchain blocked", setupLog("machine 2/4: go 1.25", 1, "go: downloading go1.25.0 (linux/amd64)", "go: download go1.25.0: golang.org/toolchain@v0.0.1-go1.25.0.linux-amd64: reading https://proxy.golang.org/golang.org/toolchain/@v/v0.0.1-go1.25.0.linux-amd64.zip: 403 Forbidden"), nil, "D102", CatNetwork, ""},
		{"network by name", setupLog("machine 2/5: node 22", 6, "curl: (6) Could not resolve host: deb.nodesource.com"), nil, "D102", CatNetwork, ""},
		{"tls", setupLog("project 2/2: pip install -r requirements.txt", 1, "Could not fetch URL https://pypi.org/simple/requests/: There was a problem confirming the ssl certificate: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed"), nil, "D102", CatNetwork, ""},
		{"compiler", setupLog("project 2/2: pip install -r requirements.txt", 1, "      error: command 'x86_64-linux-gnu-gcc' failed: No such file or directory"), s, "D302", CatSpec, "add build-essential to packages"},
		{"header", setupLog("project 2/2: pip install -r requirements.txt", 1, "      src/x.c:4:10: fatal error: ffi.h: No such file or directory"), s, "D303", CatSpec, "add libffi-dev to packages"},
		{"unknown header", setupLog("project 2/2: pip install", 1, "      fatal error: wibble/wobble.h: No such file or directory"), s, "D303", CatSpec, ""},
		{"pg_config", setupLog("project 2/2: pip install", 1, "    Error: pg_config executable not found."), s, "D301", CatSpec, "add libpq-dev to packages"},
		{"python version", setupLog("project 2/2: pip install", 1, "ERROR: Package 'newlib' requires a different Python: 3.12.3 not in '>=3.13'"), s, "D307", CatSpec, "set runtimes.python to 3.13"},
		{"python version below", setupLog("project 2/2: pip install", 1, "ERROR: Package 'oldlib' requires a different Python: 3.12.3 not in '<3.12,>=3.9'"), s, "D307", CatSpec, "set runtimes.python to 3.10"},
		{"no such version", setupLog("project 2/2: pip install", 1, "ERROR: Could not find a version that satisfies the requirement requests==99.0.0 (from versions: 2.32.4, 2.32.5)", "ERROR: No matching distribution found for requests==99.0.0"), s, "D308", CatRepository, ""},
		{"requirements file", setupLog("project 2/2: pip install", 1, "ERROR: Could not open requirements file: [Errno 2] No such file or directory: 'requirements-dev.txt'"), s, "D309", CatRepository, ""},
		{"hashes", setupLog("project 2/2: pip install", 1, "ERROR: THESE PACKAGES DO NOT MATCH THE HASHES FROM THE REQUIREMENTS FILE."), s, "D310", CatRepository, ""},
		{"uv lock", setupLog("project 1/1: uv sync --locked", 2, "error: The lockfile at `uv.lock` needs to be updated, but `--locked` was provided. To update the lockfile, run `uv lock`."), s, "D311", CatRepository, ""},
		{"pnpm lock", setupLog("project 1/1: pnpm install --frozen-lockfile", 1, " ERR_PNPM_OUTDATED_LOCKFILE  Cannot install with \"frozen-lockfile\" because pnpm-lock.yaml is not up to date with package.json"), nil, "D311", CatRepository, ""},
		{"node engine", setupLog("project 1/1: pnpm install", 1, " ERR_PNPM_UNSUPPORTED_ENGINE  Unsupported environment (bad pnpm and/or Node.js version)", "Expected version: >=22"), nil, "D312", CatSpec, "set runtimes.node to 22"},
		{"go version", setupLog("project 1/1: go mod download", 1, "go: go.mod requires go >= 1.26 (running go 1.25.0; GOTOOLCHAIN=local)"), nil, "D315", CatSpec, "set runtimes.go to 1.26"},
		{"uv missing", setupLog("project 2/2: uv pip install -r requirements.txt", 127, "bash: line 79: uv: command not found"), s, "D306", CatSpec, "add uv to tools"},
		{"python missing", setupLog("project 1/2: python3 -m venv .venv", 127, "bash: line 80: python3: command not found"), loadSpec(t, strings.Replace(ordersSpec, "runtimes:\n  python: \"3.12\"\n\n", "", 1)), "D306", CatSpec, "set runtimes.python to 3.12"},
		{"cli missing", setupLog("ready 1/1: pytest -q", 1, "E       FileNotFoundError: [Errno 2] No such file or directory: 'sqlite3'"), s, "D306", CatSpec, "add sqlite3 to packages"},
		{"shared library", setupLog("ready 1/1: pytest -q", 1, "E   ImportError: failed to find libmagic.  Check your installation"), s, "D305", CatSpec, "add libmagic1t64 to packages"},
		{"shared object", setupLog("ready 1/1: pytest -q", 1, "E   ImportError: libGL.so.1: cannot open shared object file: No such file or directory"), s, "D305", CatSpec, "add libgl1 to packages"},
		{"locale", setupLog("ready 1/1: pytest -q", 1, "E   locale.Error: unsupported locale setting"), s, "D405", CatSpec, "add locales-all to packages"},
		{"redis refused", setupLog("ready 1/1: pytest -q", 1, "E   redis.exceptions.ConnectionError: Error 111 connecting to localhost:6379. Connection refused."), noRedis, "D401", CatSpec, "add redis 7 under services"},
		{"refused, with asserts around it", setupLog("ready 1/1: pytest -q", 1, "E       assert client.ping()", "E   redis.exceptions.ConnectionError: Error 111 connecting to localhost:6379. Connection refused.", "FAILED tests/test_cache.py::test_ping - redis.exceptions.ConnectionError", "=== 1 failed in 0.2s ==="), noRedis, "D401", CatSpec, "add redis 7 under services"},
		{"python headers", setupLog("project 2/2: pip install", 1, "      ./psycopg/psycopg.h:35:10: fatal error: Python.h: No such file or directory"), s, "D303", CatSpec, "add python3-dev to packages"},
		{"postgres refused", setupLog("ready 1/1: pytest -q", 1, `E   psycopg.OperationalError: connection failed: connection to server at "127.0.0.1", port 5432 failed: Connection refused`), noPg, "D401", CatSpec, "add postgres 16 under services, with user orders and database orders"},
		{"postgres refused, running", setupLog("ready 1/1: pytest -q", 1, `E   psycopg.OperationalError: connection failed: connection to server at "127.0.0.1", port 5432 failed: Connection refused`), s, "D401", CatPlatform, ""},
		{"wrong port", setupLog("ready 1/1: pytest -q", 1, `E   psycopg.OperationalError: connection failed: connection to server at "127.0.0.1", port 5433 failed: Connection refused`), loadSpec(t, strings.Replace(ordersSpec, "localhost:5432/orders", "localhost:5433/orders", 1)), "D401", CatSpec, "set env DATABASE_URL to postgres://orders:***@localhost:5432/orders"},
		{"node refused", setupLog("ready 1/1: pnpm test", 1, "Error: connect ECONNREFUSED 127.0.0.1:6379"), noRedis, "D401", CatSpec, "add redis 7 under services"},
		{"pg auth", setupLog("ready 1/1: pytest -q", 1, `E   psycopg.OperationalError: connection failed: connection to server at "127.0.0.1", port 5432 failed: FATAL:  password authentication failed for user "orders"`), loadSpec(t, strings.Replace(ordersSpec, "orders:orders@", "orders:secret@", 1)), "D402", CatSpec, "set env DATABASE_URL to postgres://orders:***@localhost:5432/orders"},
		{"pg database", setupLog("ready 1/1: pytest -q", 1, `E   psycopg.OperationalError: connection failed: FATAL:  database "shop" does not exist`), loadSpec(t, strings.Replace(ordersSpec, "5432/orders", "5432/shop", 1)), "D403", CatSpec, "set env DATABASE_URL to postgres://orders:***@localhost:5432/orders"},
		{"env from service", setupLog("ready 1/1: pytest -q", 1, "E   KeyError: 'DATABASE_URL'"), loadSpec(t, strings.Replace(ordersSpec, "  DATABASE_URL: postgres://orders:orders@localhost:5432/orders\n", "", 1)), "D404", CatSpec, "add DATABASE_URL under env: postgres://orders:***@localhost:5432/orders"},
		{"secret unset", setupLog("ready 1/1: pytest -q", 1, ">>> preconfig: warning: PAYMENTS_API_KEY is not set", "E   KeyError: 'PAYMENTS_API_KEY'"), s, "D404", CatSecret, ""},
		{"unknown variable", setupLog("ready 1/1: pytest -q", 1, "E   KeyError: 'FEATURE_FLAGS'"), s, "D404", CatSpec, ""},
		{"registry auth", setupLog("project 2/2: pip install", 1, "ERROR: HTTP error 401 while getting https://pkgs.example.com/simple/x/", "401 Client Error: Unauthorized for url: https://pkgs.example.com/simple/x/"), s, "D314", CatSecret, ""},
		{"port taken", ">>> preconfig: services 1/2: postgres 16\n2026-09-30 18:37:10.619 UTC [5690] LOG:  could not bind IPv4 address \"127.0.0.1\": Address already in use\n2026-09-30 18:37:10.619 UTC [5690] HINT:  Is another postmaster already running on port 5432? If not, wait a few seconds and retry.\n>>> preconfig: FAILED: services 1/2: postgres 16 (exit code 1)\n", s, "D201", CatPlatform, ""},
		{"apt busy", setupLog("machine 1/5: system packages", 100, "E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 1234 (unattended-upgr)"), s, "D105", CatPlatform, ""},
		{"disk full", setupLog("project 2/2: pip install", 1, "ERROR: Could not install packages due to an OSError: [Errno 28] No space left on device"), s, "D106", CatPlatform, ""},
		{"tests failed", setupLog("ready 1/1: pytest -q", 1, "E       assert 1 == 2", "FAILED tests/test_orders.py::test_count_is_cached_in_redis - assert 1 == 2", "=================== 1 failed, 3 passed in 0.25s ==================="), s, "D408", CatCode, ""},
		{"image missing", setupLog("services 1/1: postgres 16", 1, "Error response from daemon: manifest for postgres:99 not found: manifest unknown"), s, "D109", CatPlatform, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Diagnose(tc.log, tc.spec)
			if d.Rule != tc.rule || d.Category != tc.category {
				t.Fatalf("rule %s %s, want %s %s: %s", d.Rule, d.Category, tc.rule, tc.category, d.Cause)
			}
			got := ""
			if len(d.Changes) > 0 {
				got = d.Changes[0].String()
			}
			if got != tc.change {
				t.Errorf("change %q, want %q", got, tc.change)
			}
			if len(d.Evidence) == 0 {
				t.Errorf("no evidence")
			}
		})
	}
}

func TestUnknownIsNeverAFix(t *testing.T) {
	d := Diagnose(setupLog("project 2/2: make", 2, "something odd happened", "Error 2: frobnication failed"), loadSpec(t, ordersSpec))
	if d.Category != CatUnknown || len(d.Changes) != 0 || d.Rule != "" {
		t.Fatalf("got %+v", d)
	}
	if len(d.Evidence) == 0 || !strings.Contains(d.Evidence[0].Text, "frobnication") && !strings.Contains(d.Evidence[len(d.Evidence)-1].Text, "FAILED") {
		t.Errorf("evidence %+v", d.Evidence)
	}
}

func TestDockerAndActionsAndVerifyOutput(t *testing.T) {
	docker := strings.Join([]string{
		`#0 building with "default" instance using docker driver`,
		`#6 [3/3] RUN bash /opt/preconfig/setup.sh machine`,
		`#6 0.201 >>> preconfig: machine 1/5: system packages`,
		`#6 9.812 E: Unable to locate package libpq-devv`,
		`#6 9.815 >>> preconfig: FAILED: machine 1/5: system packages (exit code 100)`,
		`#6 ERROR: process "/bin/sh -c bash /opt/preconfig/setup.sh machine" did not complete successfully: exit code: 100`,
		`ERROR: failed to solve: process "/bin/sh -c bash /opt/preconfig/setup.sh machine" did not complete successfully: exit code: 100`,
	}, "\n")
	d := Diagnose(docker, loadSpec(t, strings.Replace(ordersSpec, "secrets:\n", "packages:\n  - libpq-devv\n\nsecrets:\n", 1)))
	if d.Log != KindDocker || d.Rule != "D101" || d.Evidence[0].N != 4 {
		t.Errorf("docker: %+v", d)
	}
	actions := strings.Join([]string{
		"2026-09-30T10:00:00.1000000Z ##[group]Run actions/setup-python@v7",
		"2026-09-30T10:00:00.2000000Z with:",
		"2026-09-30T10:00:00.3000000Z   python-version: 3.99",
		"2026-09-30T10:00:00.4000000Z ##[endgroup]",
		"2026-09-30T10:00:01.0000000Z ##[error]The version '3.99' with architecture 'x64' was not found for Ubuntu 24.04.",
	}, "\n")
	d = Diagnose(actions, nil)
	if d.Log != KindActions || d.Rule != "D110" || d.Step != "Run actions/setup-python@v7" {
		t.Errorf("actions: %+v", d)
	}
	verify := strings.Join([]string{
		"verify    0.0s  start  orders-api: python 3.12, postgres 16, on a clean ubuntu:24.04",
		"verify   51.4s  step   ready 1/1: .venv/bin/pytest -q",
		"verify   55.1s  FAIL   ready 1/1: .venv/bin/pytest -q  (exit code 1)",
		"verify   55.1s  NOT READY  the setup worked, but the ready check failed (exit 1)",
		"               the last lines it printed:",
		"               | E           redis.exceptions.ConnectionError: Error 111 connecting to localhost:6379. Connection refused.",
		"               | 3 failed, 1 passed in 0.31s",
	}, "\n")
	d = Diagnose(verify, loadSpec(t, strings.Replace(ordersSpec, "  redis: \"7\"\n", "", 1)))
	if d.Log != KindVerify || d.Rule != "D401" || d.Step != "ready 1/1: .venv/bin/pytest -q" || d.ExitCode != 1 {
		t.Errorf("verify: %+v", d)
	}
}

func TestEventsNameTheStep(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "verify", "fail-no-redis.jsonl"))
	if err != nil {
		t.Skip("no recorded events")
	}
	d := Diagnose(string(b), nil)
	if d.Log != KindEvents || d.Step != "ready 1/1: .venv/bin/pytest -q" || d.Category != CatUnknown {
		t.Errorf("events: %+v", d)
	}
	if !strings.Contains(d.Advice, "--log") {
		t.Errorf("advice %q doesn't point at --log", d.Advice)
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"postgres://orders:hunter2@localhost:5432/orders": "postgres://orders:***@localhost:5432/orders",
		"token ghp_abcdefghijklmnopqrstuvwxyz0123456789":  "token ghp_***",
		"export API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz": "export API_KEY=***",
		"Authorization: Bearer abc.def.ghi":               "Authorization: ***",
		"AWS key AKIAABCDEFGHIJKLMNOP in the log":         "AWS key AKIA*** in the log",
		"PAYMENTS_API_KEY is not set":                     "PAYMENTS_API_KEY is not set",
		"E   KeyError: 'PAYMENTS_API_KEY'":                "E   KeyError: 'PAYMENTS_API_KEY'",
		`password authentication failed for user "orders"`: `password authentication failed for user "orders"`,
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
	// Evidence is masked too.
	d := Diagnose(setupLog("project 2/2: pip install", 1, "Could not fetch URL https://user:s3cretpass@pkgs.example.com/simple/: connection error: Name or service not known"), nil)
	for _, l := range d.Evidence {
		if strings.Contains(l.Text, "s3cretpass") {
			t.Errorf("evidence leaks a password: %q", l.Text)
		}
	}
}

func TestPickVersion(t *testing.T) {
	cases := []struct {
		spec   string
		minors []int
		prefix string
		want   string
	}{
		{">=3.13", kb.PythonMinors, "3.", "3.13"},
		{"<3.12,>=3.9", kb.PythonMinors, "3.", "3.10"},
		{">=3.11,<3.13", kb.PythonMinors, "3.", "3.11"},
		{">3.14", kb.PythonMinors, "3.", ""},
		{"!=3.10,>=3.10", kb.PythonMinors, "3.", "3.11"},
		{">=22", kb.NodeMajors, "", "22"},
		{">=18", kb.NodeMajors, "", "20"},
	}
	for _, c := range cases {
		if got := pickVersion(c.spec, c.minors, c.prefix); got != c.want {
			t.Errorf("pickVersion(%q) = %q, want %q", c.spec, got, c.want)
		}
	}
}
