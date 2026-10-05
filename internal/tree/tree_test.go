package tree

import (
	"encoding/json"
	"os"
	"testing"
)

func TestYAMLString(t *testing.T) {
	cases := map[string]string{
		"plain": "plain", "22": `"22"`, "3.10": `"3.10"`, "1_000": `"1_000"`, "2026-09-30": `"2026-09-30"`,
		"yes": `"yes"`, "Off": `"Off"`, "y": `"y"`, ".inf": `".inf"`, ".NaN": `".NaN"`, ".venv/bin/pytest -q": ".venv/bin/pytest -q",
		"../x": "../x", ".5": `".5"`, "-x": `"-x"`, "@x": `"@x"`, "a: b": `"a: b"`, "a:": `"a:"`, "x #y": `"x #y"`,
		"postgres://u:p@localhost:5432/db": "postgres://u:p@localhost:5432/db", "": `""`, " a": `" a"`,
		"tab\there": `"tab\there"`, "line\nbreak": `"line\nbreak"`, "del\x7f": `"del\u007f"`, "nel\u0085": `"nel\u0085"`,
		"sep\u2028": `"sep\u2028"`, "bom\ufeff": `"bom\ufeff"`, "é": `"é"`,
	}
	for in, want := range cases {
		if got := YAMLString(in); got != want {
			t.Errorf("YAMLString(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestDumpYAMLStrings writes YAMLString's output for a list of strings, so
// that tools/quoting can check it against other YAML readers:
//
//	QUOTING_IN=in.json QUOTING_OUT=out.json go test ./internal/tree -run TestDumpYAMLStrings
func TestDumpYAMLStrings(t *testing.T) {
	in, out := os.Getenv("QUOTING_IN"), os.Getenv("QUOTING_OUT")
	if in == "" || out == "" {
		t.Skip("QUOTING_IN and QUOTING_OUT are not set")
	}
	b, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	var ss []string
	if err := json.Unmarshal(b, &ss); err != nil {
		t.Fatal(err)
	}
	m := make(map[string]string, len(ss))
	for _, s := range ss {
		m[s] = YAMLString(s)
	}
	b, _ = json.Marshal(m)
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestJSON(t *testing.T) {
	n := NewMap().
		Set("name", NewStr("a \"b\"")).
		Set("empty", NewMap()).
		Set("list", &Node{Kind: Seq, Items: []*Node{NewBool(true), {Kind: Scalar, Type: Number, Value: "2"}, {Kind: Scalar, Type: Null}}}).
		Set("none", &Node{Kind: Seq}).
		Set("name", NewStr("replaced"))
	want := "{\n  \"name\": \"replaced\",\n  \"empty\": {},\n  \"list\": [\n    true,\n    2,\n    null\n  ],\n  \"none\": []\n}\n"
	if got := JSON(n, "  "); got != want {
		t.Errorf("JSON =\n%s\nwant\n%s", got, want)
	}
	if JSON(nil, "  ") != "null\n" {
		t.Error("nil should be null")
	}
}

func TestAccessors(t *testing.T) {
	m := NewMap().Set("a", &Node{Kind: Seq, Items: []*Node{NewStr("x"), {Kind: Scalar, Type: Null}, NewMap()}})
	if got := m.Get("a").Strings(); len(got) != 1 || got[0] != "x" {
		t.Errorf("Strings = %v", got)
	}
	if m.Get("b") != nil || m.Key("b") != nil || m.Path("a", "b") != nil || m.Get("a").Get("x") != nil {
		t.Error("missing keys should give nil")
	}
	var nilNode *Node
	if nilNode.Str() != "" || nilNode.Strings() != nil || nilNode.List() != nil || !nilNode.IsNull() || nilNode.Describe() != "nothing" {
		t.Error("a nil node should read as empty")
	}
	if NewStr("x").Strings()[0] != "x" || (&Node{Kind: Scalar, Type: Null}).Strings() != nil {
		t.Error("scalars as lists")
	}
	cases := map[string]*Node{
		"a mapping": NewMap(), "a list": {Kind: Seq}, "an empty value": {Kind: Scalar, Type: Null},
		"true/false": NewBool(false), "a number": {Kind: Scalar, Type: Number}, "text": NewStr("x"),
	}
	for want, n := range cases {
		if got := n.Describe(); got != want {
			t.Errorf("Describe = %q, want %q", got, want)
		}
	}
}

func TestSuggest(t *testing.T) {
	keys := []string{"runtimes", "services", "install", "postCreateCommand"}
	cases := map[string]string{"runtime": "runtimes", "Services": "services", "instal": "install", "postCreateCommands": "postCreateCommand", "zzz": "", "": ""}
	for in, want := range cases {
		if got := Suggest(in, keys); got != want {
			t.Errorf("Suggest(%q) = %q, want %q", in, got, want)
		}
	}
}
