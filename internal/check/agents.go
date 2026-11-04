package check

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"preconfiguration.com/preconfig/internal/jsonc"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
	"preconfiguration.com/preconfig/internal/yaml"
)

// Rules for the agent platforms added in 0.2: Claude Code's settings file,
// Ona's tasks and services, Devin's blueprint. Codes: A for Claude Code, O
// for Ona, V for Devin.

// ---- Claude Code ----

func (c *checker) claudeSettings(p, text string) {
	opts := jsonc.Options{Comments: true, TrailingCommas: true}
	root, err := jsonc.Parse(text, opts)
	if je, ok := err.(*jsonc.Error); ok && strings.Contains(je.Msg, "appears twice") {
		c.add(p, &tree.Node{Line: je.Line, Col: je.Col}, spec.Warning, "A005", "Keep one; Claude Code uses the last.", "%s", je.Msg)
		opts.LastDuplicate = true
		root, err = jsonc.Parse(text, opts)
	}
	if err != nil {
		je := err.(*jsonc.Error)
		c.add(p, &tree.Node{Line: je.Line, Col: je.Col}, spec.Error, "A001", "Claude Code skips a settings file it can't read, hooks and all.", "settings.json can't be read: %s", je.Msg)
		return
	}
	if root.Kind != tree.Map {
		c.add(p, root, spec.Error, "A001", "", "settings.json should be an object, not %s", root.Describe())
		return
	}
	if env := root.Get("env"); env != nil {
		if env.Kind != tree.Map {
			c.add(p, env, spec.Error, "A002", "", "env should be an object of names and values")
		} else {
			for i, v := range env.Values {
				if v.Kind != tree.Scalar || v.Type != tree.String {
					c.add(p, v, spec.Warning, "A002", "Quote it, as Claude Code passes environment values as strings.", "env %s should be a string", env.Keys[i].Value)
				}
			}
		}
	}
	hooks := root.Get("hooks")
	if hooks == nil {
		return
	}
	if hooks.Kind != tree.Map {
		c.add(p, hooks, spec.Error, "A003", "", "hooks should be an object keyed by event name")
		return
	}
	ss := hooks.Get("SessionStart")
	if ss == nil {
		return
	}
	if ss.Kind != tree.Seq {
		c.add(p, ss, spec.Error, "A003", "", "hooks.SessionStart should be a list")
		return
	}
	for _, e := range ss.Items {
		if e.Kind != tree.Map {
			c.add(p, e, spec.Error, "A003", "", "a SessionStart entry should be an object with matcher and hooks")
			continue
		}
		hs := e.Get("hooks")
		if hs == nil || hs.Kind != tree.Seq {
			c.add(p, e, spec.Error, "A003", "", "a SessionStart entry needs a hooks list")
			continue
		}
		for _, h := range hs.Items {
			if h.Get("type").Str() == "command" && strings.TrimSpace(h.Get("command").Str()) == "" {
				c.add(p, h, spec.Error, "A003", "", "a command hook needs a command")
			}
			cmd := h.Get("command").Str()
			if strings.Contains(cmd, kb.ClaudeScriptPath) && !c.exists(kb.ClaudeScriptPath) {
				c.add(p, h, spec.Error, "A004", "Run preconfig build, or commit "+kb.ClaudeScriptPath+".", "the SessionStart hook runs %s, which isn't in the repository, so every session starts with a failed hook", kb.ClaudeScriptPath)
			}
		}
	}
}

// ---- Ona ----

var onaIDRe = regexp.MustCompile(kb.OnaIDPattern)

