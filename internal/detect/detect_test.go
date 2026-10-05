package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
	"preconfiguration.com/preconfig/internal/yaml"
)

func readRepo(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, p := range Paths {
		if b, err := os.ReadFile(filepath.Join(dir, p)); err == nil {
			files[p] = string(b)
		}
	}
	return files
}

func load(t *testing.T, src string) *spec.Spec {
	t.Helper()
	s, ds := spec.Load("", src)
	if s == nil {
		t.Fatalf("the draft doesn't load: %v\n%s", ds, src)
	}
	s.Source = ""
	return s
}

// Each sample repository's preconfig.yaml was written by a person. detect,
// reading only the repository's other files, must arrive at the same spec.
func TestSamplesRoundTrip(t *testing.T) {
	for _, name := range []string{"orders-api", "web-shop", "ingest-worker"} {
		dir := filepath.Join("..", "..", "testdata", "repos", name)
		res := Detect(readRepo(t, dir), name)
		got := load(t, res.Spec)
		b, err := os.ReadFile(filepath.Join(dir, "preconfig.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		want := load(t, string(b))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the draft differs from preconfig.yaml\ndraft: %+v\nwant:  %+v\n%s", name, got, want, res.Spec)
		}
		if len(res.Notes) != 0 {
			t.Errorf("%s: unexpected notes %v", name, res.Notes)
		}
		if _, ds := spec.Load("", res.Spec); len(ds) != 0 {
			t.Errorf("%s: the draft has findings: %v", name, ds)
		}
	}
}

func TestFoundAndSources(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "repos", "web-shop")
	res := Detect(readRepo(t, dir), "web-shop")
	if strings.Join(res.Found, ",") != ".nvmrc,package.json,pnpm-lock.yaml,docker-compose.yml,.env.example" {
		t.Errorf("found = %v", res.Found)
	}
	for _, want := range []string{
		"# Drafted by preconfig detect",
		`node: "22"  # .nvmrc`,
		"- pnpm@10  # packageManager in package.json",
		"# docker-compose.yml (service postgres)",
		"- STRIPE_API_KEY  # .env.example; the name looks like a secret",
	} {
		if !strings.Contains(res.Spec, want) {
			t.Errorf("the draft has no %q:\n%s", want, res.Spec)
		}
	}
}

// draftOf runs detect on a handful of files and loads the result.
func draftOf(t *testing.T, files map[string]string) (*spec.Spec, Result) {
	t.Helper()
	res := Detect(files, "demo")
	s, ds := spec.Load("", res.Spec)
	if s == nil {
		return nil, res
	}
	_ = ds
	return s, res
}

