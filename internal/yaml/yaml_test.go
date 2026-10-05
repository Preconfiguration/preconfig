package yaml

import (
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/tree"
)

func mustParse(t *testing.T, src string) *tree.Node {
	t.Helper()
	n, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return n
}

func TestScalarsAndTypes(t *testing.T) {
	n := mustParse(t, `
s: hello world
q: "3.12"
sq: 'it''s'
num: 3.10
int: 22
b: true
on: push
nul: ~
empty:
url: postgres://u:p@localhost:5432/db
dash: --health-cmd pg_isready
`)
	cases := []struct {
		key, val string
		typ      tree.Type
	}{
		{"s", "hello world", tree.String},
		{"q", "3.12", tree.String},
		{"sq", "it's", tree.String},
		{"num", "3.10", tree.Number},
		{"int", "22", tree.Number},
		{"b", "true", tree.Bool},
		{"on", "push", tree.String},
		{"nul", "", tree.Null},
		{"empty", "", tree.Null},
		{"url", "postgres://u:p@localhost:5432/db", tree.String},
		{"dash", "--health-cmd pg_isready", tree.String},
	}
	for _, c := range cases {
		v := n.Get(c.key)
		if v == nil {
			t.Fatalf("%s missing", c.key)
		}
		if v.Value != c.val || v.Type != c.typ {
			t.Errorf("%s = %q (type %d), want %q (type %d)", c.key, v.Value, v.Type, c.val, c.typ)
		}
	}
	// "on" as a key stays the string "on", as GitHub reads it.
	if n.Key("on") == nil {
		t.Error("the key on is missing")
	}
}

func TestLineNumbers(t *testing.T) {
	n := mustParse(t, "a: 1\n\n# comment\nb:\n  c: x\n  d:\n    - y\n")
	if got := n.Path("b", "c"); got.Line != 5 || got.Col != 6 {
		t.Errorf("b.c at %d:%d, want 5:6", got.Line, got.Col)
	}
	if got := n.Path("b", "d").Items[0]; got.Line != 7 {
		t.Errorf("b.d[0] at line %d, want 7", got.Line)
	}
	if k := n.Key("b"); k.Line != 4 || k.Col != 1 {
		t.Errorf("key b at %d:%d, want 4:1", k.Line, k.Col)
	}
}

func TestSequences(t *testing.T) {
	n := mustParse(t, `
steps:
  - uses: actions/checkout@v7
    with:
      lfs: true
  - name: two
    run: echo hi
same-indent:
- a
- b
nested:
  - - x
    - y
  - z
flow: [a, "b c", 'd']
flowmap: {a: 1, b: [2, 3]}
empty: []
`)
	steps := n.Get("steps")
	if len(steps.Items) != 2 || steps.Items[0].Path("with", "lfs").Value != "true" || steps.Items[1].Get("run").Value != "echo hi" {
		t.Errorf("steps parsed wrong: %+v", steps)
	}
	if got := n.Get("same-indent").Strings(); strings.Join(got, ",") != "a,b" {
		t.Errorf("same-indent = %v", got)
	}
	nested := n.Get("nested")
	if len(nested.Items) != 2 || strings.Join(nested.Items[0].Strings(), ",") != "x,y" || nested.Items[1].Value != "z" {
		t.Errorf("nested parsed wrong")
	}
	if got := n.Get("flow").Strings(); strings.Join(got, "|") != "a|b c|d" {
		t.Errorf("flow = %v", got)
	}
	fm := n.Get("flowmap")
	if fm.Get("a").Value != "1" || strings.Join(fm.Get("b").Strings(), ",") != "2,3" {
		t.Errorf("flowmap parsed wrong")
	}
	if e := n.Get("empty"); e.Kind != tree.Seq || len(e.Items) != 0 {
		t.Errorf("empty list parsed wrong")
	}
}