func (c *checker) onaConfig(p, text string) {
	root, err := yaml.Parse(text)
	if err != nil {
		if ye, ok := err.(*yaml.Error); ok {
			c.add(p, &tree.Node{Line: ye.Line, Col: ye.Col}, spec.Error, "O001", "", "%s is not valid YAML: %s", base(p), ye.Msg)
		}
		return
	}
	if root == nil || root.IsNull() {
		return
	}
	if root.Kind != tree.Map {
		c.add(p, root, spec.Error, "O001", "", "%s should be a mapping with services and tasks", base(p))
		return
	}
	c.unknownKeys(p, root, kb.OnaTopKeys, "O002", "Ona's config")
	ids := map[string]*tree.Node{}
	deps := map[string][]string{}
	for _, section := range []string{"services", "tasks"} {
		m := root.Get(section)
		if m == nil || m.IsNull() {
			continue
		}
		if m.Kind != tree.Map {
			c.add(p, m, spec.Error, "O002", "", "%s should be a mapping of IDs", section)
			continue
		}
		for i, k := range m.Keys {
			id, item := k.Value, m.Values[i]
			if !onaIDRe.MatchString(id) {
				c.add(p, k, spec.Error, "O003", "Use letters, digits, - and _, up to 128.", "the ID %q isn't one Ona accepts", id)
			}
			if item.Kind != tree.Map {
				c.add(p, item, spec.Error, "O004", "", "%s.%s should be a mapping", section, id)
				continue
			}
			if section == "tasks" {
				ids[id] = k
				c.unknownKeys(p, item, kb.OnaTaskKeys, "O002", "an Ona task")
				if strings.TrimSpace(item.Get("command").Str()) == "" {
					c.add(p, item, spec.Error, "O004", "", "the task %s needs a command", id)
				}
				deps[id] = item.Get("dependsOn").Strings()
			} else {
				c.unknownKeys(p, item, kb.OnaServiceKeys, "O002", "an Ona service")
				if cmds := item.Get("commands"); cmds != nil && strings.TrimSpace(cmds.Get("start").Str()) == "" {
					c.add(p, cmds, spec.Error, "O004", "", "the service %s needs commands.start", id)
				}
			}
			if item.Get("name").Str() == "" {
				c.add(p, item, spec.Error, "O004", "", "%s.%s needs a name", section, id)
			}
			for _, t := range item.Get("triggeredBy").List() {
				if !contains(kb.OnaTriggers, t.Str()) {
					hint := "Triggers: " + strings.Join(kb.OnaTriggers, ", ") + "."
					if t.Str() == "postMachineStart" || t.Str() == "beforeSnapshot" {
						hint = "Ona sets that trigger through its API only."
					}
					c.add(p, t, spec.Error, "O005", hint, "%s.%s: Ona doesn't take the trigger %q in a config file", section, id, t.Str())
				}
			}
		}
	}
	// dependsOn names tasks that exist, without cycles.
	var names []string
	for id := range deps {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, id := range names {
		for _, d := range deps[id] {
			if _, ok := ids[d]; !ok {
				c.add(p, ids[id], spec.Error, "O006", "", "the task %s depends on %q, which isn't a task here", id, d)
			}
		}
	}
	state := map[string]int{}
	var visit func(id string) bool
	visit = func(id string) bool {
		switch state[id] {
		case 1:
			return true
		case 2:
			return false
		}
		state[id] = 1
		for _, d := range deps[id] {
			if _, ok := ids[d]; ok && visit(d) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for _, id := range names {
		if state[id] == 0 && visit(id) {
			c.add(p, ids[id], spec.Error, "O006", "", "the tasks' dependsOn go round in a circle through %s", id)
			break
		}
	}
}

// ---- Devin ----

func (c *checker) devinBlueprint(p, text string) {
	root, err := yaml.Parse(text)
	if err != nil {
		if ye, ok := err.(*yaml.Error); ok {
			c.add(p, &tree.Node{Line: ye.Line, Col: ye.Col}, spec.Error, "V001", "", "blueprint.yaml is not valid YAML: %s", ye.Msg)
		}
		return
	}
	if root == nil || root.IsNull() {
		return
	}
	if root.Kind != tree.Map {
		c.add(p, root, spec.Error, "V001", "", "blueprint.yaml should be a mapping")
		return
	}
	for _, k := range root.Keys {
		if k.Value == "post-build" {
			c.add(p, k, spec.Error, "V002", "Move these steps to initialize or maintenance.", "post-build is for organization and enterprise blueprints, not a repository's")
		}
	}
	c.unknownKeys(p, root, append(append([]string(nil), kb.DevinKeys...), "post-build"), "V002", "a Devin blueprint")
	for _, phase := range []string{"initialize", "maintenance"} {
		n := root.Get(phase)
		if n == nil || n.IsNull() {
			continue
		}
		if n.Kind == tree.Scalar {
			continue
		}
		if n.Kind != tree.Seq {
			c.add(p, n, spec.Error, "V003", "", "%s should be a script or a list of steps", phase)
			continue
		}
		for _, st := range n.Items {
			if st.Kind == tree.Scalar {
				continue
			}
			if st.Kind != tree.Map {
				c.add(p, st, spec.Error, "V003", "", "a %s step should be a command or a mapping", phase)
				continue
			}
			c.unknownKeys(p, st, []string{"name", "run", "uses", "with", "env"}, "V003", "a Devin step")
			run, uses := st.Get("run"), st.Get("uses")
			switch {
			case run != nil && uses != nil:
				c.add(p, st, spec.Error, "V003", "Split it into two steps.", "a step has run or uses, not both")
			case run == nil && uses == nil:
				c.add(p, st, spec.Error, "V003", "", "a step needs run or uses")
			}
			if st.Get("with") != nil && uses == nil {
				c.add(p, st.Key("with"), spec.Error, "V003", "", "with goes only with uses")
			}
			if uses != nil && phase == "maintenance" {
				c.add(p, uses, spec.Error, "V004", "Move it to initialize.", "Devin doesn't run uses steps in maintenance")
			}
		}
	}
	if k := root.Get("knowledge"); k != nil && !k.IsNull() {
		if k.Kind != tree.Seq {
			c.add(p, k, spec.Error, "V005", "", "knowledge should be a list of name and contents")
		} else {
			for _, it := range k.Items {
				if it.Kind != tree.Map || it.Get("name").Str() == "" || it.Get("contents") == nil {
					c.add(p, it, spec.Error, "V005", "", "a knowledge entry needs a name and contents")
				}
			}
		}
	}
}

// unknownKeys reports keys outside allowed.
func (c *checker) unknownKeys(p string, m *tree.Node, allowed []string, code, what string) {
	for _, k := range m.Keys {
		if !contains(allowed, k.Value) {
			hint := ""
			if sug := tree.Suggest(k.Value, allowed); sug != "" {
				hint = fmt.Sprintf("Did you mean %q?", sug)
			}
			c.add(p, k, spec.Warning, code, hint, "%s has no key %q, so it's ignored", what, k.Value)
		}
	}
}