func hasNote(res Result, part string) bool {
	for _, n := range res.Notes {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

func TestNodeVersions(t *testing.T) {
	pkg := `{"name": "demo", "scripts": {"test": "vitest run"}}`
	cases := []struct {
		files map[string]string
		want  string
		note  string
	}{
		{map[string]string{"package.json": pkg, ".nvmrc": "v20.11.0\n"}, "20", ""},
		{map[string]string{"package.json": pkg, ".nvmrc": "lts/iron"}, "20", ""},
		{map[string]string{"package.json": pkg, ".nvmrc": "lts/krypton"}, "24", ""},
		{map[string]string{"package.json": pkg, ".node-version": "22.9"}, "22", ""},
		{map[string]string{"package.json": pkg, ".tool-versions": "python 3.12.1\nnodejs 24.1.0\n"}, "24", ""},
		{map[string]string{"package.json": `{"engines": {"node": ">=20.9"}}`}, "20", ""},
		{map[string]string{"package.json": `{"engines": {"node": "^24"}}`}, "24", ""},
		{map[string]string{"package.json": pkg}, "22", "No Node.js version is pinned"},
		{map[string]string{"package.json": pkg, ".nvmrc": "18"}, "18", "Node.js 18, which this version doesn't install"},
		{map[string]string{".nvmrc": "22"}, "22", ""},
	}
	for _, c := range cases {
		res := Detect(c.files, "demo")
		if !strings.Contains(res.Spec, `node: "`+c.want+`"`) {
			t.Errorf("%v: want node %s in\n%s", c.files, c.want, res.Spec)
		}
		if c.note != "" && !hasNote(res, c.note) {
			t.Errorf("%v: want a note %q, got %v", c.files, c.note, res.Notes)
		}
		if c.note == "" && hasNote(res, "Node.js") {
			t.Errorf("%v: unexpected notes %v", c.files, res.Notes)
		}
	}
}

func TestPackageManagers(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		tools string
		setup string
		ready string
		note  string
	}{
		{"pnpm with lock", map[string]string{"package.json": `{"packageManager": "pnpm@10.2.1+sha512.abc", "scripts": {"test": "vitest"}}`, "pnpm-lock.yaml": "x"}, "pnpm@10", "pnpm install --frozen-lockfile", "pnpm test", ""},
		{"pnpm without lock", map[string]string{"package.json": `{"packageManager": "pnpm@9.15.0", "scripts": {"test": "vitest"}}`}, "pnpm@9", "pnpm install", "pnpm test", ""},
		{"pnpm lock only", map[string]string{"package.json": `{"scripts": {"test": "vitest"}}`, "pnpm-lock.yaml": "x"}, "pnpm", "pnpm install --frozen-lockfile", "pnpm test", ""},
		{"yarn 1", map[string]string{"package.json": `{"packageManager": "yarn@1.22.22", "scripts": {"test": "jest"}}`, "yarn.lock": "x"}, "yarn", "yarn install --frozen-lockfile", "yarn test", ""},
		{"yarn berry", map[string]string{"package.json": `{"scripts": {"test": "jest"}}`, "yarn.lock": "x"}, "yarn", "yarn install --immutable", "yarn test", ""},
		{"npm lock", map[string]string{"package.json": `{"scripts": {"test": "node --test"}}`, "package-lock.json": "{}"}, "", "npm ci", "npm test", ""},
		{"no lock", map[string]string{"package.json": `{"scripts": {"test": "node --test"}}`}, "", "npm install", "npm test", "no lockfile"},
		{"bun", map[string]string{"package.json": `{"scripts": {"test": "bun test"}}`, "bun.lock": "x"}, "", "npm install", "npm test", "Bun"},
		{"build only", map[string]string{"package.json": `{"scripts": {"test": "echo \"Error: no test specified\" && exit 1", "build": "tsc"}}`, "package-lock.json": "{}"}, "", "npm ci", "npm run build", "no test script"},
		{"broken package.json", map[string]string{"package.json": `{"name": `}, "", "npm install", "", "package.json can't be read"},
	}
	for _, c := range cases {
		res := Detect(c.files, "demo")
		sec := func(key string) string {
			n, err := yaml.Parse(res.Spec)
			if err != nil {
				t.Fatalf("%s: the draft is not YAML: %v", c.name, err)
			}
			return strings.Join(n.Get(key).Strings(), "; ")
		}
		if got := sec("tools"); got != c.tools {
			t.Errorf("%s: tools = %q, want %q", c.name, got, c.tools)
		}
		if got := sec("setup"); got != c.setup {
			t.Errorf("%s: setup = %q, want %q", c.name, got, c.setup)
		}
		if got := sec("ready"); got != c.ready {
			t.Errorf("%s: ready = %q, want %q", c.name, got, c.ready)
		}
		if c.note != "" && !hasNote(res, c.note) {
			t.Errorf("%s: want a note %q, got %v", c.name, c.note, res.Notes)
		}
	}
}

func TestPython(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		python   string
		setup    string
		ready    string
		tools    string
		packages string
		note     string
	}{
		{"requirements", map[string]string{"requirements.txt": "pytest\n", ".python-version": "3.13.1\n"}, "3.13", "python3 -m venv .venv; .venv/bin/pip install -r requirements.txt", ".venv/bin/pytest -q", "", "", ""},
		{"dev requirements", map[string]string{"requirements.txt": "flask\n", "requirements-dev.txt": "pytest==9\n"}, "3.12", "python3 -m venv .venv; .venv/bin/pip install -r requirements.txt; .venv/bin/pip install -r requirements-dev.txt", ".venv/bin/pytest -q", "", "", "No Python version is pinned"},
		{"requires-python", map[string]string{"pyproject.toml": "[project]\nname = \"svc\"\nrequires-python = \">=3.11\"\ndependencies = [\"pytest\"]\n"}, "3.11", "python3 -m venv .venv; .venv/bin/pip install -e .", ".venv/bin/pytest -q", "", "", ""},
		{"uv", map[string]string{"pyproject.toml": "[project]\nname = \"svc\"\n[dependency-groups]\ndev = [\"pytest>=8\"]\n", "uv.lock": "x", ".python-version": "3.12"}, "3.12", "uv sync --frozen", "uv run pytest -q", "uv", "", ""},
		{"poetry", map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"svc\"\n[tool.poetry.group.dev.dependencies]\npytest = \"^8\"\n", ".python-version": "3.12"}, "3.12", "poetry install --no-interaction", "poetry run pytest -q", "poetry", "", ""},
		{"pipenv", map[string]string{"Pipfile": "[packages]\n", ".python-version": "3.12"}, "3.12", "", "", "", "", "Pipenv"},
		{"no tests", map[string]string{"requirements.txt": "flask\n", ".python-version": "3.12"}, "3.12", "python3 -m venv .venv; .venv/bin/pip install -r requirements.txt", "", "", "", "No test runner"},
		{"psycopg2", map[string]string{"requirements.txt": "psycopg2==2.9.10\npytest\n", ".python-version": "3.12"}, "3.12", "python3 -m venv .venv; .venv/bin/pip install -r requirements.txt", ".venv/bin/pytest -q", "", "build-essential; libpq-dev", ""},
		{"psycopg2-binary needs nothing", map[string]string{"requirements.txt": "psycopg2-binary==2.9.10\npytest\n", ".python-version": "3.12"}, "3.12", "python3 -m venv .venv; .venv/bin/pip install -r requirements.txt", ".venv/bin/pytest -q", "", "", ""},
		{"old python", map[string]string{"requirements.txt": "pytest\n", ".python-version": "3.8"}, "3.8", "python3 -m venv .venv; .venv/bin/pip install -r requirements.txt", ".venv/bin/pytest -q", "", "", "Python 3.8, which this version doesn't install"},
	}
	for _, c := range cases {
		res := Detect(c.files, "demo")
		n, err := yaml.Parse(res.Spec)
		if err != nil {
			t.Fatalf("%s: not YAML: %v", c.name, err)
		}
		get := func(key string) string { return strings.Join(n.Get(key).Strings(), "; ") }
		if got := n.Path("runtimes", "python").Str(); got != c.python {
			t.Errorf("%s: python = %q, want %q", c.name, got, c.python)
		}
		for key, want := range map[string]string{"setup": c.setup, "ready": c.ready, "tools": c.tools, "packages": c.packages} {
			if got := get(key); got != want {
				t.Errorf("%s: %s = %q, want %q", c.name, key, got, want)
			}
		}
		if c.note != "" && !hasNote(res, c.note) {
			t.Errorf("%s: want a note %q, got %v", c.name, c.note, res.Notes)
		}
	}
}

