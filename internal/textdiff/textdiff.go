// Package textdiff writes a unified diff of two small texts, line by line.
package textdiff

import (
	"fmt"
	"strings"
)

type op struct {
	kind byte // ' ', '-', '+'
	text string
	a, b int // line numbers (1-based) in the old and new text
}

// Unified returns a unified diff from old to new with the given number of
// context lines, or "" when the texts are equal.
func Unified(oldName, newName, oldText, newText string, context int) string {
	if oldText == newText {
		return ""
	}
	a := splitLines(oldText)
	b := splitLines(newText)
	ops := diff(a, b)
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", oldName, newName)
	// Group changes into hunks with context.
	i := 0
	for i < len(ops) {
		// Find the next change.
		for i < len(ops) && ops[i].kind == ' ' {
			i++
		}
		if i >= len(ops) {
			break
		}
		start := max(i-context, 0)
		end := i
		for {
			// Extend past this change.
			for end < len(ops) && ops[end].kind != ' ' {
				end++
			}
			// Is there another change within 2*context lines?
			next := end
			for next < len(ops) && ops[next].kind == ' ' && next-end < 2*context {
				next++
			}
			if next < len(ops) && ops[next].kind != ' ' {
				end = next
				continue
			}
			break
		}
		stop := min(end+context, len(ops))
		hunk := ops[start:stop]
		aStart, aLen, bStart, bLen := 0, 0, 0, 0
		for _, o := range hunk {
			if o.kind != '+' {
				if aStart == 0 {
					aStart = o.a
				}
				aLen++
			}
			if o.kind != '-' {
				if bStart == 0 {
					bStart = o.b
				}
				bLen++
			}
		}
		if aStart == 0 {
			aStart = hunkAnchor(ops, start, true)
		}
		if bStart == 0 {
			bStart = hunkAnchor(ops, start, false)
		}
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", rng(aStart, aLen), rng(bStart, bLen))
		for _, o := range hunk {
			out.WriteByte(o.kind)
			out.WriteString(o.text)
			out.WriteByte('\n')
		}
		i = stop
	}
	return out.String()
}

func hunkAnchor(ops []op, start int, old bool) int {
	// The line before the hunk in the text where the hunk adds or removes everything.
	for j := start - 1; j >= 0; j-- {
		if old && ops[j].kind != '+' {
			return ops[j].a
		}
		if !old && ops[j].kind != '-' {
			return ops[j].b
		}
	}
	return 0
}

func rng(start, n int) string {
	if n == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, n)
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// diff computes an edit script with a longest-common-subsequence table.
// Setup files are small, so the quadratic table is fine.
func diff(a, b []string) []op {
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i], i + 1, j + 1})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{'-', a[i], i + 1, 0})
			i++
		default:
			ops = append(ops, op{'+', b[j], 0, j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i], i + 1, 0})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j], 0, j + 1})
	}
	return ops
}

// Stat counts added and removed lines.
func Stat(oldText, newText string) (added, removed int) {
	for _, o := range diff(splitLines(oldText), splitLines(newText)) {
		switch o.kind {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return
}
