// Package spec reads preconfig.yaml: the one file that says what a machine
// needs before an agent (or a person) starts work on a repository. The reader
// is strict. An unknown key, a wrong type or a version the engine can't
// install is an error with a line number, never a silent guess.
package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/tree"
	"preconfiguration.com/preconfig/internal/yaml"
)

// Severity of a diagnostic.
const (
	Error   = "error"
	Warning = "warning"
	Note    = "note"
)

// Diagnostic is one message about a file.
type Diagnostic struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

func (d Diagnostic) String() string {
	loc := d.File
	if d.Line > 0 {
		loc += ":" + strconv.Itoa(d.Line)
		if d.Col > 0 {
			loc += ":" + strconv.Itoa(d.Col)
		}
	}
	s := fmt.Sprintf("%s: %s %s: %s", loc, d.Severity, d.Code, d.Message)
	if d.Hint != "" {
		s += "\n    " + d.Hint
	}
	return s
}

// HasErrors reports whether any diagnostic is an error.
func HasErrors(ds []Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == Error {
			return true
		}
	}
	return false
}

// ToolUse is a tool with an optional version.
type ToolUse struct {
	Name    string
	Version string
}

// String gives "pnpm@10" or "uv".
func (t ToolUse) String() string {
	if t.Version == "" {
		return t.Name
	}
	return t.Name + "@" + t.Version
}

// Service is a database or cache that must be running.
type Service struct {
	Name     string // postgres or redis
	Version  string
	Major    int
	User     string
	Password string
	Database string
}

// EnvVar is one environment variable with a value that is safe to commit.
type EnvVar struct {
	Key   string
	Value string
}

// Spec is a parsed, checked preconfig.yaml.
type Spec struct {
	Version  int
	Name     string
	Base     kb.Base
	Node     string // "22", or "" when not used
	Python   string // "3.12" or "3.12.4"
	Go       string // "1.24" or "1.24.7"
	Tools    []ToolUse
	Packages []string
	Services []Service
	Env      []EnvVar
	Secrets  []string
	Setup    []string
	Ready    []string
	Targets  []string
	Repo     string
	Source   string // the text it was read from
}

// Has reports whether the spec lists a target.
func (s *Spec) Has(target string) bool {
	for _, t := range s.Targets {
		if t == target {
			return true
		}
	}
	return false
}

// Tool returns the tool with the given name, if the spec lists it.
func (s *Spec) Tool(name string) (ToolUse, bool) {
	for _, t := range s.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return ToolUse{}, false
}

// Service returns the named service, if the spec lists it.
func (s *Spec) Service(name string) (Service, bool) {
	for _, sv := range s.Services {
		if sv.Name == name {
			return sv, true
		}
	}
	return Service{}, false
}

// PythonMinor returns "3.12" for "3.12.4".
func (s *Spec) PythonMinor() string { return minorOf(s.Python) }

// GoMinor returns "1.24" for "1.24.7".
func (s *Spec) GoMinor() string { return minorOf(s.Go) }

func minorOf(v string) string {
	p := strings.Split(v, ".")
	if len(p) >= 2 {
		return p[0] + "." + p[1]
	}
	return v
}

var topKeys = []string{"version", "name", "base", "runtimes", "tools", "packages", "services", "env", "secrets", "setup", "ready", "targets", "repo"}

var (
	nameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	envKeyRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	aptRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)
	secretKey = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|PASSWD|API_?KEY|PRIVATE_?KEY|ACCESS_?KEY|CREDENTIALS?)`)
	dbWordRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
	repoRe    = regexp.MustCompile(`^(https://[^\s]+|git@[^\s:]+:[^\s]+)$`)
)

type loader struct {
	file string
	ds   []Diagnostic
	s    *Spec
}

func (l *loader) add(sev, code string, n *tree.Node, msg, hint string) {
	d := Diagnostic{File: l.file, Severity: sev, Code: code, Message: msg, Hint: hint}
	if n != nil {
		d.Line, d.Col = n.Line, n.Col
	}
	l.ds = append(l.ds, d)
}

func (l *loader) errf(code string, n *tree.Node, hint, format string, args ...any) {
	l.add(Error, code, n, fmt.Sprintf(format, args...), hint)
}

func (l *loader) warnf(code string, n *tree.Node, hint, format string, args ...any) {
	l.add(Warning, code, n, fmt.Sprintf(format, args...), hint)
}

