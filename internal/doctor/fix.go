package doctor

import (
	"fmt"
	"strings"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
	"preconfiguration.com/preconfig/internal/yaml"
)

// topOrder is the order of preconfig.yaml's top-level keys, for placing a key
// the file doesn't have yet.
var topOrder = []string{"version", "name", "base", "runtimes", "tools", "packages", "services", "env", "secrets", "setup", "ready", "targets", "repo"}

// Apply writes changes into the text of preconfig.yaml. It edits only the lines
// it has to, so comments and layout stay as they were, and it reads the result
// back to make sure the spec is still valid and says what the change meant.
func Apply(src string, changes []Change) (string, error) {
	out := src
	for _, c := range changes {
		next, err := apply1(out, c)
		if err != nil {
			return src, fmt.Errorf("%s: %w", c.String(), err)
		}
		out = next
	}
	s, ds := spec.Load(kb.SpecPath, out)
	if s == nil {
		var msgs []string
		for _, d := range ds {
			if d.Severity == spec.Error {
				msgs = append(msgs, d.String())
			}
		}
		return src, fmt.Errorf("the changed spec has errors: %s", strings.Join(msgs, "; "))
	}
	for _, c := range changes {
		if !holds(s, c) {
			return src, fmt.Errorf("%s: the changed spec doesn't say so", c.String())
		}
	}
	return out, nil
}

// holds reports whether a spec reflects a change.
func holds(s *spec.Spec, c Change) bool {
	switch c.Op {
	case "add-package":
		return contains(s.Packages, c.Value)
	case "replace-package":
		return contains(s.Packages, c.Value) && !contains(s.Packages, c.Old)
	case "add-tool":
		_, ok := s.Tool(c.Value)
		return ok
	case "add-secret":
		return contains(s.Secrets, c.Key)
	case "add-service":
		sv, ok := s.Service(c.Key)
		return ok && sv.Version == c.Value
	case "set-runtime":
		switch c.Key {
		case "python":
			return s.Python == c.Value
		case "node":
			return s.Node == c.Value
		case "go":
			return s.Go == c.Value
		}
	case "add-env", "set-env":
		for _, e := range s.Env {
			if e.Key == c.Key {
				return e.Value == c.Value
			}
		}
	}
	return false
}

func apply1(src string, c Change) (string, error) {
	switch c.Op {
	case "add-package":
		return listAdd(src, "packages", c.Value)
	case "replace-package":
		return listReplace(src, "packages", c.Old, c.Value)
	case "add-tool":
		return listAdd(src, "tools", c.Value)
	case "add-secret":
		return listAdd(src, "secrets", c.Key)
	case "add-service":
		var body []string
		switch {
		case c.Key == "postgres" && c.User != "":
			body = []string{
				"version: " + quote(c.Value),
				"user: " + tree.YAMLString(c.User),
				"password: " + tree.YAMLString(c.Password),
				"database: " + tree.YAMLString(c.Database),
			}
			return mapAdd(src, "services", c.Key, "", body)
		default:
			return mapAdd(src, "services", c.Key, quote(c.Value), nil)
		}
	case "set-runtime":
		return mapSet(src, "runtimes", c.Key, quote(c.Value))
	case "add-env":
		return mapAdd(src, "env", c.Key, tree.YAMLString(c.Value), nil)
	case "set-env":
		return mapSet(src, "env", c.Key, tree.YAMLString(c.Value))
	}
	return src, fmt.Errorf("Doctor can't apply %q", c.Op)
}

func quote(v string) string { return `"` + v + `"` }

// doc is the spec's text as lines, with its parse.
type doc struct {
	lines []string
	root  *tree.Node
	nl    bool // the text ended with a line break
}

func parse(src string) (*doc, error) {
	root, err := yaml.Parse(src)
	if err != nil {
		return nil, err
	}
	if root.Kind != tree.Map {
		return nil, fmt.Errorf("preconfig.yaml isn't a mapping")
	}
	d := &doc{root: root, nl: strings.HasSuffix(src, "\n")}
	d.lines = strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	return d, nil
}

