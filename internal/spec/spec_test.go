package spec

import (
	"strings"
	"testing"
)

const orders = `version: 1
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

func TestLoadsTheSample(t *testing.T) {
	s, ds := Load("", orders)
	if s == nil {
		t.Fatalf("spec rejected: %v", ds)
	}
	if len(ds) != 0 {
		t.Errorf("unexpected diagnostics: %v", ds)
	}
	if s.Name != "orders-api" || s.Python != "3.12" || s.Node != "" || s.Go != "" {
		t.Errorf("runtimes wrong: %+v", s)
	}
	pg, ok := s.Service("postgres")
	if !ok || pg.Major != 16 || pg.User != "orders" || pg.Database != "orders" {
		t.Errorf("postgres wrong: %+v", pg)
	}
	if r, ok := s.Service("redis"); !ok || r.Major != 7 {
		t.Errorf("redis wrong: %+v", r)
	}
	if len(s.Env) != 2 || s.Env[0].Key != "DATABASE_URL" {
		t.Errorf("env wrong: %+v", s.Env)
	}
	if strings.Join(s.Targets, ",") != "devcontainer,copilot,cursor,cloud-init,script" {
		t.Errorf("default targets = %v", s.Targets)
	}
	if s.Base.Name != "ubuntu-24.04" {
		t.Errorf("default base = %q", s.Base.Name)
	}
}

func TestDefaults(t *testing.T) {
	s, ds := Load("", "version: 1\nservices:\n  postgres: \"17\"\nready:\n  - true\n")
	if s == nil {
		t.Fatalf("rejected: %v", ds)
	}
	pg, _ := s.Service("postgres")
	if s.Name != "project" || pg.User != "postgres" || pg.Password != "postgres" || pg.Database != "postgres" {
		t.Errorf("defaults wrong: name %q, %+v", s.Name, pg)
	}
}

// codeAt finds the diagnostic with a code and returns its line, or -1.
func codeAt(ds []Diagnostic, code string) int {
	for _, d := range ds {
		if d.Code == code {
			return d.Line
		}
	}
	return -1
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name, src, code string
		line            int
		hint            string
	}{
		{"yaml error", "version: 1\nname: [x\n", "P001", 2, ""},
		{"not a map", "- a\n", "S001", 1, ""},
		{"empty", "", "S001", 1, ""},
		{"unknown key", "version: 1\nruntime:\n  node: \"22\"\n", "S002", 2, "runtimes"},
		{"no version", "name: x\n", "S003", 1, "version: 1"},
		{"version 2", "version: 2\n", "S003", 1, ""},
		{"bad name", "version: 1\nname: my app\n", "S010", 2, ""},
		{"bad base", "version: 1\nbase: debian-13\n", "S011", 2, ""},
		{"unknown runtime", "version: 1\nruntimes:\n  nodejs: \"22\"\n", "S020", 3, "node"},
		{"java later", "version: 1\nruntimes:\n  java: \"21\"\n", "S020", 3, "planned"},
		{"node minor", "version: 1\nruntimes:\n  node: \"22.11\"\n", "S021", 3, "major"},
		{"node odd", "version: 1\nruntimes:\n  node: \"21\"\n", "S021", 3, ""},
		{"python 2", "version: 1\nruntimes:\n  python: \"2.7\"\n", "S022", 3, ""},
		{"python old", "version: 1\nruntimes:\n  python: \"3.9\"\n", "S022", 3, ""},
		{"go bad", "version: 1\nruntimes:\n  go: \"2\"\n", "S023", 3, ""},
		{"go old", "version: 1\nruntimes:\n  go: \"1.21\"\n", "S023", 3, ""},
		{"unknown tool", "version: 1\ntools: [npmx]\n", "S030", 2, ""},
		{"tool typo", "version: 1\nruntimes: {node: \"22\"}\ntools: [pnmp]\n", "S030", 3, "pnpm"},
		{"tool version", "version: 1\nruntimes: {node: \"22\"}\ntools: [pnpm@latest]\n", "S030", 3, ""},
		{"tool twice", "version: 1\nruntimes: {node: \"22\"}\ntools: [pnpm, pnpm@10]\n", "S030", 3, ""},
		{"tool runtime", "version: 1\ntools: [pnpm]\n", "S031", 2, "node"},
		{"bad package", "version: 1\npackages: [LibPQ]\n", "S040", 2, ""},
		{"unknown service", "version: 1\nservices:\n  mysql: \"8\"\n", "S050", 3, "MySQL"},
		{"service key", "version: 1\nservices:\n  postgres:\n    version: \"16\"\n    port: 5433\n", "S050", 5, ""},
		{"redis user", "version: 1\nservices:\n  redis:\n    version: \"7\"\n    user: x\n", "S050", 5, ""},
		{"service no version", "version: 1\nservices:\n  postgres:\n    user: x\n", "S050", 4, ""},
		{"postgres old", "version: 1\nservices:\n  postgres: \"12\"\n", "S051", 3, ""},
		{"redis 6", "version: 1\nservices:\n  redis: \"6\"\n", "S051", 3, ""},
		{"bad password", "version: 1\nservices:\n  postgres:\n    version: \"16\"\n    password: \"it's\"\n", "S050", 5, ""},
		{"control character in password", "version: 1\nservices:\n  postgres:\n    version: \"16\"\n    password: \"ab\\x01cd\"\n", "S050", 5, "control character"},
		{"line separator in password", "version: 1\nservices:\n  postgres:\n    version: \"16\"\n    password: \"ab\\u2028ef\"\n", "S050", 5, "control character"},
		{"next line in password", "version: 1\nservices:\n  postgres:\n    version: \"16\"\n    password: \"ab\\u0085ef\"\n", "S050", 5, "control character"},
		{"env key", "version: 1\nenv:\n  1BAD: x\n", "S060", 3, ""},
		{"env secret", "version: 1\nenv:\n  NPM_TOKEN: abc\n", "S061", 3, "secrets"},
		{"env map", "version: 1\nenv:\n  A: [1]\n", "S060", 3, ""},
		{"secret name", "version: 1\nsecrets: [\"a b\"]\n", "S070", 2, ""},
		{"secret twice", "version: 1\nenv:\n  TOKEN_NAME: \"\"\nsecrets: [TOKEN_NAME]\n", "S071", 3, ""},
		{"setup scalar", "version: 1\nsetup: npm ci\n", "S080", 2, "list"},
		{"setup empty", "version: 1\nsetup: [\"\"]\n", "S080", 2, ""},
		{"unknown target", "version: 1\ntargets: [copliot]\n", "S090", 2, "copilot"},
		{"no targets", "version: 1\ntargets: []\n", "S090", 2, ""},
		{"bad repo", "version: 1\nrepo: github.com/acme/x\n", "S100", 2, ""},
		{"wrong type", "version: 1\nruntimes: node\n", "S004", 2, ""},
		{"control in env", "version: 1\nenv:\n  A: \"a\\x01b\"\n", "S060", 3, "U+0001"},
		{"separator in env", "version: 1\nenv:\n  A: \"a\\u2028b\"\n", "S060", 3, "U+2028"},
		{"control in command", "version: 1\nsetup: [\"echo \\x7f\"]\n", "S080", 2, "U+007F"},
		{"control in repo", "version: 1\nrepo: \"https://example.com/a\\x01\"\n", "S100", 2, ""},
	}
	for _, c := range cases {
		s, ds := Load("", c.src)
		line := codeAt(ds, c.code)
		if line != c.line {
			t.Errorf("%s: want %s at line %d, got %v", c.name, c.code, c.line, ds)
			continue
		}
		if c.hint != "" {
			found := false
			for _, d := range ds {
				if d.Code == c.code && (strings.Contains(d.Hint, c.hint) || strings.Contains(d.Message, c.hint)) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: the message doesn't mention %q: %v", c.name, c.hint, ds)
			}
		}
		for _, d := range ds {
			if d.Code == c.code && d.Severity == Error && s != nil {
				t.Errorf("%s: an error still returned a spec", c.name)
			}
		}
	}
}

func TestWarningsAndNotes(t *testing.T) {
	cases := []struct {
		name, src, code, sev string
	}{
		{"no ready", "version: 1\n", "S081", Warning},
		{"tool not listed", "version: 1\nruntimes: {node: \"22\"}\nsetup: [pnpm install]\nready: [pnpm test]\n", "S082", Warning},
		{"runtime not listed", "version: 1\nsetup: [npm ci]\nready: [\"true\"]\n", "S083", Warning},
		{"venv without python", "version: 1\nready: [.venv/bin/pytest]\n", "S083", Warning},
		{"env points at redis", "version: 1\nenv:\n  REDIS_URL: redis://localhost:6379/0\nready: [\"true\"]\n", "S062", Warning},
		{"env points at postgres", "version: 1\nenv:\n  DB: postgres://u:p@127.0.0.1:5432/d\nready: [\"true\"]\n", "S062", Warning},
		{"python via uv", "version: 1\nruntimes: {python: \"3.13\"}\nready: [\"true\"]\n", "S024", Note},
		{"cursor adds script", "version: 1\ntargets: [cursor]\nready: [\"true\"]\n", "S091", Note},
		{"package twice", "version: 1\npackages: [git, git]\nready: [\"true\"]\n", "S040", Warning},
	}
	for _, c := range cases {
		s, ds := Load("", c.src)
		if s == nil {
			t.Errorf("%s: rejected: %v", c.name, ds)
			continue
		}
		ok := false
		for _, d := range ds {
			if d.Code == c.code && d.Severity == c.sev {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s: want %s %s, got %v", c.name, c.sev, c.code, ds)
		}
	}
}

func TestCursorPullsInScript(t *testing.T) {
	s, _ := Load("", "version: 1\ntargets: [cursor, copilot]\nready: [\"true\"]\n")
	if strings.Join(s.Targets, ",") != "copilot,cursor,script" {
		t.Errorf("targets = %v", s.Targets)
	}
}

func TestVersionsKeepTheirText(t *testing.T) {
	// YAML reads 3.10 as a number; the spec must keep "3.10", not 3.1.
	s, ds := Load("", "version: 1\nruntimes:\n  python: 3.10\n  go: 1.25.3\nready: [\"true\"]\n")
	if s == nil {
		t.Fatalf("rejected: %v", ds)
	}
	if s.Python != "3.10" || s.PythonMinor() != "3.10" || s.Go != "1.25.3" || s.GoMinor() != "1.25" {
		t.Errorf("python %q go %q", s.Python, s.Go)
	}
}

func TestTabsAreFine(t *testing.T) {
	s, ds := Load("", "version: 1\nenv:\n  A: \"a\\tb\"\nready: [\"printf 'x\\ty'\"]\n")
	if s == nil || s.Env[0].Value != "a\tb" {
		t.Errorf("a tab was refused: %v", ds)
	}
}

func TestDiagnosticString(t *testing.T) {
	d := Diagnostic{File: "preconfig.yaml", Line: 3, Col: 5, Severity: Error, Code: "S002", Message: "unknown key \"x\"", Hint: "Did you mean \"y\"?"}
	want := "preconfig.yaml:3:5: error S002: unknown key \"x\"\n    Did you mean \"y\"?"
	if d.String() != want {
		t.Errorf("String() = %q", d.String())
	}
}

func FuzzLoad(f *testing.F) {
	f.Add(orders)
	f.Add("version: 1\nruntimes: {node: \"22\"}\ntools: [pnpm@10]\n")
	f.Add("version: 1\nservices:\n  postgres:\n    version: \"16\"\n")
	f.Fuzz(func(t *testing.T, src string) {
		s, ds := Load("", src)
		if s != nil && HasErrors(ds) {
			t.Fatal("a spec with errors was returned")
		}
		if s == nil && !HasErrors(ds) {
			t.Fatal("no spec and no error")
		}
	})
}
