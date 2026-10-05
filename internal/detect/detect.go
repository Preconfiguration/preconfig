// Package detect drafts a preconfig.yaml from the files a repository already
// has: version files, package manifests, lockfiles, compose files and example
// environment files. It reads files and nothing else, so the draft is a
// starting point for a person to check, not a finding.
package detect

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"preconfiguration.com/preconfig/internal/jsonc"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/tree"
	"preconfiguration.com/preconfig/internal/yaml"
)

// Paths are the files detect reads when they exist.
var Paths = []string{
	".nvmrc", ".node-version", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock",
	".python-version", "pyproject.toml", "requirements.txt", "requirements-dev.txt", "uv.lock", "poetry.lock", "Pipfile",
	"go.mod",
	"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml",
	".env.example", ".env.sample", ".env.template", ".env.dist",
	".tool-versions",
}

// Result is a drafted spec.
type Result struct {
	Spec  string   `json:"spec"`  // the text of preconfig.yaml
	Notes []string `json:"notes"` // what detect couldn't tell, for the person to decide
	Found []string `json:"found"` // the files it read
}

type item struct {
	value  string
	source string
}

type service struct {
	name, version, user, password, database, source string
}

type draft struct {
	files    map[string]string
	name     item
	node     item
	python   item
	goVer    item
	tools    []item
	packages []item
	services []service
	env      []item // "KEY=value"
	secrets  []item
	setup    []item
	ready    []item
	notes    []string
}

// Detect drafts a spec. dirName is the repository folder's name, used when no
// manifest names the project.
func Detect(files map[string]string, dirName string) Result {
	d := &draft{files: files}
	var found []string
	for _, p := range Paths {
		if _, ok := files[p]; ok {
			found = append(found, p)
		}
	}
	d.detectNode()
	d.detectPython()
	d.detectGo()
	d.detectServices()
	d.detectEnv()
	if d.name.value == "" {
		d.name = item{safeName(dirName), "the folder name"}
	}
	if d.name.value == "" {
		d.name = item{"project", ""}
	}
	if d.node.value == "" && d.python.value == "" && d.goVer.value == "" {
		d.notes = append(d.notes, "No Node.js, Python or Go project files were found, so the draft has no runtimes.")
	}
	if d.notes == nil {
		d.notes = []string{}
	}
	if found == nil {
		found = []string{}
	}
	return Result{Spec: d.render(), Notes: d.notes, Found: found}
}