func (d *doc) String() string {
	s := strings.Join(d.lines, "\n")
	if d.nl {
		s += "\n"
	}
	return s
}

// keyIndex is the position of a top-level key in the file, or -1.
func (d *doc) keyIndex(key string) int {
	for i, k := range d.root.Keys {
		if k.Value == key {
			return i
		}
	}
	return -1
}

// blockEnd is the index of the last line that belongs to the top-level key at
// position i: the line before the next top-level key, less blank lines and
// comments that belong to the next block.
func (d *doc) blockEnd(i int) int {
	start := d.root.Keys[i].Line - 1
	next := len(d.lines)
	for _, k := range d.root.Keys {
		if l := k.Line - 1; l > start && l < next {
			next = l
		}
	}
	end := next - 1
	for end > start {
		t := strings.TrimSpace(d.lines[end])
		if t == "" || (strings.HasPrefix(t, "#") && !strings.HasPrefix(d.lines[end], " ")) {
			end--
			continue
		}
		break
	}
	return end
}

// insert puts lines after index at.
func (d *doc) insert(at int, lines ...string) {
	out := append([]string{}, d.lines[:at+1]...)
	out = append(out, lines...)
	d.lines = append(out, d.lines[at+1:]...)
}

// addBlock adds a new top-level key, in its usual place.
func (d *doc) addBlock(key string, body []string) {
	want := -1
	for i, k := range topOrder {
		if k == key {
			want = i
		}
	}
	after := -1 // position in d.root.Keys of the key the new block follows
	best := -1
	for i, k := range d.root.Keys {
		for j, o := range topOrder {
			if o == k.Value && j < want && j > best {
				best, after = j, i
			}
		}
	}
	block := append([]string{"", key + ":"}, body...)
	if after < 0 {
		// Nothing that comes before it: put it first.
		d.lines = append(append(block[1:], ""), d.lines...)
		return
	}
	d.insert(d.blockEnd(after), block...)
}

// itemIndent is the indentation and dash of a block sequence's items, from the
// first item's line.
func (d *doc) itemIndent(seq *tree.Node) string {
	if len(seq.Items) == 0 {
		return "  "
	}
	l := d.lines[seq.Items[0].Line-1]
	return l[:len(l)-len(strings.TrimLeft(l, " "))]
}

func flowOnKeyLine(key, val *tree.Node) bool {
	if val.Kind == tree.Seq || val.Kind == tree.Map {
		if len(val.Items) > 0 && val.Items[0].Line == key.Line {
			return true
		}
		if len(val.Keys) > 0 && val.Keys[0].Line == key.Line {
			return true
		}
		// An empty flow collection: "tools: []".
		if len(val.Items) == 0 && len(val.Keys) == 0 && val.Line == key.Line {
			return true
		}
	}
	return false
}

func listAdd(src, key, item string) (string, error) {
	d, err := parse(src)
	if err != nil {
		return src, err
	}
	i := d.keyIndex(key)
	if i < 0 {
		d.addBlock(key, []string{"  - " + tree.YAMLString(item)})
		return d.String(), nil
	}
	k, v := d.root.Keys[i], d.root.Values[i]
	if v.IsNull() {
		d.insert(k.Line-1, "  - "+tree.YAMLString(item))
		return d.String(), nil
	}
	if v.Kind != tree.Seq {
		return src, fmt.Errorf("%s isn't a list", key)
	}
	for _, it := range v.Items {
		if it.Value == item {
			return src, nil
		}
	}
	if flowOnKeyLine(k, v) {
		// Rewrite "key: [a, b]" as a block list, which takes a new item simply.
		body := []string{}
		for _, it := range v.Items {
			body = append(body, "  - "+tree.YAMLString(it.Value))
		}
		body = append(body, "  - "+tree.YAMLString(item))
		end := d.blockEnd(i)
		d.lines = append(append(append([]string{}, d.lines[:k.Line-1]...), append([]string{key + ":"}, body...)...), d.lines[end+1:]...)
		return d.String(), nil
	}
	d.insert(d.blockEnd(i), d.itemIndent(v)+"- "+tree.YAMLString(item))
	return d.String(), nil
}

