package check

import (
	"fmt"
	"regexp"
	"strings"

	"preconfiguration.com/preconfig/internal/gen"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/textdiff"
	"preconfiguration.com/preconfig/internal/tree"
	"preconfiguration.com/preconfig/internal/yaml"
)

// yamlParse reads a file that someone else wrote, forgiving duplicate keys.
func yamlParse(text string) (*tree.Node, error) {
	return yaml.ParseWithOptions(text, yaml.Options{AllowDuplicateKeys: true})
}

// Fact is one version a setup file installs, and where it says so: a line of
// the file itself, or of the file it hands the work to (File).
type Fact struct {
	Version string
	Line    int
	File    string // set when the version is read from another file
}

// Facts are the versions a setup file installs, read back from the file.
type Facts struct {
	File     string
	Node     Fact
	Python   Fact
	Go       Fact
	Postgres Fact
	Redis    Fact
}

var things = []string{"node", "python", "go", "postgres", "redis"}

func (f *Facts) get(thing string) *Fact {
	switch thing {
	case "node":
		return &f.Node
	case "python":
		return &f.Python
	case "go":
		return &f.Go
	case "postgres":
		return &f.Postgres
	}
	return &f.Redis
}

func label(thing string) string {
	switch thing {
	case "node":
		return "Node.js"
	case "python":
		return "Python"
	case "go":
		return "Go"
	case "postgres":
		return "PostgreSQL"
	}
	return "Redis"
}

// through marks facts as read from another file, such as the Dockerfile or
// the setup script that a Cursor environment runs.
func through(f Facts, file string) Facts {
	for _, t := range things {
		if fa := f.get(t); fa.Version != "" && fa.File == "" {
			fa.File = file
		}
	}
	return f
}

// shift moves the line numbers of facts read from text that starts after
// line offset of the file, such as a script inside cloud-init.yaml.
func shift(f Facts, offset int) Facts {
	for _, t := range things {
		if fa := f.get(t); fa.Version != "" && fa.Line > 0 {
			fa.Line += offset
		}
	}
	return f
}

// where says where a fact was read, for a finding about file: the line, or
// nothing when the version was read from another file.
func where(file string, fa *Fact) *tree.Node {
	if fa.File != "" && fa.File != file {
		return nil
	}
	return &tree.Node{Line: fa.Line}
}

// via names the other file a version was read from, for a message.
func via(file string, fa *Fact) string {
	if fa.File == "" || fa.File == file {
		return ""
	}
	if fa.Line > 0 {
		return fmt.Sprintf(" (set in %s, line %d)", fa.File, fa.Line)
	}
	return fmt.Sprintf(" (set in %s)", fa.File)
}

func mergeFacts(a, b Facts) Facts {
	for _, t := range things {
		if a.get(t).Version == "" {
			*a.get(t) = *b.get(t)
		}
	}
	return a
}

func str(n *tree.Node) string {
	if n == nil || n.Kind != tree.Scalar {
		return ""
	}
	return n.Value
}

func copilotFacts(p string, job *tree.Node) Facts {
	f := Facts{File: p}
	for _, st := range job.Get("steps").List() {
		uses := str(st.Get("uses"))
		with := st.Get("with")
		switch {
		case strings.HasPrefix(uses, "actions/setup-node@"):
			if v := with.Get("node-version"); v != nil {
				f.Node = Fact{Version: str(v), Line: v.Line}
			}
		case strings.HasPrefix(uses, "actions/setup-python@"):
			if v := with.Get("python-version"); v != nil {
				f.Python = Fact{Version: str(v), Line: v.Line}
			}
		case strings.HasPrefix(uses, "actions/setup-go@"):
			if v := with.Get("go-version"); v != nil {
				f.Go = Fact{Version: str(v), Line: v.Line}
			}
		}
	}
	svcs := job.Get("services")
	if svcs != nil && svcs.Kind == tree.Map {
		for _, v := range svcs.Values {
			img := v.Get("image")
			if img == nil && v.Kind == tree.Scalar {
				img = v
			}
			imageFact(&f, str(img), lineOf(img))
		}
	}
	return f
}