// Load reads and checks preconfig.yaml. It returns the spec (nil when the file
// can't be used) and every diagnostic it found, errors first by line.
func Load(file, src string) (*Spec, []Diagnostic) {
	if file == "" {
		file = kb.SpecPath
	}
	l := &loader{file: file, s: &Spec{Source: src}}
	root, err := yaml.Parse(src)
	if err != nil {
		d := Diagnostic{File: file, Severity: Error, Code: "P001", Message: err.Error()}
		if ye, ok := err.(*yaml.Error); ok {
			d.Line, d.Col, d.Message = ye.Line, ye.Col, ye.Msg
		}
		return nil, []Diagnostic{d}
	}
	l.load(root)
	sortDiagnostics(l.ds)
	if HasErrors(l.ds) {
		return nil, l.ds
	}
	return l.s, l.ds
}

func sortDiagnostics(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].Line != ds[j].Line {
			return ds[i].Line < ds[j].Line
		}
		return ds[i].Col < ds[j].Col
	})
}

func (l *loader) load(root *tree.Node) {
	s := l.s
	if root.Kind != tree.Map {
		if root.IsNull() {
			l.errf("S001", root, "Start with `version: 1` and list what the machine needs.", "preconfig.yaml is empty")
			return
		}
		l.errf("S001", root, "", "preconfig.yaml must be a mapping of keys such as version, runtimes and setup, not %s", root.Describe())
		return
	}
	for _, k := range root.Keys {
		if !contains(topKeys, k.Value) {
			hint := ""
			if sug := tree.Suggest(k.Value, topKeys); sug != "" {
				hint = fmt.Sprintf("Did you mean %q?", sug)
			} else {
				hint = "Known keys: " + strings.Join(topKeys, ", ") + "."
			}
			l.errf("S002", k, hint, "unknown key %q", k.Value)
		}
	}

	// version
	v := root.Get("version")
	switch {
	case v == nil:
		l.errf("S003", root, "Add `version: 1` as the first line.", "the version key is missing")
	case v.Kind != tree.Scalar || v.Value != "1":
		l.errf("S003", v, "This engine reads version 1 of the format.", "unsupported version %q", v.Value)
	default:
		s.Version = 1
	}

	s.Name = "project"
	if n := root.Get("name"); n != nil {
		if str, ok := l.scalar(n, "name"); ok {
			if !nameRe.MatchString(str) {
				l.errf("S010", n, "Use letters, digits, dots, dashes or underscores, up to 64 characters.", "the name %q can't be used for folders and containers", str)
			} else {
				s.Name = str
			}
		}
	}

	baseName := kb.DefaultBase
	if n := root.Get("base"); n != nil {
		if str, ok := l.scalar(n, "base"); ok {
			baseName = str
		}
	}
	if b, ok := kb.Bases[baseName]; ok {
		s.Base = b
	} else {
		n := root.Get("base")
		l.errf("S011", n, "This version knows ubuntu-24.04.", "unknown base %q", baseName)
		s.Base = kb.Bases[kb.DefaultBase]
	}

	l.runtimes(root.Get("runtimes"))
	l.tools(root.Get("tools"))
	l.packages(root.Get("packages"))
	l.services(root.Get("services"))
	l.env(root.Get("env"))
	l.secrets(root.Get("secrets"))
	s.Setup = l.commands(root.Get("setup"), "setup")
	readyNode := root.Get("ready")
	s.Ready = l.commands(readyNode, "ready")
	if len(s.Ready) == 0 {
		at := readyNode
		if at == nil {
			at = root
		}
		l.warnf("S081", at, "Add a command that passes only on a working machine, such as `npm test` or `pytest -q`.", "there is no ready check, so verify can't prove the setup works")
	}
	l.targets(root.Get("targets"))
	if n := root.Get("repo"); n != nil {
		if str, ok := l.scalar(n, "repo"); ok {
			if _, bad := badChar(str); bad || !repoRe.MatchString(str) {
				l.errf("S100", n, "Use an https:// URL or git@host:owner/repo.", "%q is not a repository URL", str)
			} else {
				s.Repo = str
			}
		}
	}
	l.crossChecks(root)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// scalar returns the text of a scalar node, reporting anything else.
func (l *loader) scalar(n *tree.Node, what string) (string, bool) {
	if n.Kind != tree.Scalar || n.Type == tree.Null {
		l.errf("S004", n, "", "%s should be a single value, not %s", what, n.Describe())
		return "", false
	}
	return n.Value, true
}

func (l *loader) runtimes(n *tree.Node) {
	if n == nil {
		return
	}
	if n.Kind != tree.Map {
		l.errf("S004", n, "For example:\n    runtimes:\n      node: \"22\"", "runtimes should be a mapping of runtime names to versions, not %s", n.Describe())
		return
	}
	known := []string{"node", "python", "go"}
	for i, k := range n.Keys {
		val := n.Values[i]
		if !contains(known, k.Value) {
			hint := "This version installs node, python and go. Java, Ruby and Rust are planned."
			if sug := tree.Suggest(k.Value, append(known, "nodejs", "golang")); sug != "" && contains(known, sug) {
				hint = fmt.Sprintf("Did you mean %q?", sug)
			}
			if k.Value == "nodejs" {
				hint = "Did you mean \"node\"?"
			}
			if k.Value == "golang" {
				hint = "Did you mean \"go\"?"
			}
			l.errf("S020", k, hint, "unknown runtime %q", k.Value)
			continue
		}
		ver, ok := l.scalar(val, "the "+k.Value+" version")
		if !ok {
			continue
		}
		switch k.Value {
		case "node":
			parts, err := kb.ParseVersion(ver)
			if err != nil || len(parts) != 1 {
				l.errf("S021", val, "Give a major version such as \"22\"; each platform picks the latest release of it.", "node version %q should be a major version", ver)
				continue
			}
			if !kb.Contains(kb.NodeMajors, parts[0]) {
				l.errf("S021", val, "This version installs Node "+kb.Join(kb.NodeMajors, "")+".", "node %s is not supported", ver)
				continue
			}
			l.s.Node = ver
		case "python":
			parts, err := kb.ParseVersion(ver)
			if err != nil || len(parts) < 2 || parts[0] != 3 {
				l.errf("S022", val, "Give a version such as \"3.12\" (in quotes).", "python version %q should look like 3.12", ver)
				continue
			}
			if !kb.Contains(kb.PythonMinors, parts[1]) {
				l.errf("S022", val, "This version installs Python "+kb.Join(kb.PythonMinors, "3.")+".", "python %s is not supported", ver)
				continue
			}
			l.s.Python = ver
		case "go":
			parts, err := kb.ParseVersion(ver)
			if err != nil || len(parts) < 2 || parts[0] != 1 {
				l.errf("S023", val, "Give a version such as \"1.24\" (in quotes).", "go version %q should look like 1.24", ver)
				continue
			}
			if !kb.Contains(kb.GoMinors, parts[1]) {
				l.errf("S023", val, "This version installs Go "+kb.Join(kb.GoMinors, "1.")+".", "go %s is not supported", ver)
				continue
			}
			l.s.Go = ver
		}
	}
}

func (l *loader) list(n *tree.Node, what string) []*tree.Node {
	if n == nil || n.IsNull() {
		return nil
	}
	if n.Kind != tree.Seq {
		l.errf("S004", n, "Write each entry on its own line starting with \"- \".", "%s should be a list, not %s", what, n.Describe())
		return nil
	}
	return n.Items
}

var toolVersionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)