var nameClean = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeName(s string) string {
	s = nameClean.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-._")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// ---- Node.js ----

var ltsNames = map[string]string{"hydrogen": "18", "iron": "20", "jod": "22", "krypton": "24"}

func (d *draft) detectNode() {
	pkgText, hasPkg := d.files["package.json"]
	var pkg *tree.Node
	if hasPkg {
		n, err := jsonc.Parse(pkgText, jsonc.Options{})
		if err != nil {
			d.notes = append(d.notes, "package.json can't be read: "+err.Error())
		} else {
			pkg = n
		}
	}
	for _, f := range []string{".nvmrc", ".node-version"} {
		if t, ok := d.files[f]; ok && d.node.value == "" {
			v := strings.TrimSpace(strings.Split(t, "\n")[0])
			v = strings.TrimPrefix(strings.ToLower(v), "v")
			if strings.HasPrefix(v, "lts/") {
				if m, ok := ltsNames[strings.TrimPrefix(v, "lts/")]; ok {
					v = m
				}
			}
			if major := leadingInt(v); major != "" {
				d.node = item{major, f}
			}
		}
	}
	if t, ok := d.files[".tool-versions"]; ok && d.node.value == "" {
		for _, l := range strings.Split(t, "\n") {
			f := strings.Fields(l)
			if len(f) >= 2 && f[0] == "nodejs" {
				if major := leadingInt(f[1]); major != "" {
					d.node = item{major, ".tool-versions"}
				}
			}
		}
	}
	if pkg != nil {
		if n := pkg.Get("name").Str(); n != "" && d.name.value == "" {
			d.name = item{safeName(strings.TrimPrefix(n[strings.LastIndex(n, "/")+1:], "@")), "package.json"}
		}
		if eng := pkg.Path("engines", "node").Str(); eng != "" && d.node.value == "" {
			if major := leadingInt(strings.TrimLeft(eng, "^~>=<v ")); major != "" {
				d.node = item{major, "package.json engines"}
			}
		}
	}
	if !hasPkg && d.node.value == "" {
		return
	}
	if d.node.value == "" {
		d.node = item{"22", "no version file; 22 is the newest LTS line this version supports"}
		d.notes = append(d.notes, "No Node.js version is pinned (.nvmrc, .node-version or engines.node): the draft uses 22.")
	} else if !supportedInt(d.node.value, kb.NodeMajors) {
		d.notes = append(d.notes, fmt.Sprintf("The repository asks for Node.js %s, which this version doesn't install (it supports %s).", d.node.value, kb.Join(kb.NodeMajors, "")))
	}
	if !hasPkg {
		return
	}
	pm, pmVer := "", ""
	if pkg != nil {
		if s := pkg.Get("packageManager").Str(); s != "" {
			pm, pmVer, _ = strings.Cut(s, "@")
			pmVer = leadingInt(pmVer)
			pmVer = strings.Split(pmVer, "+")[0]
		}
	}
	_, pnpmLock := d.files["pnpm-lock.yaml"]
	_, yarnLock := d.files["yarn.lock"]
	_, npmLock := d.files["package-lock.json"]
	_, bun1 := d.files["bun.lockb"]
	_, bun2 := d.files["bun.lock"]
	run := "npm"
	switch {
	case pm == "pnpm" || pnpmLock:
		run = "pnpm"
		tool := "pnpm"
		if pmVer != "" {
			tool += "@" + pmVer
		}
		d.tools = append(d.tools, item{tool, sourceOf(pm == "pnpm", "packageManager in package.json", "pnpm-lock.yaml")})
		if pnpmLock {
			d.setup = append(d.setup, item{"pnpm install --frozen-lockfile", "pnpm-lock.yaml"})
		} else {
			d.setup = append(d.setup, item{"pnpm install", "packageManager in package.json"})
		}
	case pm == "yarn" || yarnLock:
		run = "yarn"
		d.tools = append(d.tools, item{"yarn", sourceOf(pm == "yarn", "packageManager in package.json", "yarn.lock")})
		if pmVer == "1" {
			d.setup = append(d.setup, item{"yarn install --frozen-lockfile", "yarn.lock (Yarn 1)"})
		} else {
			d.setup = append(d.setup, item{"yarn install --immutable", "yarn.lock"})
		}
	case bun1 || bun2:
		d.notes = append(d.notes, "The repository uses Bun, which this version doesn't install. The draft uses npm.")
		d.setup = append(d.setup, item{"npm install", "package.json"})
	case npmLock:
		d.setup = append(d.setup, item{"npm ci", "package-lock.json"})
	default:
		d.setup = append(d.setup, item{"npm install", "package.json (no lockfile)"})
		d.notes = append(d.notes, "package.json has no lockfile, so installs aren't repeatable. Commit package-lock.json and use npm ci.")
	}
	if pkg != nil {
		test := pkg.Path("scripts", "test").Str()
		if test != "" && !strings.Contains(test, "no test specified") {
			cmd := run + " test"
			if run == "npm" {
				cmd = "npm test"
			}
			d.ready = append(d.ready, item{cmd, "scripts.test in package.json"})
		} else if build := pkg.Path("scripts", "build").Str(); build != "" {
			d.ready = append(d.ready, item{run + " run build", "scripts.build in package.json"})
			d.notes = append(d.notes, "package.json has no test script, so the ready check runs the build.")
		}
	}
}

func sourceOf(first bool, a, b string) string {
	if first {
		return a
	}
	return b
}

var leadingIntRe = regexp.MustCompile(`^([0-9]+)`)

func leadingInt(s string) string {
	m := leadingIntRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return ""
	}
	return m[1]
}

func supportedInt(v string, list []int) bool {
	var n int
	fmt.Sscan(v, &n)
	return kb.Contains(list, n)
}

// ---- Python ----

var (
	requiresPython = regexp.MustCompile(`(?m)^\s*requires-python\s*=\s*["']([^"']+)["']`)
	pyprojectName  = regexp.MustCompile(`(?m)^\s*name\s*=\s*["']([^"']+)["']`)
	minorVersion   = regexp.MustCompile(`([0-9]+)\.([0-9]+)`)
)