func lineOf(n *tree.Node) int {
	if n == nil {
		return 0
	}
	return n.Line
}

var imageTag = regexp.MustCompile(`^(?:[^/]+/)*([a-z0-9._-]+)(?::([A-Za-z0-9._-]+))?$`)

// imageFact records a service or runtime from a container image name.
func imageFact(f *Facts, image string, line int) {
	m := imageTag.FindStringSubmatch(image)
	if m == nil {
		return
	}
	name, tag := m[1], m[2]
	major := leadingVersion(tag, 1)
	switch name {
	case "postgres", "postgresql":
		if f.Postgres.Version == "" {
			f.Postgres = Fact{Version: orUnknown(major), Line: line}
		}
	case "redis", "redis-stack-server", "valkey":
		if f.Redis.Version == "" && name != "valkey" {
			f.Redis = Fact{Version: orUnknown(major), Line: line}
		}
	case "node":
		if f.Node.Version == "" {
			f.Node = Fact{Version: orUnknown(major), Line: line}
		}
	case "python":
		if f.Python.Version == "" {
			f.Python = Fact{Version: orUnknown(leadingVersion(tag, 2)), Line: line}
		}
	case "golang":
		if f.Go.Version == "" {
			f.Go = Fact{Version: orUnknown(leadingVersion(tag, 2)), Line: line}
		}
	}
}

func orUnknown(v string) string {
	if v == "" {
		return "latest"
	}
	return v
}

var versionPrefix = regexp.MustCompile(`^v?([0-9]+(?:\.[0-9]+)*)`)

// leadingVersion takes the first parts of a version at the start of s:
// leadingVersion("16.4-alpine", 1) is "16".
func leadingVersion(s string, parts int) string {
	m := versionPrefix.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	ps := strings.Split(m[1], ".")
	if len(ps) > parts {
		ps = ps[:parts]
	}
	return strings.Join(ps, ".")
}

// devcontainerImage matches the runtime images of devcontainers/images, such
// as mcr.microsoft.com/devcontainers/python:1-3.12-bookworm.
var devcontainerImage = regexp.MustCompile(`devcontainers/(python|javascript-node|typescript-node|go):(?:[0-9]+-)?([0-9]+(?:\.[0-9]+)?)`)

func devcontainerFacts(p string, root *tree.Node) Facts {
	f := Facts{File: p}
	if img := root.Get("image"); img != nil {
		if m := devcontainerImage.FindStringSubmatch(str(img)); m != nil {
			switch m[1] {
			case "python":
				f.Python = Fact{Version: m[2], Line: img.Line}
			case "go":
				f.Go = Fact{Version: m[2], Line: img.Line}
			default:
				f.Node = Fact{Version: leadingVersion(m[2], 1), Line: img.Line}
			}
		} else {
			imageFact(&f, str(img), img.Line)
		}
	}
	feats := root.Get("features")
	if feats != nil && feats.Kind == tree.Map {
		for i, k := range feats.Keys {
			opts := feats.Values[i]
			ver := opts.Get("version")
			v := str(ver)
			line := k.Line
			if ver != nil {
				line = ver.Line
			}
			switch {
			case strings.Contains(k.Value, "/features/node:"):
				f.Node = Fact{Version: orUnknown(v), Line: line}
			case strings.Contains(k.Value, "/features/python:"):
				f.Python = Fact{Version: orUnknown(v), Line: line}
			case strings.Contains(k.Value, "/features/go:"):
				f.Go = Fact{Version: orUnknown(v), Line: line}
			}
		}
	}
	return f
}

func composeFacts(p, text string) Facts {
	f := Facts{File: p}
	root, err := yamlParse(text)
	if err != nil {
		return f
	}
	svcs := root.Get("services")
	if svcs == nil || svcs.Kind != tree.Map {
		return f
	}
	for _, v := range svcs.Values {
		img := v.Get("image")
		imageFact(&f, str(img), lineOf(img))
	}
	return f
}