func (l *loader) tools(n *tree.Node) {
	seen := map[string]bool{}
	for _, it := range l.list(n, "tools") {
		str, ok := l.scalar(it, "a tool")
		if !ok {
			continue
		}
		name, ver, _ := strings.Cut(str, "@")
		if _, known := kb.Tools[name]; !known {
			hint := "This version knows " + strings.Join(kb.ToolNames, ", ") + "."
			if sug := tree.Suggest(name, kb.ToolNames); sug != "" {
				hint = fmt.Sprintf("Did you mean %q?", sug)
			}
			l.errf("S030", it, hint, "unknown tool %q", name)
			continue
		}
		if strings.Contains(str, "@") && !toolVersionRe.MatchString(ver) {
			l.errf("S030", it, "Write the version as digits, such as pnpm@10 or uv@0.12.20.", "the version in %q can't be read", str)
			continue
		}
		if seen[name] {
			l.errf("S030", it, "", "the tool %q is listed twice", name)
			continue
		}
		seen[name] = true
		l.s.Tools = append(l.s.Tools, ToolUse{Name: name, Version: ver})
	}
}

func (l *loader) packages(n *tree.Node) {
	seen := map[string]bool{}
	for _, it := range l.list(n, "packages") {
		str, ok := l.scalar(it, "a package")
		if !ok {
			continue
		}
		if !aptRe.MatchString(str) {
			l.errf("S040", it, "Give the Ubuntu package name, such as libpq-dev.", "%q is not an Ubuntu package name", str)
			continue
		}
		if seen[str] {
			l.warnf("S040", it, "", "the package %q is listed twice", str)
			continue
		}
		seen[str] = true
		l.s.Packages = append(l.s.Packages, str)
	}
}

