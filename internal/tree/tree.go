// Package tree holds the document model shared by the YAML and JSON readers:
// maps that keep their key order, sequences and scalars, each with the line and
// column where it starts, so that every message can point at a place in a file.
package tree

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Kind is the kind of a node.
type Kind int

const (
	Scalar Kind = iota
	Map
	Seq
)

func (k Kind) String() string {
	switch k {
	case Map:
		return "mapping"
	case Seq:
		return "list"
	}
	return "value"
}

// Type is the type of a scalar, as the file wrote it.
type Type int

const (
	String Type = iota
	Number
	Bool
	Null
)

// Style records how a YAML scalar was written. JSON scalars are Plain or Double.
type Style int

const (
	Plain Style = iota
	Single
	Double
	Literal
	Folded
)

// Node is one value in a document.
type Node struct {
	Kind Kind
	Line int // 1-based; 0 when unknown
	Col  int // 1-based

	// Scalars.
	Value string
	Type  Type
	Style Style

	// Maps keep their pairs in file order.
	Keys   []*Node
	Values []*Node

	// Sequences.
	Items []*Node

	Anchor string
}

// Get returns the value for key in a map, or nil.
func (n *Node) Get(key string) *Node {
	if n == nil || n.Kind != Map {
		return nil
	}
	for i, k := range n.Keys {
		if k.Value == key {
			return n.Values[i]
		}
	}
	return nil
}

// Key returns the key node for key in a map, or nil.
func (n *Node) Key(key string) *Node {
	if n == nil || n.Kind != Map {
		return nil
	}
	for _, k := range n.Keys {
		if k.Value == key {
			return k
		}
	}
	return nil
}

// Path follows keys through nested maps.
func (n *Node) Path(keys ...string) *Node {
	cur := n
	for _, k := range keys {
		cur = cur.Get(k)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// Str returns the text of a scalar, or "" for anything else.
func (n *Node) Str() string {
	if n == nil || n.Kind != Scalar || n.Type == Null {
		return ""
	}
	return n.Value
}

// IsNull reports whether n is missing or an explicit null.
func (n *Node) IsNull() bool {
	return n == nil || (n.Kind == Scalar && n.Type == Null)
}

// Strings returns the scalar items of a sequence (or a single scalar as a list of one).
func (n *Node) Strings() []string {
	if n == nil {
		return nil
	}
	if n.Kind == Scalar {
		if n.Type == Null {
			return nil
		}
		return []string{n.Value}
	}
	var out []string
	for _, it := range n.Items {
		if it.Kind == Scalar && it.Type != Null {
			out = append(out, it.Value)
		}
	}
	return out
}

// List returns the items of a sequence, or nil for anything else, so that a
// missing or mistyped value reads as an empty list.
func (n *Node) List() []*Node {
	if n == nil || n.Kind != Seq {
		return nil
	}
	return n.Items
}

// Describe gives a short human description of a node's kind for messages.
func (n *Node) Describe() string {
	if n == nil {
		return "nothing"
	}
	if n.Kind != Scalar {
		return "a " + n.Kind.String()
	}
	switch n.Type {
	case Null:
		return "an empty value"
	case Bool:
		return "true/false"
	case Number:
		return "a number"
	}
	return "text"
}

// NewMap builds an empty map, for writers.
func NewMap() *Node { return &Node{Kind: Map} }

// NewStr builds a string scalar.
func NewStr(s string) *Node { return &Node{Kind: Scalar, Value: s, Type: String} }

// NewBool builds a boolean scalar.
func NewBool(b bool) *Node {
	return &Node{Kind: Scalar, Value: strconv.FormatBool(b), Type: Bool}
}

// Set appends or replaces key in a map and returns the map for chaining.
func (n *Node) Set(key string, v *Node) *Node {
	for i, k := range n.Keys {
		if k.Value == key {
			n.Values[i] = v
			return n
		}
	}
	n.Keys = append(n.Keys, NewStr(key))
	n.Values = append(n.Values, v)
	return n
}

// JSON writes the node as indented JSON, keeping map order.
func JSON(n *Node, indent string) string {
	var b strings.Builder
	writeJSON(&b, n, indent, "")
	b.WriteByte('\n')
	return b.String()
}

func writeJSON(b *strings.Builder, n *Node, indent, pad string) {
	if n == nil {
		b.WriteString("null")
		return
	}
	switch n.Kind {
	case Map:
		if len(n.Keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		inner := pad + indent
		for i, k := range n.Keys {
			b.WriteString(inner)
			b.WriteString(Quote(k.Value))
			b.WriteString(": ")
			writeJSON(b, n.Values[i], indent, inner)
			if i < len(n.Keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(pad)
		b.WriteByte('}')
	case Seq:
		if len(n.Items) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		inner := pad + indent
		for i, it := range n.Items {
			b.WriteString(inner)
			writeJSON(b, it, indent, inner)
			if i < len(n.Items)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(pad)
		b.WriteByte(']')
	default:
		switch n.Type {
		case Null:
			b.WriteString("null")
		case Bool, Number:
			b.WriteString(n.Value)
		default:
			b.WriteString(Quote(n.Value))
		}
	}
}

// Quote returns s as a JSON string.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == 0x2028 || r == 0x2029 || r == 0xfeff || r == 0xfffe || r == 0xffff {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// YAMLString writes s as a YAML scalar that YAML 1.1 and 1.2 readers alike
// (PyYAML for cloud-init, GitHub's reader, Compose's) give back as the same
// string: plainly when that is safe, in double quotes otherwise.
func YAMLString(s string) string {
	if PlainSafe(s) {
		return s
	}
	return Quote(s)
}

var plainChars = regexp.MustCompile(`^[A-Za-z0-9_./@=+,:() -]+$`)

// PlainSafe reports whether s can be written without quotes. It allows only a
// value that starts with a letter, an underscore, a slash or a dot followed by
// a letter, a dot or a slash, which rules out every number and timestamp in
// either YAML version, and that isn't a word YAML reads as a boolean, a null,
// infinity or not-a-number.
func PlainSafe(s string) bool {
	if s == "" || strings.TrimSpace(s) != s || !plainChars.MatchString(s) {
		return false
	}
	c := s[0]
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_', c == '/':
	case c == '.' && len(s) > 1 && (s[1] == '.' || s[1] == '/' || s[1] >= 'A' && s[1] <= 'Z' || s[1] >= 'a' && s[1] <= 'z'):
	default:
		return false
	}
	if strings.Contains(s, ": ") || strings.HasSuffix(s, ":") || strings.Contains(s, " #") {
		return false
	}
	switch strings.ToLower(s) {
	case "true", "false", "null", "yes", "no", "on", "off", "y", "n", ".inf", ".nan":
		return false
	}
	return true
}

// Suggest returns the candidate closest to word, when it is close enough to be a
// likely typo, or "".
func Suggest(word string, candidates []string) string {
	best, bestD := "", 1<<30
	lw := strings.ToLower(word)
	for _, c := range candidates {
		d := distance(lw, strings.ToLower(c))
		if d < bestD {
			best, bestD = c, d
		}
	}
	limit := 2
	if len(word) >= 8 {
		limit = 3
	}
	if bestD <= limit && bestD < len(word) {
		return best
	}
	return ""
}

// distance is the Levenshtein edit distance.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
