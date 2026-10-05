// Package check reads the setup files already in a repository and reports
// what is wrong with them: format errors that make a platform skip or reject
// a file, files that disagree with each other, and files that have drifted
// from preconfig.yaml.
package check

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"preconfiguration.com/preconfig/internal/jsonc"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
	"preconfiguration.com/preconfig/internal/yaml"
)

// Input is what check looks at.
type Input struct {
	// Files maps repository-relative paths to contents. It holds every file
	// the caller could read among KnownPaths and Referenced.
	Files map[string]string
	// Exists reports whether a path exists in the repository. When nil,
	// only the paths in Files count as existing.
	Exists func(path string) bool
}

// FileDiff is a generated file that differs from what is in the repository.
type FileDiff struct {
	Path    string `json:"path"`
	Diff    string `json:"diff"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// Report is the result of a check.
type Report struct {
	Findings []spec.Diagnostic `json:"findings"`
	Diffs    []FileDiff        `json:"diffs"`
	Checked  []string          `json:"checked"`
	Errors   int               `json:"errors"`
	Warnings int               `json:"warnings"`
	HasSpec  bool              `json:"hasSpec"`
}

// KnownPaths are the files check reads when they exist.
var KnownPaths = []string{
	kb.SpecPath,
	kb.CopilotWorkflowPath,
	".github/copilot-setup-steps.yml",
	".github/copilot-setup-steps.yaml",
	".github/workflows/copilot-setup-steps.yaml",
	kb.CursorEnvPath,
	kb.CursorDocker,
	kb.DevcontainerPath,
	".devcontainer.json",
	kb.ComposePath,
	kb.CloudInitPath,
	kb.ScriptPath,
}

// Referenced returns further files that the known files point at, such as the
// Dockerfile named in .cursor/environment.json or a compose file named in
// devcontainer.json, so that the caller can read them too.
func Referenced(files map[string]string) []string {
	var out []string
	if t, ok := files[kb.CursorEnvPath]; ok {
		if root, err := jsonc.Parse(t, jsonc.Options{Comments: true, TrailingCommas: true}); err == nil {
			if df := root.Path("build", "dockerfile").Str(); df != "" {
				out = append(out, cursorPath(df))
			}
		}
	}
	for _, p := range []string{kb.DevcontainerPath, ".devcontainer.json"} {
		t, ok := files[p]
		if !ok {
			continue
		}
		root, err := jsonc.Parse(t, jsonc.JSONC)
		if err != nil {
			continue
		}
		dir := path.Dir(p)
		for _, c := range root.Get("dockerComposeFile").Strings() {
			out = append(out, path.Clean(path.Join(dir, c)))
		}
	}
	return out
}

// cursorPath resolves a path from environment.json: relative to .cursor, with
// ".", "./" and ".." meaning the repository root.
func cursorPath(p string) string {
	switch p {
	case ".", "./", "..":
		return "."
	}
	return path.Clean(path.Join(".cursor", p))
}

type checker struct {
	in  Input
	rep *Report
}

func (c *checker) add(file string, n *tree.Node, sev, code, hint, format string, args ...any) {
	d := spec.Diagnostic{File: file, Severity: sev, Code: code, Message: fmt.Sprintf(format, args...), Hint: hint}
	if n != nil {
		d.Line, d.Col = n.Line, n.Col
	}
	c.rep.Findings = append(c.rep.Findings, d)
}

func (c *checker) exists(p string) bool {
	if _, ok := c.in.Files[p]; ok {
		return true
	}
	if c.in.Exists != nil {
		return c.in.Exists(p)
	}
	return false
}

// Check runs every check that applies to the files it was given.
func Check(in Input) Report {
	rep := &Report{}
	c := &checker{in: in, rep: rep}
	var facts []Facts

	for _, p := range sortedPaths(in.Files) {
		text := in.Files[p]
		switch {
		case p == kb.SpecPath:
			continue
		case p == kb.CopilotWorkflowPath:
			rep.Checked = append(rep.Checked, p)
			if f, ok := c.copilot(p, text); ok {
				facts = append(facts, f)
			}
		case contains(kb.CopilotWrongPaths, p):
			rep.Checked = append(rep.Checked, p)
			c.add(p, nil, spec.Error, "C010", "Move it to "+kb.CopilotWorkflowPath+".",
				"Copilot never reads this file: it looks only at %s", kb.CopilotWorkflowPath)
		case p == kb.CursorEnvPath:
			rep.Checked = append(rep.Checked, p)
			if f, ok := c.cursor(p, text); ok {
				facts = append(facts, f)
			}
		case p == kb.DevcontainerPath || p == ".devcontainer.json":
			rep.Checked = append(rep.Checked, p)
			if f, ok := c.devcontainer(p, text); ok {
				facts = append(facts, f)
			}
		case p == kb.CloudInitPath:
			rep.Checked = append(rep.Checked, p)
			if f, ok := c.cloudInit(p, text); ok {
				facts = append(facts, f)
			}
		case p == kb.ScriptPath:
			rep.Checked = append(rep.Checked, p)
			facts = append(facts, scriptFacts(p, text))
		}
	}

	var s *spec.Spec
	if src, ok := in.Files[kb.SpecPath]; ok {
		rep.HasSpec = true
		rep.Checked = append([]string{kb.SpecPath}, rep.Checked...)
		var ds []spec.Diagnostic
		s, ds = spec.Load(kb.SpecPath, src)
		rep.Findings = append(rep.Findings, ds...)
	}
	if s != nil {
		c.compareWithSpec(s, facts)
		c.drift(s)
	} else {
		c.crossCheck(facts)
	}
	finish(rep)
	if rep.Findings == nil {
		rep.Findings = []spec.Diagnostic{}
	}
	if rep.Diffs == nil {
		rep.Diffs = []FileDiff{}
	}
	if rep.Checked == nil {
		rep.Checked = []string{}
	}
	return *rep
}

func finish(rep *Report) {
	// One finding per place and message.
	seen := map[string]bool{}
	var out []spec.Diagnostic
	for _, d := range rep.Findings {
		k := fmt.Sprintf("%s|%d|%s|%s", d.File, d.Line, d.Code, d.Message)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, d)
	}
	rank := map[string]int{spec.Error: 0, spec.Warning: 1, spec.Note: 2}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return fileOrder(out[i].File) < fileOrder(out[j].File)
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return rank[out[i].Severity] < rank[out[j].Severity]
	})
	rep.Findings = out
	rep.Errors, rep.Warnings = 0, 0
	for _, d := range out {
		switch d.Severity {
		case spec.Error:
			rep.Errors++
		case spec.Warning:
			rep.Warnings++
		}
	}
}

func fileOrder(p string) string {
	if p == kb.SpecPath {
		return "0" + p
	}
	return "1" + p
}

func sortedPaths(m map[string]string) []string {
	var ps []string
	for p := range m {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- Copilot ----

var ghRunnerArm = regexp.MustCompile(`(?i)(^macos|arm|aarch64)`)

func (c *checker) copilot(p, text string) (Facts, bool) {
	root, err := yaml.Parse(text)
	if err != nil {
		var n *tree.Node
		if ye, ok := err.(*yaml.Error); ok {
			n = &tree.Node{Line: ye.Line, Col: ye.Col}
			c.add(p, n, spec.Error, "C001", "GitHub can't run a workflow it can't read, so none of these setup steps run.", "the workflow is not valid YAML: %s", ye.Msg)
		} else {
			c.add(p, nil, spec.Error, "C001", "", "the workflow is not valid YAML: %v", err)
		}
		return Facts{}, false
	}
	if root.Kind != tree.Map {
		c.add(p, root, spec.Error, "C001", "", "the workflow should be a mapping, not %s", root.Describe())
		return Facts{}, false
	}
	jobs := root.Get("jobs")
	if jobs == nil || jobs.Kind != tree.Map {
		c.add(p, root, spec.Error, "C002", "Add a job named copilot-setup-steps under jobs.", "the workflow has no jobs, so Copilot stops with an error instead of starting work")
		return Facts{File: p}, false
	}
	job := jobs.Get(kb.CopilotJobName)
	if job == nil {
		var names []string
		for _, k := range jobs.Keys {
			names = append(names, k.Value)
		}
		hint := "Copilot runs only a job named exactly copilot-setup-steps."
		if len(names) == 1 {
			hint = fmt.Sprintf("Rename the job %q to copilot-setup-steps.", names[0])
		}
		at := jobs
		if len(jobs.Keys) > 0 {
			at = jobs.Keys[0]
		}
		c.add(p, at, spec.Error, "C002", hint, "no job is named copilot-setup-steps (found: %s), so Copilot stops with an error instead of starting work", strings.Join(names, ", "))
		if len(jobs.Keys) != 1 {
			c.triggers(p, root)
			return Facts{File: p}, false
		}
		// Check the one job as the setup job it was meant to be, so that
		// every problem shows at once.
		job = jobs.Values[0]
	} else if len(jobs.Keys) > 1 {
		var others []string
		for _, k := range jobs.Keys {
			if k.Value != kb.CopilotJobName {
				others = append(others, k.Value)
			}
		}
		c.add(p, jobs.Key(others[0]), spec.Warning, "C003", "Keep only the copilot-setup-steps job in this file.", "Copilot runs only the copilot-setup-steps job; %s run only when the workflow runs on its own", strings.Join(others, ", "))
	}
	if job.Kind != tree.Map {
		c.add(p, job, spec.Error, "C008", "", "the copilot-setup-steps job should be a mapping, not %s", job.Describe())
		return Facts{File: p}, false
	}
	for _, k := range job.Keys {
		if contains(kb.CopilotJobKeys, k.Value) || k.Value == "name" {
			continue
		}
		hint := "Copilot honors only " + strings.Join(kb.CopilotJobKeys, ", ") + "."
		switch k.Value {
		case "env":
			hint = "Set variables inside a step, or add them to the repository's copilot environment."
		case "container":
			hint = "Copilot runs the steps on the runner itself. Install what the container provided in a step."
		}
		c.add(p, k, spec.Warning, "C004", hint, "Copilot ignores the job setting %q", k.Value)
	}
	if t := job.Get("timeout-minutes"); t != nil {
		if t.Type == tree.Number {
			var v float64
			fmt.Sscan(t.Value, &v)
			if v > float64(kb.CopilotMaxTimeout) {
				c.add(p, t, spec.Error, "C005", fmt.Sprintf("Use %d or less.", kb.CopilotMaxTimeout), "timeout-minutes is %s; Copilot allows at most %d", t.Value, kb.CopilotMaxTimeout)
			}
		}
	}
	if r := job.Get("runs-on"); r != nil {
		for _, label := range r.Strings() {
			if ghRunnerArm.MatchString(label) {
				c.add(p, r, spec.Error, "C006", "Use an Ubuntu x64 runner such as ubuntu-24.04, or a Windows x64 runner.", "Copilot runs only on Ubuntu x64 and Windows 64-bit runners, not %q", label)
			}
		}
	}
	steps := job.Get("steps")
	if steps == nil || steps.Kind != tree.Seq || len(steps.Items) == 0 {
		at := steps
		if at == nil {
			at = jobs.Key(kb.CopilotJobName)
		}
		c.add(p, at, spec.Error, "C008", "Add the steps that install what the project needs.", "the copilot-setup-steps job has no steps")
	} else {
		for _, st := range steps.Items {
			if strings.HasPrefix(st.Get("uses").Str(), "actions/checkout@") {
				if fd := st.Path("with", "fetch-depth"); fd != nil {
					c.add(p, fd, spec.Note, "C009", "", "Copilot overrides fetch-depth so that it can roll back commits")
				}
			}
		}
	}
	c.triggers(p, root)
	f := copilotFacts(p, job)
	return f, true
}

// triggers warns when the workflow never runs on its own, so a broken setup
// shows up only when Copilot starts a session.
func (c *checker) triggers(p string, root *tree.Node) {
	on := root.Get("on")
	onKey := root.Key("on")
	if on == nil {
		c.add(p, root, spec.Warning, "C007", "Add workflow_dispatch, and push and pull_request filtered on this file's path.", "the workflow has no triggers, so it never runs on its own and a broken setup shows up only when Copilot starts")
		return
	}
	events := map[string]*tree.Node{}
	switch on.Kind {
	case tree.Scalar:
		events[on.Value] = nil
	case tree.Seq:
		for _, e := range on.Items {
			events[e.Str()] = nil
		}
	case tree.Map:
		for i, k := range on.Keys {
			events[k.Value] = on.Values[i]
		}
	}
	validated := false
	for _, ev := range []string{"push", "pull_request"} {
		cfg, ok := events[ev]
		if !ok {
			continue
		}
		paths := cfg.Get("paths")
		if paths == nil {
			validated = true
			continue
		}
		for _, pth := range paths.Strings() {
			if pth == p || strings.Contains(pth, "copilot-setup-steps") || pth == ".github/**" || pth == ".github/workflows/**" || pth == "**" {
				validated = true
			}
		}
	}
	if !validated {
		at := onKey
		if at == nil {
			at = on
		}
		c.add(p, at, spec.Warning, "C007", "Add push and pull_request triggers filtered on "+p+", so every change to it is run and checked.", "changes to this file don't run it, so a broken setup shows up only when Copilot starts a session")
	}
}

// ---- Cursor ----

func (c *checker) cursor(p, text string) (Facts, bool) {
	root, err := jsonc.Parse(text, jsonc.Options{Comments: true, TrailingCommas: false})
	if err != nil {
		je := err.(*jsonc.Error)
		c.add(p, &tree.Node{Line: je.Line, Col: je.Col}, spec.Error, "K001", "Cursor's schema allows comments but not trailing commas.", "environment.json can't be read: %s", je.Msg)
		return Facts{}, false
	}
	if root.Kind != tree.Map {
		c.add(p, root, spec.Error, "K001", "", "environment.json should be an object, not %s", root.Describe())
		return Facts{}, false
	}
	for i, k := range root.Keys {
		v := root.Values[i]
		if !contains(kb.CursorKeys, k.Value) {
			hint := ""
			switch k.Value {
			case "update":
				hint = "Cursor calls this command \"install\" in environment.json."
			case "dockerfile":
				hint = "Put it inside \"build\": {\"dockerfile\": \"Dockerfile\"}."
			default:
				if sug := tree.Suggest(k.Value, kb.CursorKeys); sug != "" {
					hint = fmt.Sprintf("Did you mean %q?", sug)
				}
			}
			c.add(p, k, spec.Error, "K002", hint, "unknown key %q: Cursor's schema rejects keys it doesn't define", k.Value)
			continue
		}
		switch k.Value {
		case "name", "user", "install", "start", "image", "snapshot", "chromeExecutablePath":
			if v.Kind != tree.Scalar || v.Type != tree.String {
				c.add(p, v, spec.Error, "K005", "", "%s should be a string, not %s", k.Value, v.Describe())
			}
		case "terminals", "ports", "repositoryDependencies", "egressAllowlist", "mcpServerAllowlist":
			if v.Kind != tree.Seq {
				c.add(p, v, spec.Error, "K005", "", "%s should be a list, not %s", k.Value, v.Describe())
			}
		case "build":
			if v.Kind != tree.Map {
				c.add(p, v, spec.Error, "K005", "", "build should be an object, not %s", v.Describe())
				continue
			}
			for _, bk := range v.Keys {
				if !contains(kb.CursorBuildKeys, bk.Value) {
					c.add(p, bk, spec.Error, "K003", "build takes dockerfile, dockerfileContents and context.", "unknown key %q in build", bk.Value)
				}
			}
			df := v.Get("dockerfile")
			if df == nil && v.Get("dockerfileContents") == nil {
				c.add(p, v, spec.Error, "K003", "Add \"dockerfile\": \"Dockerfile\" (a path relative to .cursor).", "build needs a dockerfile or dockerfileContents")
			}
			if df != nil && df.Str() != "" {
				if rp := cursorPath(df.Str()); rp != "." && !c.exists(rp) {
					c.add(p, df, spec.Error, "K004", "Paths in build are relative to the .cursor folder.", "the Dockerfile %s doesn't exist in the repository", rp)
				}
			}
		}
	}
	if root.Get("snapshot") != nil && (root.Get("build") != nil || root.Get("image") != nil) {
		c.add(p, root.Key("snapshot"), spec.Warning, "K006", "Remove the snapshot to build from the Dockerfile.", "snapshot takes precedence, so Cursor ignores build and image")
	}
	f := Facts{File: p}
	if df := root.Path("build", "dockerfile").Str(); df != "" {
		rp := cursorPath(df)
		if t, ok := c.in.Files[rp]; ok {
			f = through(dockerfileFacts(p, t), rp)
			if strings.Contains(t, kb.ScriptPath) {
				if st, ok := c.in.Files[kb.ScriptPath]; ok {
					f = mergeFacts(f, through(scriptFacts(p, st), kb.ScriptPath))
				}
			}
			f.File = p
		}
	}
	return f, true
}

// ---- dev container ----

var devcontainerKeys = []string{
	"$schema", "name", "image", "build", "dockerComposeFile", "service", "runServices", "workspaceFolder",
	"workspaceMount", "shutdownAction", "overrideCommand", "appPort", "runArgs", "features",
	"overrideFeatureInstallOrder", "forwardPorts", "portsAttributes", "otherPortsAttributes",
	"updateRemoteUserUID", "containerEnv", "containerUser", "mounts", "init", "privileged", "capAdd",
	"securityOpt", "remoteEnv", "remoteUser", "initializeCommand", "onCreateCommand",
	"updateContentCommand", "postCreateCommand", "postStartCommand", "postAttachCommand", "waitFor",
	"userEnvProbe", "hostRequirements", "customizations", "secrets", "additionalProperties",
	"dockerFile", "context", "extensions", "settings", "devPort",
}

func (c *checker) devcontainer(p, text string) (Facts, bool) {
	root, err := jsonc.Parse(text, jsonc.JSONC)
	if err != nil {
		je := err.(*jsonc.Error)
		c.add(p, &tree.Node{Line: je.Line, Col: je.Col}, spec.Error, "D001", "The dev container CLI stops on this file, so no container starts.", "devcontainer.json can't be read: %s", je.Msg)
		return Facts{}, false
	}
	if root.Kind != tree.Map {
		c.add(p, root, spec.Error, "D001", "", "devcontainer.json should be an object, not %s", root.Describe())
		return Facts{}, false
	}
	if root.Get("image") == nil && root.Get("build") == nil && root.Get("dockerComposeFile") == nil && root.Get("dockerFile") == nil {
		c.add(p, root, spec.Error, "D002", "Add \"image\", \"build\" or \"dockerComposeFile\".", "devcontainer.json doesn't say what to start: it has no image, build or dockerComposeFile")
	}
	if root.Get("dockerComposeFile") != nil && root.Get("service") == nil {
		c.add(p, root.Key("dockerComposeFile"), spec.Error, "D003", "Name the compose service to attach to, such as \"service\": \"app\".", "dockerComposeFile needs a service")
	}
	for _, k := range root.Keys {
		if !contains(devcontainerKeys, k.Value) {
			hint := ""
			if sug := tree.Suggest(k.Value, devcontainerKeys); sug != "" {
				hint = fmt.Sprintf("Did you mean %q?", sug)
			}
			c.add(p, k, spec.Warning, "D004", hint, "unknown key %q in devcontainer.json", k.Value)
		}
	}
	f := devcontainerFacts(p, root)
	dir := path.Dir(p)
	for _, cf := range root.Get("dockerComposeFile").Strings() {
		cp := path.Clean(path.Join(dir, cf))
		if t, ok := c.in.Files[cp]; ok {
			f = mergeFacts(f, through(composeFacts(p, t), cp))
		}
	}
	f.File = p
	return f, true
}

// ---- cloud-init ----

func (c *checker) cloudInit(p, text string) (Facts, bool) {
	first := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		first = text[:i]
	}
	if strings.TrimRight(first, " \r") != "#cloud-config" {
		c.add(p, &tree.Node{Line: 1, Col: 1}, spec.Error, "I001", "Make #cloud-config the very first line.", "cloud-init treats this file as cloud-config only when its first line is #cloud-config")
	}
	root, err := yaml.Parse(text)
	if err != nil {
		if ye, ok := err.(*yaml.Error); ok {
			c.add(p, &tree.Node{Line: ye.Line, Col: ye.Col}, spec.Error, "I002", "", "cloud-init.yaml is not valid YAML: %s", ye.Msg)
		}
		return Facts{}, false
	}
	f := Facts{File: p}
	for _, wf := range root.Get("write_files").List() {
		if strings.HasSuffix(wf.Get("path").Str(), "setup.sh") {
			content := wf.Get("content")
			// A block scalar's text starts on the line after its key.
			f = mergeFacts(f, shift(scriptFacts(p, content.Str()), lineOf(content)))
		}
	}
	f.File = p
	return f, true
}