func listReplace(src, key, old, item string) (string, error) {
	d, err := parse(src)
	if err != nil {
		return src, err
	}
	i := d.keyIndex(key)
	if i < 0 {
		return src, fmt.Errorf("%s isn't in the spec", key)
	}
	v := d.root.Values[i]
	for _, it := range v.Items {
		if it.Value != old {
			continue
		}
		if err := d.replaceScalar(it, tree.YAMLString(item)); err != nil {
			return src, err
		}
		return d.String(), nil
	}
	return src, fmt.Errorf("%s isn't under %s", old, key)
}

// replaceScalar replaces a scalar's text on its line.
func (d *doc) replaceScalar(n *tree.Node, with string) error {
	if n.Kind != tree.Scalar || n.Line < 1 || n.Line > len(d.lines) {
		return fmt.Errorf("can't find the value to change")
	}
	l := d.lines[n.Line-1]
	start := n.Col - 1
	if start < 0 || start > len(l) {
		return fmt.Errorf("can't find the value to change")
	}
	end := len(l)
	switch {
	case n.Style == tree.Double || n.Style == tree.Single:
		q := l[start]
		j := start + 1
		for j < len(l) {
			if l[j] == '\\' && q == '"' {
				j += 2
				continue
			}
			if l[j] == q {
				if q == '\'' && j+1 < len(l) && l[j+1] == '\'' {
					j += 2
					continue
				}
				break
			}
			j++
		}
		if j >= len(l) {
			return fmt.Errorf("a quoted value runs over more than one line")
		}
		end = j + 1
	case n.Style == tree.Plain:
		if k := strings.Index(l[start:], " #"); k >= 0 {
			end = start + k
		}
		for end > start && (l[end-1] == ' ' || l[end-1] == '\t') {
			end--
		}
		if strings.TrimSpace(l[start:end]) != n.Value {
			return fmt.Errorf("the value runs over more than one line")
		}
	default:
		return fmt.Errorf("can't change a block scalar")
	}
	d.lines[n.Line-1] = l[:start] + with + l[end:]
	return nil
}

func mapAdd(src, key, name, value string, body []string) (string, error) {
	d, err := parse(src)
	if err != nil {
		return src, err
	}
	entry := func(indent string) []string {
		if body == nil {
			return []string{indent + name + ": " + value}
		}
		out := []string{indent + name + ":"}
		for _, b := range body {
			out = append(out, indent+"  "+b)
		}
		return out
	}
	i := d.keyIndex(key)
	if i < 0 {
		d.addBlock(key, entry("  "))
		return d.String(), nil
	}
	k, v := d.root.Keys[i], d.root.Values[i]
	if v.IsNull() {
		d.insert(k.Line-1, entry("  ")...)
		return d.String(), nil
	}
	if v.Kind != tree.Map {
		return src, fmt.Errorf("%s isn't a mapping", key)
	}
	if v.Get(name) != nil {
		return src, fmt.Errorf("%s already has %s", key, name)
	}
	if flowOnKeyLine(k, v) {
		return src, fmt.Errorf("%s is written in flow style; Doctor only adds to block style", key)
	}
	first := d.lines[v.Keys[0].Line-1]
	indent := first[:len(first)-len(strings.TrimLeft(first, " "))]
	d.insert(d.blockEnd(i), entry(indent)...)
	return d.String(), nil
}

func mapSet(src, key, name, value string) (string, error) {
	d, err := parse(src)
	if err != nil {
		return src, err
	}
	i := d.keyIndex(key)
	if i < 0 {
		return mapAdd(src, key, name, value, nil)
	}
	v := d.root.Values[i]
	if v.Kind != tree.Map {
		if v.IsNull() {
			return mapAdd(src, key, name, value, nil)
		}
		return src, fmt.Errorf("%s isn't a mapping", key)
	}
	cur := v.Get(name)
	if cur == nil {
		return mapAdd(src, key, name, value, nil)
	}
	if err := d.replaceScalar(cur, value); err != nil {
		return src, err
	}
	return d.String(), nil
}