func (d *draft) detectPython() {
	pyproject, hasPyproject := d.files["pyproject.toml"]
	req, hasReq := d.files["requirements.txt"]
	_, hasUVLock := d.files["uv.lock"]
	_, hasPoetryLock := d.files["poetry.lock"]
	_, hasPipfile := d.files["Pipfile"]
	pv, hasPV := d.files[".python-version"]
	if !hasPyproject && !hasReq && !hasUVLock && !hasPoetryLock && !hasPV && !hasPipfile {
		return
	}
	if hasPV {
		v := strings.TrimSpace(strings.Split(pv, "\n")[0])
		if m := minorVersion.FindStringSubmatch(v); m != nil {
			d.python = item{m[1] + "." + m[2], ".python-version"}
		}
	}
	if hasPyproject {
		if m := requiresPython.FindStringSubmatch(pyproject); m != nil && d.python.value == "" {
			if mm := minorVersion.FindStringSubmatch(m[1]); mm != nil {
				d.python = item{mm[1] + "." + mm[2], "requires-python in pyproject.toml"}
			}
		}
		if m := pyprojectName.FindStringSubmatch(pyproject); m != nil && d.name.value == "" {
			d.name = item{safeName(m[1]), "pyproject.toml"}
		}
	}
	if d.python.value == "" {
		d.python = item{"3.12", "no version file; 3.12 is what Ubuntu 24.04 ships"}
		d.notes = append(d.notes, "No Python version is pinned (.python-version or requires-python): the draft uses 3.12.")
	} else {
		var minor int
		fmt.Sscanf(d.python.value, "3.%d", &minor)
		if !kb.Contains(kb.PythonMinors, minor) {
			d.notes = append(d.notes, fmt.Sprintf("The repository asks for Python %s, which this version doesn't install.", d.python.value))
		}
	}
	allReqs := req + "\n" + d.files["requirements-dev.txt"] + "\n" + pyproject
	hasPytest := strings.Contains(strings.ToLower(allReqs), "pytest")
	switch {
	case hasUVLock:
		d.tools = append(d.tools, item{"uv", "uv.lock"})
		d.setup = append(d.setup, item{"uv sync --frozen", "uv.lock"})
		if hasPytest {
			d.ready = append(d.ready, item{"uv run pytest -q", "pytest in pyproject.toml"})
		}
	case hasPoetryLock || strings.Contains(pyproject, "[tool.poetry]"):
		d.tools = append(d.tools, item{"poetry", sourceOf(hasPoetryLock, "poetry.lock", "[tool.poetry] in pyproject.toml")})
		d.setup = append(d.setup, item{"poetry install --no-interaction", "poetry.lock"})
		if hasPytest {
			d.ready = append(d.ready, item{"poetry run pytest -q", "pytest in pyproject.toml"})
		}
	case hasReq:
		d.setup = append(d.setup, item{"python3 -m venv .venv", "requirements.txt"})
		d.setup = append(d.setup, item{".venv/bin/pip install -r requirements.txt", "requirements.txt"})
		if _, ok := d.files["requirements-dev.txt"]; ok {
			d.setup = append(d.setup, item{".venv/bin/pip install -r requirements-dev.txt", "requirements-dev.txt"})
		}
		if hasPytest {
			d.ready = append(d.ready, item{".venv/bin/pytest -q", "pytest in the requirements"})
		}
	case hasPyproject:
		d.setup = append(d.setup, item{"python3 -m venv .venv", "pyproject.toml"})
		d.setup = append(d.setup, item{".venv/bin/pip install -e .", "pyproject.toml"})
		if hasPytest {
			d.ready = append(d.ready, item{".venv/bin/pytest -q", "pytest in pyproject.toml"})
		}
	case hasPipfile:
		d.notes = append(d.notes, "The repository uses Pipenv, which this version doesn't install. Add its install command to setup yourself.")
	}
	if !hasPytest {
		d.notes = append(d.notes, "No test runner was found for Python, so there's no ready check. Add one, such as pytest -q.")
	}
	lower := strings.ToLower(allReqs)
	if regexp.MustCompile(`(?m)^psycopg2\s*([=<>~!].*)?$`).MatchString(lower) {
		d.packages = append(d.packages, item{"libpq-dev", "psycopg2 builds against libpq (requirements.txt)"}, item{"build-essential", "psycopg2 builds from source"})
	}
}

// ---- Go ----

var (
	goDirective = regexp.MustCompile(`(?m)^go\s+([0-9]+\.[0-9]+)`)
	goModule    = regexp.MustCompile(`(?m)^module\s+(\S+)`)
)

