package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
)

// FuzzDiagnose: whatever the log holds, Doctor doesn't panic, every line of
// evidence is a line of the log, masked, and a change it proposes either
// applies to the spec cleanly or is refused without touching it.
func FuzzDiagnose(f *testing.F) {
	f.Add(setupLog("ready 1/1: pytest -q", 1, "E   redis.exceptions.ConnectionError: Error 111 connecting to localhost:6379. Connection refused."))
	f.Add(setupLog("machine 1/5: system packages", 100, "E: Unable to locate package libpq-devv"))
	f.Add("#6 0.201 >>> preconfig: machine 1/5: system packages\n#6 ERROR: process did not complete successfully: exit code: 100\n")
	f.Add("2026-09-30T10:00:00.1000000Z ##[group]Run x\n2026-09-30T10:00:01.0000000Z ##[error]Process completed with exit code 1.\n")
	f.Add("verify   55.1s  FAIL   ready 1/1: pytest  (exit code 1)\n               | E  KeyError: 'DATABASE_URL'\n")
	f.Add(`{"v":1,"type":"step.fail","step":"ready 1/1: x","fields":{"exit_code":1}}`)
	f.Add("")
	s, _ := spec.Load(kb.SpecPath, ordersSpec)
	f.Fuzz(func(t *testing.T, text string) {
		d := Diagnose(text, s)
		n := strings.Count(text, "\n") + 1
		for _, l := range d.Evidence {
			if l.N < 1 || l.N > n {
				t.Fatalf("evidence line %d outside a log of %d lines", l.N, n)
			}
			if Mask(l.Text) != l.Text {
				t.Fatalf("evidence isn't masked: %q", l.Text)
			}
		}
		if d.Outcome != Passed && d.Outcome != Failed && d.Outcome != Unclear {
			t.Fatalf("outcome %q", d.Outcome)
		}
		if d.Category == CatUnknown && len(d.Changes) > 0 {
			t.Fatalf("an unknown cause came with a change")
		}
		if len(d.Changes) > 0 {
			out, err := Apply(ordersSpec, d.Changes)
			if err != nil && out != ordersSpec {
				t.Fatalf("a refused change touched the spec")
			}
		}
	})
}

// FuzzApply: whatever the spec's text, Apply either returns a spec that
// loads, or the text it was given.
func FuzzApply(f *testing.F) {
	f.Add(ordersSpec, "add-package", "", "make")
	f.Add("version: 1\npackages: [a]\n", "add-package", "", "b")
	f.Add("version: 1\nenv:\n  A: \"x\" # c\n", "set-env", "A", "y")
	f.Add("version: 1\nservices: {redis: \"7\"}\n", "add-service", "postgres", "16")
	f.Add("version: 1\nruntimes:\n", "set-runtime", "python", "3.12")
	f.Fuzz(func(t *testing.T, src, op, key, value string) {
		out, err := Apply(src, []Change{{Op: op, Key: key, Value: value}})
		if err != nil {
			if out != src {
				t.Fatalf("a refused change touched the text")
			}
			return
		}
		if s, ds := spec.Load(kb.SpecPath, out); s == nil {
			t.Fatalf("Apply returned a spec that doesn't load: %v\n%s", ds, out)
		}
	})
}

func BenchmarkDiagnose(b *testing.B) {
	log, err := os.ReadFile(filepath.Join("..", "verify", "testdata", "fail.log"))
	if err != nil {
		b.Skip("no recorded log")
	}
	s, _ := spec.Load(kb.SpecPath, strings.Replace(ordersSpec, "  redis: \"7\"\n", "", 1))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := Diagnose(string(log), s)
		if d.Rule != "D401" {
			b.Fatal(d.Rule)
		}
	}
}
