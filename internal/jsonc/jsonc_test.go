package jsonc

import (
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/tree"
)

func TestParseValues(t *testing.T) {
	n, err := Parse(`{
  // a comment
  "name": "orders-api", /* block */
  "n": -1.5e3,
  "ok": true,
  "none": null,
  "list": [1, "two", {"three": 3},],
  "esc": "a\"b\\c\u00e9\ud83d\ude00",
}`, JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if n.Get("name").Value != "orders-api" || n.Get("name").Line != 3 {
		t.Errorf("name = %q at line %d", n.Get("name").Value, n.Get("name").Line)
	}
	if n.Get("n").Type != tree.Number || n.Get("n").Value != "-1.5e3" {
		t.Error("number parsed wrong")
	}
	if n.Get("ok").Type != tree.Bool || n.Get("none").Type != tree.Null {
		t.Error("literals parsed wrong")
	}
	if len(n.Get("list").Items) != 3 || n.Get("list").Items[2].Get("three").Value != "3" {
		t.Error("list parsed wrong")
	}
	if got := n.Get("esc").Value; got != "a\"b\\c\u00e9\U0001F600" {
		t.Errorf("escapes = %q", got)
	}
}

func TestStrictness(t *testing.T) {
	cases := []struct {
		src  string
		opts Options
		line int
		msg  string
	}{
		{"{\"a\": 1,}", Options{}, 1, "trailing comma"},
		{"{\"a\": [1,]}", Options{Comments: true}, 1, "trailing comma"},
		{"// hi\n{}", Options{}, 1, "comments are not allowed"},
		{"{\n  \"a\": 1\n}\n}\n", JSONC, 4, "after the end of the JSON value"},
		{"{\"a\": 1, \"a\": 2}", JSONC, 1, "appears twice"},
		{"{'a': 1}", JSONC, 1, "expected a key in double quotes"},
		{"[\"a\n\"]", JSONC, 1, "runs past the end of its line"},
		{"{\"a\" 1}", JSONC, 1, "expected \":\""},
		{"[1 2]", JSONC, 1, "expected \",\" or \"]\""},
		{"", JSONC, 1, "empty"},
		{"/* never closed", JSONC, 1, "never closed"},
		{"[01]", JSONC, 1, "expected"},
		{"{\"a\": tru}", JSONC, 1, "unexpected"},
	}
	for _, c := range cases {
		_, err := Parse(c.src, c.opts)
		if err == nil {
			t.Errorf("%q: no error", c.src)
			continue
		}
		je := err.(*Error)
		if je.Line != c.line || !strings.Contains(je.Msg, c.msg) {
			t.Errorf("%q: got line %d %q, want line %d containing %q", c.src, je.Line, je.Msg, c.line, c.msg)
		}
	}
}

func TestTrailingCommaAllowedWhenAsked(t *testing.T) {
	if _, err := Parse(`{"a": [1, 2,], "b": {"c": 1,},}`, JSONC); err != nil {
		t.Errorf("JSONC should allow trailing commas: %v", err)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{`{}`, `{"a":[1,2,{"b":null}]}`, "// c\n{\"a\": 1,}", `"\u00e9"`, `[1e5, -0.5]`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		for _, o := range []Options{{}, JSONC} {
			n, err := Parse(src, o)
			if err == nil && n == nil {
				t.Fatal("nil node without an error")
			}
		}
	})
}