func (d *draft) detectGo() {
	mod, ok := d.files["go.mod"]
	if !ok {
		return
	}
	if m := goDirective.FindStringSubmatch(mod); m != nil {
		d.goVer = item{m[1], "go directive in go.mod"}
		var minor int
		fmt.Sscanf(m[1], "1.%d", &minor)
		if !kb.Contains(kb.GoMinors, minor) {
			d.notes = append(d.notes, fmt.Sprintf("go.mod asks for Go %s, which this version doesn't install (it supports %s).", m[1], kb.Join(kb.GoMinors, "1.")))
		}
	} else {
		d.goVer = item{"1.24", "go.mod has no go directive"}
	}
	if m := goModule.FindStringSubmatch(mod); m != nil && d.name.value == "" {
		d.name = item{safeName(m[1][strings.LastIndex(m[1], "/")+1:]), "go.mod"}
	}
	d.setup = append(d.setup, item{"go mod download", "go.mod"})
	d.ready = append(d.ready, item{"go test ./...", "go.mod"})
}

// ---- services ----

var imageRe = regexp.MustCompile(`^(?:[^/]+/)*([a-z0-9._-]+)(?::([A-Za-z0-9._-]+))?$`)

func (d *draft) detectServices() {
	for _, p := range []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"} {
		t, ok := d.files[p]
		if !ok {
			continue
		}
		root, err := yaml.ParseWithOptions(t, yaml.Options{AllowDuplicateKeys: true})
		if err != nil {
			d.notes = append(d.notes, p+" can't be read: "+err.Error())
			continue
		}
		svcs := root.Get("services")
		if svcs == nil || svcs.Kind != tree.Map {
			continue
		}
		for i, k := range svcs.Keys {
			v := svcs.Values[i]
			img := v.Get("image").Str()
			m := imageRe.FindStringSubmatch(img)
			if m == nil {
				continue
			}
			name, tag := m[1], m[2]
			major := leadingInt(tag)
			src := fmt.Sprintf("%s (service %s)", p, k.Value)
			env := composeEnv(v.Get("environment"))
			switch name {
			case "postgres", "postgresql":
				if major == "" {
					major = "16"
					d.notes = append(d.notes, fmt.Sprintf("%s uses PostgreSQL without a version: the draft uses 16.", src))
				} else if !supportedInt(major, kb.PostgresMajors) {
					d.notes = append(d.notes, fmt.Sprintf("%s uses PostgreSQL %s, which this version doesn't run.", src, major))
				}
				sv := service{name: "postgres", version: major, source: src}
				sv.user = firstNonEmpty(env["POSTGRES_USER"], env["POSTGRESQL_USERNAME"])
				sv.password = firstNonEmpty(env["POSTGRES_PASSWORD"], env["POSTGRESQL_PASSWORD"])
				sv.database = firstNonEmpty(env["POSTGRES_DB"], env["POSTGRESQL_DATABASE"])
				d.addService(sv)
			case "redis", "redis-stack-server":
				if major == "" {
					major = "7"
					d.notes = append(d.notes, fmt.Sprintf("%s uses Redis without a version: the draft uses 7.", src))
				} else if !supportedInt(major, kb.RedisMajors) {
					d.notes = append(d.notes, fmt.Sprintf("%s uses Redis %s, which this version doesn't run on Ubuntu 24.04.", src, major))
				}
				d.addService(service{name: "redis", version: major, source: src})
			case "mysql", "mariadb", "mongo", "mongodb", "rabbitmq", "elasticsearch", "opensearch", "memcached", "valkey":
				d.notes = append(d.notes, fmt.Sprintf("%s runs %s, which this version can't set up yet.", src, name))
			}
		}
		break
	}
}