func (l *loader) services(n *tree.Node) {
	if n == nil || n.IsNull() {
		return
	}
	if n.Kind != tree.Map {
		l.errf("S004", n, "For example:\n    services:\n      postgres: \"16\"", "services should be a mapping of service names to versions, not %s", n.Describe())
		return
	}
	known := []string{"postgres", "redis"}
	for i, k := range n.Keys {
		val := n.Values[i]
		if !contains(known, k.Value) {
			hint := "This version runs postgres and redis. MySQL is planned."
			if k.Value == "postgresql" || k.Value == "pg" {
				hint = "Did you mean \"postgres\"?"
			}
			l.errf("S050", k, hint, "unknown service %q", k.Value)
			continue
		}
		svc := Service{Name: k.Value}
		verNode := val
		if val.Kind == tree.Map {
			allowed := []string{"version"}
			if k.Value == "postgres" {
				allowed = append(allowed, "user", "password", "database")
			}
			for j, sk := range val.Keys {
				if !contains(allowed, sk.Value) {
					l.errf("S050", sk, "Keys for "+k.Value+": "+strings.Join(allowed, ", ")+".", "unknown key %q for %s", sk.Value, k.Value)
					continue
				}
				sv, ok := l.scalar(val.Values[j], k.Value+" "+sk.Value)
				if !ok {
					continue
				}
				switch sk.Value {
				case "user":
					svc.User = sv
				case "password":
					svc.Password = sv
				case "database":
					svc.Database = sv
				}
				if (sk.Value == "user" || sk.Value == "database") && !dbWordRe.MatchString(sv) {
					l.errf("S050", val.Values[j], "Use letters, digits and underscores.", "%q can't be used as a PostgreSQL %s", sv, sk.Value)
				}
				if sk.Value == "password" {
					if r, bad := badChar(sv); bad {
						l.errf("S050", val.Values[j], "Keep the local test password to letters, digits and simple punctuation.", "the postgres password holds the control character %U, which the setup files can't carry", r)
					} else if strings.ContainsAny(sv, "'\"\\$` ") {
						l.errf("S050", val.Values[j], "Keep the local test password to letters, digits and simple punctuation.", "the postgres password contains quotes, spaces or shell characters")
					}
				}
			}
			verNode = val.Get("version")
			if verNode == nil {
				l.errf("S050", val, "Add version: \"16\" (or the major version you use).", "%s needs a version", k.Value)
				continue
			}
		}
		ver, ok := l.scalar(verNode, k.Value+" version")
		if !ok {
			continue
		}
		parts, err := kb.ParseVersion(ver)
		if err != nil || len(parts) != 1 {
			l.errf("S051", verNode, "Give a major version such as \"16\".", "%s version %q should be a major version", k.Value, ver)
			continue
		}
		svc.Version, svc.Major = ver, parts[0]
		switch k.Value {
		case "postgres":
			if !kb.Contains(kb.PostgresMajors, parts[0]) {
				l.errf("S051", verNode, "This version runs PostgreSQL "+kb.Join(kb.PostgresMajors, "")+".", "postgres %s is not supported", ver)
				continue
			}
			if svc.User == "" {
				svc.User = "postgres"
			}
			if svc.Password == "" {
				svc.Password = "postgres"
			}
			if svc.Database == "" {
				svc.Database = svc.User
			}
		case "redis":
			if !kb.Contains(kb.RedisMajors, parts[0]) {
				l.errf("S051", verNode, "This version runs Redis "+kb.Join(kb.RedisMajors, "")+" on Ubuntu 24.04.", "redis %s is not supported", ver)
				continue
			}
		}
		l.s.Services = append(l.s.Services, svc)
	}
}