func TestGo(t *testing.T) {
	cases := []struct {
		mod, goVer, name, note string
	}{
		{"module example.com/acme/ingest\n\ngo 1.25.3\n", "1.25", "ingest", ""},
		{"module ingest\n", "1.24", "ingest", ""},
		{"module github.com/acme/old\n\ngo 1.21\n", "1.21", "old", "Go 1.21, which this version doesn't install"},
	}
	for _, c := range cases {
		res := Detect(map[string]string{"go.mod": c.mod}, "folder")
		n, err := yaml.Parse(res.Spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := n.Path("runtimes", "go").Str(); got != c.goVer {
			t.Errorf("%q: go = %q", c.mod, got)
		}
		if got := n.Get("name").Str(); got != c.name {
			t.Errorf("%q: name = %q", c.mod, got)
		}
		if strings.Join(n.Get("setup").Strings(), ";") != "go mod download" || strings.Join(n.Get("ready").Strings(), ";") != "go test ./..." {
			t.Errorf("%q: commands wrong:\n%s", c.mod, res.Spec)
		}
		if c.note != "" && !hasNote(res, c.note) {
			t.Errorf("%q: want note %q, got %v", c.mod, c.note, res.Notes)
		}
	}
}

func TestServices(t *testing.T) {
	compose := `services:
  db:
    image: bitnami/postgresql:15.8
    environment:
      - POSTGRESQL_USERNAME=app
      - POSTGRESQL_PASSWORD=secret
      - POSTGRESQL_DATABASE=appdb
  db2:
    image: postgres:17
  cache:
    image: redis
  queue:
    image: rabbitmq:4-management
  search:
    image: docker.elastic.co/elasticsearch/elasticsearch:9.1.0
  build-only:
    build: .
`
	res := Detect(map[string]string{"compose.yaml": compose, "go.mod": "module x\ngo 1.25\n"}, "x")
	s := load(t, res.Spec)
	pg, ok := s.Service("postgres")
	if !ok || pg.Version != "15" || pg.User != "app" || pg.Password != "secret" || pg.Database != "appdb" {
		t.Errorf("postgres = %+v", pg)
	}
	if r, ok := s.Service("redis"); !ok || r.Version != "7" {
		t.Errorf("redis = %+v", r)
	}
	if len(s.Services) != 2 {
		t.Errorf("want two services, got %+v", s.Services)
	}
	for _, note := range []string{"Redis without a version: the draft uses 7", "runs rabbitmq", "runs elasticsearch"} {
		if !hasNote(res, note) {
			t.Errorf("want a note %q, got %v", note, res.Notes)
		}
	}
	res = Detect(map[string]string{"docker-compose.yml": "services:\n  db:\n    image: postgres\n  r:\n    image: redis:6.2\n"}, "x")
	for _, note := range []string{"PostgreSQL without a version: the draft uses 16", "Redis 6, which this version doesn't run"} {
		if !hasNote(res, note) {
			t.Errorf("want a note %q, got %v", note, res.Notes)
		}
	}
	res = Detect(map[string]string{"compose.yaml": "services: [\n"}, "x")
	if !hasNote(res, "compose.yaml can't be read") {
		t.Errorf("want a note for a broken compose file, got %v", res.Notes)
	}
	// Only the first compose file is read.
	res = Detect(map[string]string{"compose.yaml": "services:\n  a:\n    image: redis:8\n", "docker-compose.yml": "services:\n  b:\n    image: postgres:16\n"}, "x")
	if strings.Contains(res.Spec, "postgres") || !strings.Contains(res.Spec, `redis: "8"`) {
		t.Errorf("read the wrong compose file:\n%s", res.Spec)
	}
}

func TestEnv(t *testing.T) {
	example := strings.Join([]string{
		"# Copy to .env",
		"export APP_ENV=development",
		"PORT=8080 # the port",
		`GREETING="hello world"`,
		`QUOTED='a # not a comment'`,
		"EMPTY=",
		"GITHUB_TOKEN=",
		"DB_PASSWORD=changeme",
		"aws_access_key_id=",
		"not a line",
		"",
	}, "\n")
	res := Detect(map[string]string{".env.example": example, "go.mod": "module x\ngo 1.25\n"}, "x")
	s := load(t, res.Spec)
	got := map[string]string{}
	for _, e := range s.Env {
		got[e.Key] = e.Value
	}
	want := map[string]string{"APP_ENV": "development", "PORT": "8080", "GREETING": "hello world", "QUOTED": "a # not a comment", "EMPTY": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("env = %v, want %v", got, want)
	}
	if strings.Join(s.Secrets, ",") != "GITHUB_TOKEN,DB_PASSWORD,aws_access_key_id" {
		t.Errorf("secrets = %v", s.Secrets)
	}
}

func TestNames(t *testing.T) {
	cases := []struct {
		files map[string]string
		dir   string
		want  string
	}{
		{map[string]string{"package.json": `{"name": "@acme/web-shop"}`}, "x", "web-shop"},
		{map[string]string{"pyproject.toml": "[project]\nname = \"billing svc\"\n"}, "x", "billing-svc"},
		{map[string]string{}, "My App!", "My-App"},
		{map[string]string{}, "...", "project"},
		{map[string]string{}, strings.Repeat("a", 80), strings.Repeat("a", 64)},
	}
	for _, c := range cases {
		res := Detect(c.files, c.dir)
		n, err := yaml.Parse(res.Spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := n.Get("name").Str(); got != c.want {
			t.Errorf("%v in %q: name = %q, want %q", c.files, c.dir, got, c.want)
		}
	}
	res := Detect(map[string]string{}, "empty")
	if !hasNote(res, "No Node.js, Python or Go project files") {
		t.Errorf("an empty repository should say so: %v", res.Notes)
	}
}

func TestYamlStr(t *testing.T) {
	cases := []string{"", "plain", "true", "No", "on", "~", "22", "3.10", "-1", "a: b", "a:", "trailing ", " leading", "#hash", "it's", `say "hi"`, "a\nb", "tab\there", "[x]", "{x}", "&anchor", "*alias", "!tag", "%pct", "@at", "`tick`", "é", "100%", "x #y", "- dash", "?", "postgres://u:p@localhost:5432/db"}
	for _, s := range cases {
		n, err := yaml.Parse("k: " + yamlStr(s) + "\n")
		if err != nil {
			t.Errorf("%q written as %s doesn't parse: %v", s, yamlStr(s), err)
			continue
		}
		v := n.Get("k")
		if v.Type != tree.String || v.Value != s {
			t.Errorf("%q written as %s reads back as %q (type %d)", s, yamlStr(s), v.Value, v.Type)
		}
	}
	if yamlStr("plain") != "plain" || yamlStr("redis://localhost:6379/0") != "redis://localhost:6379/0" {
		t.Error("plain values should stay plain")
	}
}

// FuzzYamlStr: any valid UTF-8 text survives a round trip through yamlStr.
func FuzzYamlStr(f *testing.F) {
	for _, s := range []string{"", "a: b", "true", "3.10", "x #y", "é\u2028"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8Valid(s) {
			return
		}
		n, err := yaml.Parse("k: " + yamlStr(s) + "\n")
		if err != nil {
			t.Fatalf("%q written as %s doesn't parse: %v", s, yamlStr(s), err)
		}
		if v := n.Get("k"); v.Type != tree.String || v.Value != s {
			t.Fatalf("%q written as %s reads back as %q", s, yamlStr(s), v.Value)
		}
	})
}

// FuzzDetect: whatever the files hold, detect doesn't panic and its draft is
// always valid YAML, so a person can open it and fix it.
func FuzzDetect(f *testing.F) {
	f.Add(`{"name": "x", "packageManager": "pnpm@10", "scripts": {"test": "t"}}`, "services:\n  db:\n    image: postgres:16\n    environment: [POSTGRES_USER=a]\n", "A=1\nTOKEN=\n")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, pkg, compose, env string) {
		files := map[string]string{"package.json": pkg, "compose.yaml": compose, ".env.example": env, ".nvmrc": env, "pyproject.toml": pkg, "go.mod": env, "requirements.txt": pkg}
		res := Detect(files, compose)
		if _, err := yaml.Parse(res.Spec); err != nil {
			t.Fatalf("the draft is not YAML: %v\n%s", err, res.Spec)
		}
	})
}

func utf8Valid(s string) bool { return utf8.ValidString(s) }

func BenchmarkDetect(b *testing.B) {
	t := &testing.T{}
	files := readRepo(t, filepath.Join("..", "..", "testdata", "repos", "orders-api"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Detect(files, "orders-api")
	}
}