var (
	stepMarker   = regexp.MustCompile(`step "machine [0-9]+/[0-9]+: (node|python|go|postgres|redis) ([0-9.]+)"`)
	nodesource   = regexp.MustCompile(`(?:setup_|node_)([0-9]+)\.x`)
	aptPostgres  = regexp.MustCompile(`postgresql-([0-9]+)\b`)
	aptPython    = regexp.MustCompile(`\bpython(3\.[0-9]+)\b`)
	goToolchain  = regexp.MustCompile(`GOTOOLCHAIN=go([0-9]+\.[0-9]+)`)
	fromImage    = regexp.MustCompile(`(?m)^FROM\s+(?:--platform=\S+\s+)?(\S+)`)
	uvPython     = regexp.MustCompile(`uv python install\s+(?:--\S+\s+)*([0-9]+\.[0-9]+)`)
	generatedTag = "Generated by preconfig"
)

// scriptFacts reads the versions a shell script installs. It knows the step
// markers of preconfig's own script and a few common install lines.
func scriptFacts(p, text string) Facts {
	f := Facts{File: p}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		n := i + 1
		if m := stepMarker.FindStringSubmatch(l); m != nil {
			fa := f.get(m[1])
			if fa.Version == "" {
				*fa = Fact{Version: m[2], Line: n}
			}
			continue
		}
		if strings.Contains(text, generatedTag) {
			continue // preconfig's own script says it all in its markers
		}
		if m := nodesource.FindStringSubmatch(l); m != nil && f.Node.Version == "" {
			f.Node = Fact{Version: m[1], Line: n}
		}
		if m := aptPostgres.FindStringSubmatch(l); m != nil && f.Postgres.Version == "" {
			f.Postgres = Fact{Version: m[1], Line: n}
		}
		if m := uvPython.FindStringSubmatch(l); m != nil && f.Python.Version == "" {
			f.Python = Fact{Version: m[1], Line: n}
		}
		if m := aptPython.FindStringSubmatch(l); m != nil && f.Python.Version == "" {
			f.Python = Fact{Version: m[1], Line: n}
		}
		if m := goToolchain.FindStringSubmatch(l); m != nil && f.Go.Version == "" {
			f.Go = Fact{Version: m[1], Line: n}
		}
	}
	return f
}

func dockerfileFacts(p, text string) Facts {
	f := scriptFacts(p, text)
	for _, m := range fromImage.FindAllStringSubmatchIndex(text, -1) {
		img := text[m[2]:m[3]]
		line := strings.Count(text[:m[0]], "\n") + 1
		imageFact(&f, img, line)
	}
	return f
}

// ---- comparisons ----

func specValue(s *spec.Spec, thing string) string {
	switch thing {
	case "node":
		return s.Node
	case "python":
		return s.Python
	case "go":
		return s.Go
	case "postgres":
		if sv, ok := s.Service("postgres"); ok {
			return sv.Version
		}
	case "redis":
		if sv, ok := s.Service("redis"); ok {
			return sv.Version
		}
	}
	return ""
}

// sameVersion compares at the precision the spec gives: "22" matches "22.11.0",
// "3.12" matches "3.12.4", but "3.12" doesn't match "3.11" or "latest".
func sameVersion(want, got string) bool {
	if want == got {
		return true
	}
	if got == "" || got == "latest" || got == "lts" {
		return false
	}
	w := strings.Split(want, ".")
	g := strings.Split(strings.TrimPrefix(strings.TrimSuffix(strings.TrimSuffix(got, ".x"), ".*"), "v"), ".")
	if len(g) < len(w) {
		return false
	}
	for i := range w {
		if w[i] != g[i] {
			return false
		}
	}
	return true
}