func (l *loader) env(n *tree.Node) {
	if n == nil || n.IsNull() {
		return
	}
	if n.Kind != tree.Map {
		l.errf("S004", n, "For example:\n    env:\n      NODE_ENV: test", "env should be a mapping of names to values, not %s", n.Describe())
		return
	}
	for i, k := range n.Keys {
		val := n.Values[i]
		if !envKeyRe.MatchString(k.Value) {
			l.errf("S060", k, "Use letters, digits and underscores, not starting with a digit.", "%q is not a valid environment variable name", k.Value)
			continue
		}
		if val.Kind != tree.Scalar {
			l.errf("S060", val, "", "the value of %s should be a single value, not %s", k.Value, val.Describe())
			continue
		}
		v := val.Value
		if val.Type == tree.Null {
			v = ""
		}
		if strings.Contains(v, "\n") {
			l.errf("S060", val, "", "the value of %s spans several lines", k.Value)
			continue
		}
		if r, bad := badChar(v); bad {
			l.errf("S060", val, "", "the value of %s holds the control character %U, which the setup files can't carry", k.Value, r)
			continue
		}
		if secretKey.MatchString(k.Value) && v != "" {
			l.errf("S061", k, "List "+k.Value+" under secrets and set its value in each platform's secret store. preconfig.yaml is committed with the code.", "%s looks like a secret, and its value is written in the file", k.Value)
			continue
		}
		l.s.Env = append(l.s.Env, EnvVar{Key: k.Value, Value: v})
	}
}

func (l *loader) secrets(n *tree.Node) {
	seen := map[string]bool{}
	for _, it := range l.list(n, "secrets") {
		str, ok := l.scalar(it, "a secret name")
		if !ok {
			continue
		}
		if !envKeyRe.MatchString(str) {
			l.errf("S070", it, "List names only, such as NPM_TOKEN. Values never go in this file.", "%q is not a valid secret name", str)
			continue
		}
		if seen[str] {
			l.warnf("S070", it, "", "the secret %q is listed twice", str)
			continue
		}
		seen[str] = true
		l.s.Secrets = append(l.s.Secrets, str)
	}
}

func (l *loader) commands(n *tree.Node, what string) []string {
	var out []string
	if n != nil && n.Kind == tree.Scalar && n.Type != tree.Null {
		l.errf("S080", n, "Write it as a list:\n    "+what+":\n      - "+n.Value, "%s should be a list of commands", what)
		return nil
	}
	for _, it := range l.list(n, what) {
		str, ok := l.scalar(it, "a command")
		if !ok {
			continue
		}
		str = strings.TrimSpace(str)
		if str == "" {
			l.errf("S080", it, "", "an empty command in %s", what)
			continue
		}
		if strings.Contains(str, "\n") {
			l.errf("S080", it, "Put each command in its own list item.", "a command in %s spans several lines", what)
			continue
		}
		if r, bad := badChar(str); bad {
			l.errf("S080", it, "", "a command in %s holds the control character %U, which the setup files can't carry", what, r)
			continue
		}
		out = append(out, str)
	}
	return out
}

func (l *loader) targets(n *tree.Node) {
	if n == nil {
		l.s.Targets = append([]string(nil), kb.Targets...)
		return
	}
	seen := map[string]bool{}
	var picked []string
	for _, it := range l.list(n, "targets") {
		str, ok := l.scalar(it, "a target")
		if !ok {
			continue
		}
		if !contains(kb.Targets, str) {
			hint := "Targets: " + strings.Join(kb.Targets, ", ") + "."
			if sug := tree.Suggest(str, kb.Targets); sug != "" {
				hint = fmt.Sprintf("Did you mean %q?", sug)
			}
			l.errf("S090", it, hint, "unknown target %q", str)
			continue
		}
		if seen[str] {
			l.warnf("S090", it, "", "the target %q is listed twice", str)
			continue
		}
		seen[str] = true
		picked = append(picked, str)
	}
	if len(picked) == 0 && !HasErrors(l.ds) {
		l.errf("S090", n, "Leave targets out to write all of them, or list at least one.", "no targets to write")
	}
	// The Cursor Dockerfile and cloud-init run the setup script.
	if seen["cursor"] && !seen["script"] {
		l.add(Note, "S091", n, "the cursor target runs .preconfig/setup.sh, so the script target is written too", "")
		picked = append(picked, "script")
	}
	// Keep the canonical order.
	var ordered []string
	for _, t := range kb.Targets {
		if contains(picked, t) {
			ordered = append(ordered, t)
		}
	}
	l.s.Targets = ordered
}

// commandNeeds maps the first word of a command to the runtime or tool it needs.
var commandNeeds = map[string]string{
	"npm": "node", "npx": "node", "node": "node", "corepack": "node",
	"pnpm": "pnpm", "yarn": "yarn",
	"pip": "python", "pip3": "python", "python": "python", "python3": "python", "pytest": "python",
	"uv": "uv", "poetry": "poetry",
	"go": "go", "gofmt": "go",
}