func (d *draft) addService(sv service) {
	for _, s := range d.services {
		if s.name == sv.name {
			return
		}
	}
	d.services = append(d.services, sv)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// composeEnv reads a compose environment block, as a map or a list of KEY=value.
func composeEnv(n *tree.Node) map[string]string {
	out := map[string]string{}
	if n == nil {
		return out
	}
	switch n.Kind {
	case tree.Map:
		for i, k := range n.Keys {
			out[k.Value] = n.Values[i].Str()
		}
	case tree.Seq:
		for _, it := range n.Items {
			k, v, _ := strings.Cut(it.Str(), "=")
			out[k] = v
		}
	}
	return out
}

// ---- environment ----

var (
	envLine   = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
	secretKey = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|PASSWD|API_?KEY|PRIVATE_?KEY|ACCESS_?KEY|CREDENTIALS?)`)
)

func (d *draft) detectEnv() {
	for _, p := range []string{".env.example", ".env.sample", ".env.template", ".env.dist"} {
		t, ok := d.files[p]
		if !ok {
			continue
		}
		for _, l := range strings.Split(t, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			m := envLine.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			key, val := m[1], strings.TrimSpace(m[2])
			if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			} else if i := strings.Index(val, " #"); i >= 0 {
				val = strings.TrimSpace(val[:i])
			}
			if secretKey.MatchString(key) {
				d.secrets = append(d.secrets, item{key, p + "; the name looks like a secret"})
				continue
			}
			d.env = append(d.env, item{key + "=" + val, p})
		}
		break
	}
}

// ---- output ----

func (d *draft) render() string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	type line struct{ text, source string }
	section := func(lines []line) {
		width := 0
		for _, l := range lines {
			if l.source != "" && len(l.text) > width {
				width = len(l.text)
			}
		}
		for _, l := range lines {
			if l.source == "" {
				w("%s\n", l.text)
				continue
			}
			w("%-*s  # %s\n", width, l.text, l.source)
		}
	}
	w("# Drafted by preconfig detect from the files in this repository.\n")
	w("# Read it before you use it: detect guesses from files and runs nothing.\n")
	w("version: 1\n")
	if d.name.source != "" {
		w("name: %s  # %s\n", yamlStr(d.name.value), d.name.source)
	} else {
		w("name: %s\n", yamlStr(d.name.value))
	}
	if d.node.value != "" || d.python.value != "" || d.goVer.value != "" {
		w("\n")
		ls := []line{{"runtimes:", ""}}
		if d.node.value != "" {
			ls = append(ls, line{fmt.Sprintf("  node: %q", d.node.value), d.node.source})
		}
		if d.python.value != "" {
			ls = append(ls, line{fmt.Sprintf("  python: %q", d.python.value), d.python.source})
		}
		if d.goVer.value != "" {
			ls = append(ls, line{fmt.Sprintf("  go: %q", d.goVer.value), d.goVer.source})
		}
		section(ls)
	}
	list := func(key string, items []item) {
		if len(items) == 0 {
			return
		}
		w("\n")
		ls := []line{{key + ":", ""}}
		for _, it := range items {
			ls = append(ls, line{"  - " + yamlStr(it.value), it.source})
		}
		section(ls)
	}
	list("tools", d.tools)
	list("packages", dedupe(d.packages))
	if len(d.services) > 0 {
		w("\n")
		ls := []line{{"services:", ""}}
		for _, sv := range d.services {
			if sv.name == "postgres" && (sv.user != "" || sv.password != "" || sv.database != "") {
				ls = append(ls, line{"  postgres:", sv.source})
				ls = append(ls, line{fmt.Sprintf("    version: %q", sv.version), ""})
				if sv.user != "" {
					ls = append(ls, line{"    user: " + yamlStr(sv.user), ""})
				}
				if sv.password != "" {
					ls = append(ls, line{"    password: " + yamlStr(sv.password), ""})
				}
				if sv.database != "" {
					ls = append(ls, line{"    database: " + yamlStr(sv.database), ""})
				}
				continue
			}
			ls = append(ls, line{fmt.Sprintf("  %s: %q", sv.name, sv.version), sv.source})
		}
		section(ls)
	}
	if len(d.env) > 0 {
		w("\n")
		ls := []line{{"env:", ""}}
		for _, e := range d.env {
			k, v, _ := strings.Cut(e.value, "=")
			ls = append(ls, line{"  " + k + ": " + yamlStr(v), e.source})
		}
		section(ls)
	}
	list("secrets", d.secrets)
	list("setup", d.setup)
	list("ready", d.ready)
	if len(d.notes) > 0 {
		w("\n# Couldn't tell:\n")
		for _, n := range d.notes {
			w("#   %s\n", n)
		}
	}
	return b.String()
}

func dedupe(items []item) []item {
	seen := map[string]bool{}
	var out []item
	for _, it := range items {
		if !seen[it.value] {
			seen[it.value] = true
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].value < out[j].value })
	return out
}

// yamlStr writes s so that YAML readers, 1.1 and 1.2 alike, give it back as
// the same string.
func yamlStr(s string) string { return tree.YAMLString(s) }