func (c *checker) compareWithSpec(s *spec.Spec, facts []Facts) {
	for _, f := range facts {
		if f.File == "" {
			continue
		}
		for _, t := range things {
			want := specValue(s, t)
			got := f.get(t)
			switch {
			case want != "" && got.Version == "":
				// A file that only carries part of the setup (the Cursor
				// environment without its Dockerfile) can't be judged.
				if !c.judgeable(f) {
					continue
				}
				c.add(f.File, nil, spec.Error, "X004", "Run preconfig build to rewrite it from preconfig.yaml.",
					"%s doesn't install %s; preconfig.yaml asks for %s %s", base(f.File), label(t), label(t), want)
			case want != "" && !sameVersion(want, got.Version):
				c.add(f.File, where(f.File, got), spec.Error, "X003", "Run preconfig build to rewrite it from preconfig.yaml.",
					"%s installs %s %s%s; preconfig.yaml asks for %s", base(f.File), label(t), got.Version, via(f.File, got), want)
			case want == "" && got.Version != "":
				c.add(f.File, where(f.File, got), spec.Warning, "X005", "Add it to preconfig.yaml, or run preconfig build to remove it here.",
					"%s installs %s %s%s, which preconfig.yaml doesn't list", base(f.File), label(t), got.Version, via(f.File, got))
			}
		}
	}
}

// judgeable reports whether facts were read from a complete setup, so that a
// missing runtime means the file really doesn't install it.
func (c *checker) judgeable(f Facts) bool {
	if f.File == kb.CursorEnvPath {
		return f.Node.Version+f.Python.Version+f.Go.Version+f.Postgres.Version+f.Redis.Version != ""
	}
	return true
}

func base(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func (c *checker) crossCheck(facts []Facts) {
	for _, t := range things {
		type seen struct {
			file string
			fact Fact
		}
		var all []seen
		distinct := map[string]bool{}
		for _, f := range facts {
			if v := f.get(t); v.Version != "" {
				all = append(all, seen{f.File, *v})
				distinct[normalize(t, v.Version)] = true
			}
		}
		if len(distinct) < 2 {
			continue
		}
		var parts []string
		for _, s := range all {
			parts = append(parts, fmt.Sprintf("%s %s", base(s.file), s.fact.Version))
		}
		first := all[0]
		c.add(first.file, where(first.file, &first.fact), spec.Warning, "X010",
			"Pick one version and write it in preconfig.yaml; preconfig build then writes it into every file.",
			"the setup files disagree on %s: %s", label(t), strings.Join(parts, ", "))
	}
}

func normalize(thing, v string) string {
	switch thing {
	case "python", "go":
		return leadingVersion(v, 2)
	}
	return leadingVersion(v, 1)
}

// drift compares every generated file with the file in the repository.
func (c *checker) drift(s *spec.Spec) {
	res := gen.Build(s)
	for _, f := range res.Files {
		actual, ok := c.in.Files[f.Path]
		if !ok {
			if c.in.Exists != nil && c.in.Exists(f.Path) {
				continue
			}
			c.add(f.Path, nil, spec.Error, "X001", "Run preconfig build to write it.",
				"%s is missing: preconfig.yaml lists the %s target", f.Path, f.Target)
			continue
		}
		if actual == f.Content {
			continue
		}
		added, removed := textdiff.Stat(actual, f.Content)
		line := firstDifference(actual, f.Content)
		what := "was edited by hand or written from an older preconfig.yaml"
		marker := generatedTag
		if !strings.Contains(f.Content, generatedTag) {
			// A format with no room for a comment, such as environment.json:
			// the file preconfig writes always runs its setup script.
			marker = kb.ScriptPath
		}
		if !strings.Contains(actual, marker) {
			what = "was not written by preconfig"
		}
		c.add(f.Path, &tree.Node{Line: line}, spec.Error, "X002", "Run preconfig build to rewrite it, or move the change into preconfig.yaml.",
			"%s differs from what preconfig.yaml produces (%s to add, %d to remove): it %s", base(f.Path), lines(added), removed, what)
		c.rep.Diffs = append(c.rep.Diffs, FileDiff{
			Path: f.Path, Added: added, Removed: removed,
			Diff: textdiff.Unified("a/"+f.Path, "b/"+f.Path+" (from preconfig.yaml)", actual, f.Content, 2),
		})
	}
}

func lines(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

func firstDifference(a, b string) int {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return i + 1
		}
	}
	return min(len(al), len(bl)) + 1
}