func (l *loader) crossChecks(root *tree.Node) {
	s := l.s
	// Tools need their runtime.
	for _, t := range s.Tools {
		need := kb.Tools[t.Name].Runtime
		if (need == "node" && s.Node == "") || (need == "python" && s.Python == "") {
			l.errf("S031", root.Get("tools"), "Add "+need+" under runtimes.", "the tool %s needs the %s runtime", t.Name, need)
		}
	}
	// Commands should only use what the spec installs.
	check := func(cmds []string, node *tree.Node, what string) {
		for _, c := range cmds {
			for _, part := range splitCommands(c) {
				f := strings.Fields(part)
				if len(f) == 0 {
					continue
				}
				word := f[0]
				for strings.Contains(word, "=") && len(f) > 1 { // FOO=bar cmd
					f = f[1:]
					word = f[0]
				}
				if strings.HasPrefix(word, ".venv/bin/") || strings.HasPrefix(word, "venv/bin/") {
					if s.Python == "" {
						l.warnf("S083", node, "Add python under runtimes.", "%s uses a virtual environment, but no python runtime is listed", what)
					}
					continue
				}
				if strings.HasPrefix(word, "./") {
					continue
				}
				need, ok := commandNeeds[word]
				if !ok {
					continue
				}
				switch need {
				case "node":
					if s.Node == "" {
						l.warnf("S083", node, "Add node under runtimes.", "%s runs %s, but no node runtime is listed", what, word)
					}
				case "python":
					if s.Python == "" {
						l.warnf("S083", node, "Add python under runtimes.", "%s runs %s, but no python runtime is listed", what, word)
					}
				case "go":
					if s.Go == "" {
						l.warnf("S083", node, "Add go under runtimes.", "%s runs %s, but no go runtime is listed", what, word)
					}
				default:
					if _, ok := s.Tool(need); !ok {
						l.warnf("S082", node, "Add "+need+" under tools so every target installs it.", "%s runs %s, but tools doesn't list it", what, word)
					}
				}
			}
		}
	}
	check(s.Setup, root.Get("setup"), "setup")
	check(s.Ready, root.Get("ready"), "ready")
	// A secret listed in env too.
	for _, e := range s.Env {
		for _, sec := range s.Secrets {
			if e.Key == sec {
				l.errf("S071", root.Get("env"), "Keep it under secrets only.", "%s is listed both in env and in secrets", sec)
			}
		}
	}
	// An environment variable that points at a local service the spec
	// doesn't run.
	for _, e := range s.Env {
		v := strings.ToLower(e.Value)
		local := strings.Contains(v, "localhost") || strings.Contains(v, "127.0.0.1")
		if !local {
			continue
		}
		if _, ok := s.Service("redis"); !ok && (strings.HasPrefix(v, "redis://") || strings.HasPrefix(v, "rediss://") || strings.Contains(v, ":6379")) {
			l.warnf("S062", root.Path("env", e.Key), "Add redis under services, or point "+e.Key+" somewhere else.", "%s points at Redis on this machine, but services doesn't list redis", e.Key)
		}
		if _, ok := s.Service("postgres"); !ok && (strings.HasPrefix(v, "postgres://") || strings.HasPrefix(v, "postgresql://") || strings.Contains(v, ":5432")) {
			l.warnf("S062", root.Path("env", e.Key), "Add postgres under services, or point "+e.Key+" somewhere else.", "%s points at PostgreSQL on this machine, but services doesn't list postgres", e.Key)
		}
	}
	if s.Python != "" && s.PythonMinor() != s.Base.DistroPython {
		l.add(Note, "S024", root.Path("runtimes", "python"),
			fmt.Sprintf("Ubuntu 24.04 ships Python %s; the shell targets install %s with uv", s.Base.DistroPython, s.Python), "")
	}
}

// splitCommands splits "a && b; c | d" into its simple commands.
// badChar finds a character that no setup file can carry safely: a control
// character other than tab, or a Unicode line or paragraph separator, which
// some YAML readers take for a line break.
func badChar(s string) (rune, bool) {
	for _, r := range s {
		if r == '\t' {
			continue
		}
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == '\ufeff' || r == '\ufffe' || r == '\uffff' {
			return r, true
		}
	}
	return 0, false
}

func splitCommands(c string) []string {
	r := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	return strings.Split(r.Replace(c), "\n")
}
