package textdiff

import (
	"strings"
	"testing"
)

func TestEqualTextsGiveNoDiff(t *testing.T) {
	if d := Unified("a", "b", "x\ny\n", "x\ny\n", 3); d != "" {
		t.Errorf("diff of equal texts = %q", d)
	}
}

func TestUnified(t *testing.T) {
	old := "one\ntwo\nthree\nfour\nfive\nsix\nseven\n"
	new := "one\ntwo\nTHREE\nfour\nfive\nsix\nseven\neight\n"
	got := Unified("a/f", "b/f", old, new, 1)
	want := "--- a/f\n+++ b/f\n@@ -2,3 +2,3 @@\n two\n-three\n+THREE\n four\n@@ -7 +7,2 @@\n seven\n+eight\n"
	if got != want {
		t.Errorf("diff =\n%s\nwant\n%s", got, want)
	}
}

func TestMergesNearbyChanges(t *testing.T) {
	old := "a\nb\nc\nd\ne\n"
	new := "A\nb\nc\nD\ne\n"
	got := Unified("a", "b", old, new, 2)
	if strings.Count(got, "@@ ") != 1 {
		t.Errorf("changes two lines apart should share a hunk:\n%s", got)
	}
}

func TestAddToEmpty(t *testing.T) {
	got := Unified("a", "b", "", "x\ny\n", 3)
	if !strings.Contains(got, "@@ -0,0 +1,2 @@\n+x\n+y\n") {
		t.Errorf("diff from empty =\n%s", got)
	}
}

func TestStat(t *testing.T) {
	a, r := Stat("a\nb\nc\n", "a\nc\nd\ne\n")
	if a != 2 || r != 1 {
		t.Errorf("Stat = +%d -%d, want +2 -1", a, r)
	}
}