func TestBlockScalars(t *testing.T) {
	n := mustParse(t, "lit: |\n  one\n  # not a comment\n    indented\n\n  two\nstrip: |-\n  x\n\nkeep: |+\n  y\n\nfold: >\n  a\n  b\n\n  c\nfoldstrip: >-\n  --a\n  --b\nnext: 1\n")
	cases := map[string]string{
		"lit":       "one\n# not a comment\n  indented\n\ntwo\n",
		"strip":     "x",
		"keep":      "y\n\n",
		"fold":      "a b\nc\n",
		"foldstrip": "--a --b",
	}
	for k, want := range cases {
		if got := n.Get(k).Value; got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if n.Get("next").Value != "1" {
		t.Error("the key after the block scalars is lost")
	}
}

func TestQuotedAndPlainContinuation(t *testing.T) {
	n := mustParse(t, "a: \"x\\ty\\u00e9\"\nb: \"line one\n  line two\"\nc: plain one\n  plain two\nd: \"joined\\\n  here\"\n")
	if got := n.Get("a").Value; got != "x\ty\u00e9" {
		t.Errorf("a = %q", got)
	}
	if got := n.Get("b").Value; got != "line one line two" {
		t.Errorf("b = %q", got)
	}
	if got := n.Get("c").Value; got != "plain one plain two" {
		t.Errorf("c = %q", got)
	}
	if got := n.Get("d").Value; got != "joinedhere" {
		t.Errorf("d = %q", got)
	}
}

func TestEscapes(t *testing.T) {
	n := mustParse(t, `a: "\x41\u00e9\U0001F600\0\a\b\e\f\v\N\_\L\P\/\ \"\\\t|\r"`+"\n")
	want := "A\u00e9\U0001F600\x00\a\b\x1b\f\v\u0085\u00a0\u2028\u2029/ \"\\\t|\r"
	if got := n.Get("a").Value; got != want {
		t.Errorf("escapes = %q, want %q", got, want)
	}
	for _, bad := range []string{`a: "\x4"`, `a: "\uZZZZ"`, `a: "x\"`, `a: "\q"`} {
		if _, err := Parse(bad + "\n"); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func TestNumbers(t *testing.T) {
	cases := map[string]tree.Type{
		"1": tree.Number, "-1.5": tree.Number, "+2": tree.Number, "1e5": tree.Number, ".5": tree.Number, "0x1F": tree.Number, "0o17": tree.Number,
		"1_000": tree.String, "0x": tree.String, "0xZZ": tree.String, "1.2.3": tree.String, "+": tree.String, "Infinity": tree.String, "1 2": tree.String,
	}
	for in, want := range cases {
		n := mustParse(t, "a: "+in+"\n")
		if got := n.Get("a").Type; got != want {
			t.Errorf("%q has type %d, want %d", in, got, want)
		}
	}
}

func TestDocumentEnd(t *testing.T) {
	n := mustParse(t, "a: 1\n... # end\n")
	if n.Get("a").Value != "1" {
		t.Error("... ended the document too early")
	}
}

func TestErrorString(t *testing.T) {
	_, err := Parse("a: 1\na: 2\n")
	if err == nil || !strings.HasPrefix(err.Error(), "line 2") {
		t.Errorf("error = %v", err)
	}
}

func TestAnchorsAndAliases(t *testing.T) {
	n := mustParse(t, "base: &b\n  x: 1\nuse: *b\nlist: [&s one, *s]\n")
	if n.Get("use").Get("x").Value != "1" {
		t.Error("alias to a map failed")
	}
	if got := n.Get("list").Strings(); strings.Join(got, ",") != "one,one" {
		t.Errorf("flow alias = %v", got)
	}
	if _, err := Parse("a: *nope\n"); err == nil {
		t.Error("an undefined alias was accepted")
	}
}

func TestDocumentMarkers(t *testing.T) {
	n := mustParse(t, "%YAML 1.2\n---\na: 1\n...\n")
	if n.Get("a").Value != "1" {
		t.Error("document with markers parsed wrong")
	}
	n = mustParse(t, "")
	if n.Type != tree.Null {
		t.Error("an empty document should be null")
	}
	if _, err := Parse("a: 1\n---\nb: 2\n"); err == nil {
		t.Error("a second document was accepted")
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		src  string
		line int
		msg  string
	}{
		{"a: 1\na: 2\n", 2, "appears twice"},
		{"a:\n\tb: 1\n", 2, "tab"},
		{"a: b: c\n", 1, "put the value in quotes"},
		{"a: [1, 2\n", 1, "never closed"},
		{"a: \"x\n", 1, "never closed"},
		{"a: 1\n  b: 2\n", 2, "inside a text value"},
		{"a:\n  - x\n  b: 1\n", 3, "unexpected indentation"},
		{"a: 1\n- x\n", 2, "where a key was expected"},
		{"? complex\n", 1, "complex keys"},
		{"a: {x: 1, x: 2}\n", 1, "appears twice"},
		{"a: \"\\q\"\n", 1, "unknown escape"},
		{"k: |x\n  y\n", 1, "block scalar"},
		{"a: 1\nb\n", 2, "expected"},
		{"a: [1] x\n", 1, "after the closing bracket"},
	}
	for _, c := range cases {
		_, err := Parse(c.src)
		if err == nil {
			t.Errorf("%q: no error", c.src)
			continue
		}
		ye, ok := err.(*Error)
		if !ok {
			t.Errorf("%q: error type %T", c.src, err)
			continue
		}
		if ye.Line != c.line || !strings.Contains(ye.Msg, c.msg) {
			t.Errorf("%q: got line %d %q, want line %d containing %q", c.src, ye.Line, ye.Msg, c.line, c.msg)
		}
	}
}

func TestDuplicateKeysAllowed(t *testing.T) {
	n, err := ParseWithOptions("a: 1\na: 2\n", Options{AllowDuplicateKeys: true})
	if err != nil {
		t.Fatal(err)
	}
	if n.Get("a").Value != "2" || len(n.Keys) != 1 {
		t.Errorf("the last value should win")
	}
}

func TestDeepNestingIsRefused(t *testing.T) {
	src := strings.Repeat("[", 500) + strings.Repeat("]", 500)
	if _, err := Parse("a: " + src + "\n"); err == nil {
		t.Error("500 nested lists were accepted")
	}
}

func TestInvalidUTF8(t *testing.T) {
	if _, err := Parse("a: \xff\n"); err == nil {
		t.Error("invalid UTF-8 was accepted")
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"a: 1\n", "- x\n- y\n", "a: |\n  b\n", "a: [1, {b: c}]\n", "a: &x 1\nb: *x\n",
		"on:\n  push:\n    paths: [a]\n", "\"k\": 'v'\n", "a: >-\n  x\n  y\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		n, err := Parse(src)
		if err == nil && n == nil {
			t.Fatal("nil node without an error")
		}
	})
}
