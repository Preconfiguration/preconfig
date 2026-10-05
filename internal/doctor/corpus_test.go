package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
)

// corpusCase is a case.json from testdata/doctor: how the case was made and the
// cause that was planted, written before Doctor read the log.
type corpusCase struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Expect struct {
		Outcome  string            `json:"outcome"`
		Phase    string            `json:"phase"`
		Category string            `json:"category"`
		Host     string          `json:"host"`
		Change   json.RawMessage `json:"change"`
	} `json:"expect"`
}

// needed is the changes a case calls for: none, one, or several that are all
// needed. Each maps a field to the values that are right for it.
func (c corpusCase) needed() []map[string][]string {
	raw := strings.TrimSpace(string(c.Expect.Change))
	if raw == "" || raw == "null" {
		return nil
	}
	var list []map[string]any
	if strings.HasPrefix(raw, "[") {
		json.Unmarshal(c.Expect.Change, &list)
	} else {
		var one map[string]any
		json.Unmarshal(c.Expect.Change, &one)
		list = []map[string]any{one}
	}
	var out []map[string][]string
	for _, w := range list {
		m := map[string][]string{}
		for k, v := range w {
			switch x := v.(type) {
			case string:
				m[k] = []string{x}
			case []any:
				for _, y := range x {
					m[k] = append(m[k], fmt.Sprint(y))
				}
			}
		}
		out = append(out, m)
	}
	return out
}

// verdict scores a diagnosis the way tools/doctor/evaluate.py does: right,
// partial (an incomplete fix, nothing in it wrong), unknown (Doctor said it
// doesn't know) or wrong.
func verdict(c corpusCase, d Diagnosis) (string, string) {
	e := c.Expect
	if e.Outcome == "passed" {
		if d.Outcome == Passed {
			return "right", ""
		}
		return "wrong", "a passing log got a diagnosis: " + d.Cause
	}
	if d.Outcome == Passed {
		return "wrong", "a failing log was read as passing"
	}
	if d.Category == CatUnknown {
		return "unknown", ""
	}
	var why []string
	if d.Category != e.Category {
		why = append(why, fmt.Sprintf("category %s, want %s", d.Category, e.Category))
	}
	cv := changeVerdict(c.needed(), d.Changes)
	if cv == "wrong" {
		why = append(why, fmt.Sprintf("changes %v, want %s", d.Changes, e.Change))
	}
	if e.Host != "" && !strings.Contains(d.Cause+" "+d.Advice, e.Host) {
		why = append(why, "host "+e.Host+" not named")
	}
	if len(why) > 0 {
		return "wrong", strings.Join(why, "; ")
	}
	if cv == "partial" {
		return "partial", fmt.Sprintf("changes %v, want %s", d.Changes, e.Change)
	}
	return "right", ""
}

func oneMatches(want map[string][]string, g Change) bool {
	fields := map[string]string{"op": g.Op, "key": g.Key, "name": g.Key, "value": g.Value, "version": g.Value,
		"old": g.Old, "user": g.User, "password": g.Password, "database": g.Database}
	for k, vs := range want {
		ok := false
		for _, v := range vs {
			if fields[k] == v {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// changeVerdict: right when Doctor's changes are exactly the needed ones,
// partial when some are missing and none is extra, wrong otherwise.
func changeVerdict(want []map[string][]string, got []Change) string {
	for _, g := range got {
		ok := false
		for _, w := range want {
			if oneMatches(w, g) {
				ok = true
			}
		}
		if !ok {
			return "wrong"
		}
	}
	found := 0
	for _, w := range want {
		for _, g := range got {
			if oneMatches(w, g) {
				found++
				break
			}
		}
	}
	if found == len(want) {
		return "right"
	}
	return "partial"
}

// unsafe is a wrong answer that would do harm: a change the case doesn't call
// for, or a failed setup read as passing, or the other way round.
func unsafe(c corpusCase, d Diagnosis) bool {
	if (c.Expect.Outcome == "passed") != (d.Outcome == Passed) {
		return true
	}
	return d.Outcome != Passed && changeVerdict(c.needed(), d.Changes) == "wrong"
}

// TestCorpus runs Doctor on every recorded case: the tuning corpus and the
// holdout corpus recorded after the rules were frozen. Any wrong answer on the
// tuning corpus fails the test. On the holdout, whose score is the honest
// measure, only an unsafe one does: its other misses are logged as measured,
// so a later fix shows up here without the holdout being tuned in silence.
// "partial" and "unknown" are allowed, and counted.
func TestCorpus(t *testing.T) {
	for _, set := range []string{"corpus", "holdout"} {
		root := filepath.Join("..", "..", "testdata", "doctor", set)
		dirs, _ := filepath.Glob(filepath.Join(root, "*", "case.json"))
		if len(dirs) == 0 {
			t.Logf("%s: no cases", set)
			continue
		}
		sort.Strings(dirs)
		counts := map[string]int{}
		for _, p := range dirs {
			dir := filepath.Dir(p)
			var c corpusCase
			b, err := os.ReadFile(p)
			if err != nil || json.Unmarshal(b, &c) != nil {
				t.Fatalf("%s: can't read case.json", dir)
			}
			var s *spec.Spec
			if src, err := os.ReadFile(filepath.Join(dir, "spec.yaml")); err == nil {
				s, _ = spec.Load(kb.SpecPath, string(src))
			}
			for _, form := range []string{"log.txt", "out.txt", "build.txt", "err.txt"} {
				text, err := os.ReadFile(filepath.Join(dir, form))
				if err != nil || (form == "err.txt" && c.Expect.Category != CatSpec) || len(text) == 0 {
					continue
				}
				if form == "err.txt" && !strings.Contains(string(text), ": error S") {
					continue
				}
				d := Diagnose(string(text), s)
				v, why := verdict(c, d)
				counts[form+" "+v]++
				switch {
				case v != "wrong":
				case set == "corpus" || unsafe(c, d):
					t.Errorf("%s %s/%s: %s", set, c.ID, form, why)
				default:
					t.Logf("%s %s/%s: wrong, as measured when the holdout was scored: %s", set, c.ID, form, why)
				}
			}
		}
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t.Logf("%s: %s %d", set, k, counts[k])
		}
	}
}
