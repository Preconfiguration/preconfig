// Package jsonc reads JSON, optionally with comments and trailing commas (the
// "JSON with comments" that devcontainer.json uses), into the shared tree
// model with line numbers.
package jsonc

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"preconfiguration.com/preconfig/internal/tree"
)

// Options say which extensions to plain JSON are allowed.
type Options struct {
	Comments       bool
	TrailingCommas bool
}

// JSONC is what devcontainer.json allows.
var JSONC = Options{Comments: true, TrailingCommas: true}

// Error is a parse error at a place in the file.
type Error struct {
	Line, Col int
	Msg       string
}

func (e *Error) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

type reader struct {
	s     string
	i     int
	line  int
	col   int
	opts  Options
	depth int
}

// Parse reads one JSON value and fails if anything but whitespace or comments
// follows it.
func Parse(src string, opts Options) (*tree.Node, error) {
	if !utf8.ValidString(src) {
		return nil, &Error{Line: 1, Col: 1, Msg: "the file is not valid UTF-8"}
	}
	r := &reader{s: strings.TrimPrefix(src, "\ufeff"), line: 1, col: 1, opts: opts}
	if err := r.skip(); err != nil {
		return nil, err
	}
	if r.i >= len(r.s) {
		return nil, r.errf("the file is empty")
	}
	n, err := r.value()
	if err != nil {
		return nil, err
	}
	if err := r.skip(); err != nil {
		return nil, err
	}
	if r.i < len(r.s) {
		return nil, r.errf("unexpected %q after the end of the JSON value; the file has extra text after its last closing bracket", r.s[r.i])
	}
	return n, nil
}

func (r *reader) errf(format string, args ...any) error {
	return &Error{Line: r.line, Col: r.col, Msg: fmt.Sprintf(format, args...)}
}

func (r *reader) advance(n int) {
	for k := 0; k < n && r.i < len(r.s); k++ {
		if r.s[r.i] == '\n' {
			r.line++
			r.col = 1
		} else {
			r.col++
		}
		r.i++
	}
}

// skip moves past whitespace and, when allowed, comments.
func (r *reader) skip() error {
	for r.i < len(r.s) {
		c := r.s[r.i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			r.advance(1)
		case c == '/' && r.i+1 < len(r.s) && (r.s[r.i+1] == '/' || r.s[r.i+1] == '*'):
			if !r.opts.Comments {
				return r.errf("comments are not allowed in this file")
			}
			if r.s[r.i+1] == '/' {
				for r.i < len(r.s) && r.s[r.i] != '\n' {
					r.advance(1)
				}
			} else {
				start := *r
				r.advance(2)
				for {
					if r.i+1 >= len(r.s) {
						return start.errf("a /* comment is never closed")
					}
					if r.s[r.i] == '*' && r.s[r.i+1] == '/' {
						r.advance(2)
						break
					}
					r.advance(1)
				}
			}
		default:
			return nil
		}
	}
	return nil
}

func (r *reader) value() (*tree.Node, error) {
	r.depth++
	defer func() { r.depth-- }()
	if r.depth > 200 {
		return nil, r.errf("the document is nested too deeply")
	}
	if r.i >= len(r.s) {
		return nil, r.errf("a value is missing at the end of the file")
	}
	line, col := r.line, r.col
	switch c := r.s[r.i]; {
	case c == '{':
		return r.object()
	case c == '[':
		return r.array()
	case c == '"':
		s, err := r.str()
		if err != nil {
			return nil, err
		}
		return &tree.Node{Kind: tree.Scalar, Type: tree.String, Style: tree.Double, Value: s, Line: line, Col: col}, nil
	case c == 't' && strings.HasPrefix(r.s[r.i:], "true"):
		r.advance(4)
		return &tree.Node{Kind: tree.Scalar, Type: tree.Bool, Value: "true", Line: line, Col: col}, nil
	case c == 'f' && strings.HasPrefix(r.s[r.i:], "false"):
		r.advance(5)
		return &tree.Node{Kind: tree.Scalar, Type: tree.Bool, Value: "false", Line: line, Col: col}, nil
	case c == 'n' && strings.HasPrefix(r.s[r.i:], "null"):
		r.advance(4)
		return &tree.Node{Kind: tree.Scalar, Type: tree.Null, Line: line, Col: col}, nil
	case c == '-' || (c >= '0' && c <= '9'):
		return r.number()
	case c == '\'':
		return nil, r.errf("strings must use double quotes, not single quotes")
	}
	return nil, r.errf("unexpected %q where a value was expected", r.s[r.i])
}

func (r *reader) object() (*tree.Node, error) {
	n := &tree.Node{Kind: tree.Map, Line: r.line, Col: r.col}
	r.advance(1)
	seen := map[string]int{}
	for {
		if err := r.skip(); err != nil {
			return nil, err
		}
		if r.i >= len(r.s) {
			return nil, &Error{Line: n.Line, Col: n.Col, Msg: "a { is never closed"}
		}
		if r.s[r.i] == '}' {
			r.advance(1)
			return n, nil
		}
		if r.s[r.i] != '"' {
			return nil, r.errf("expected a key in double quotes")
		}
		kl, kc := r.line, r.col
		k, err := r.str()
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[k]; dup {
			return nil, &Error{Line: kl, Col: kc, Msg: fmt.Sprintf("the key %q appears twice (first on line %d)", k, prev)}
		}
		seen[k] = kl
		if err := r.skip(); err != nil {
			return nil, err
		}
		if r.i >= len(r.s) || r.s[r.i] != ':' {
			return nil, r.errf("expected \":\" after the key %q", k)
		}
		r.advance(1)
		if err := r.skip(); err != nil {
			return nil, err
		}
		v, err := r.value()
		if err != nil {
			return nil, err
		}
		n.Keys = append(n.Keys, &tree.Node{Kind: tree.Scalar, Type: tree.String, Value: k, Line: kl, Col: kc})
		n.Values = append(n.Values, v)
		if err := r.skip(); err != nil {
			return nil, err
		}
		if r.i >= len(r.s) {
			return nil, &Error{Line: n.Line, Col: n.Col, Msg: "a { is never closed"}
		}
		switch r.s[r.i] {
		case ',':
			cl, cc := r.line, r.col
			r.advance(1)
			if err := r.skip(); err != nil {
				return nil, err
			}
			if r.i < len(r.s) && r.s[r.i] == '}' && !r.opts.TrailingCommas {
				return nil, &Error{Line: cl, Col: cc, Msg: "a trailing comma before \"}\" is not allowed in this file"}
			}
		case '}':
		default:
			return nil, r.errf("expected \",\" or \"}\" after a value")
		}
	}
}

func (r *reader) array() (*tree.Node, error) {
	n := &tree.Node{Kind: tree.Seq, Line: r.line, Col: r.col}
	r.advance(1)
	for {
		if err := r.skip(); err != nil {
			return nil, err
		}
		if r.i >= len(r.s) {
			return nil, &Error{Line: n.Line, Col: n.Col, Msg: "a [ is never closed"}
		}
		if r.s[r.i] == ']' {
			r.advance(1)
			return n, nil
		}
		v, err := r.value()
		if err != nil {
			return nil, err
		}
		n.Items = append(n.Items, v)
		if err := r.skip(); err != nil {
			return nil, err
		}
		if r.i >= len(r.s) {
			return nil, &Error{Line: n.Line, Col: n.Col, Msg: "a [ is never closed"}
		}
		switch r.s[r.i] {
		case ',':
			cl, cc := r.line, r.col
			r.advance(1)
			if err := r.skip(); err != nil {
				return nil, err
			}
			if r.i < len(r.s) && r.s[r.i] == ']' && !r.opts.TrailingCommas {
				return nil, &Error{Line: cl, Col: cc, Msg: "a trailing comma before \"]\" is not allowed in this file"}
			}
		case ']':
		default:
			return nil, r.errf("expected \",\" or \"]\" after a value")
		}
	}
}

func (r *reader) str() (string, error) {
	startLine, startCol := r.line, r.col
	r.advance(1) // opening quote
	var b strings.Builder
	for {
		if r.i >= len(r.s) {
			return "", &Error{Line: startLine, Col: startCol, Msg: "a string is never closed"}
		}
		c := r.s[r.i]
		switch {
		case c == '"':
			r.advance(1)
			return b.String(), nil
		case c == '\n':
			return "", &Error{Line: startLine, Col: startCol, Msg: "a string runs past the end of its line"}
		case c < 0x20:
			return "", r.errf("a control character inside a string must be escaped")
		case c == '\\':
			if r.i+1 >= len(r.s) {
				return "", r.errf("a backslash ends the file")
			}
			e := r.s[r.i+1]
			switch e {
			case '"', '\\', '/':
				b.WriteByte(e)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				if r.i+6 > len(r.s) {
					return "", r.errf("a \\u escape is too short")
				}
				v, err := strconv.ParseUint(r.s[r.i+2:r.i+6], 16, 32)
				if err != nil {
					return "", r.errf("a \\u escape has an invalid code")
				}
				ru := rune(v)
				// A surrogate pair.
				if ru >= 0xD800 && ru < 0xDC00 && r.i+12 <= len(r.s) && r.s[r.i+6] == '\\' && r.s[r.i+7] == 'u' {
					if lo, err := strconv.ParseUint(r.s[r.i+8:r.i+12], 16, 32); err == nil && lo >= 0xDC00 && lo < 0xE000 {
						ru = (ru-0xD800)<<10 + (rune(lo) - 0xDC00) + 0x10000
						r.advance(6)
					}
				}
				b.WriteRune(ru)
				r.advance(4)
			default:
				return "", r.errf("unknown escape \\%c in a string", e)
			}
			r.advance(2)
		default:
			_, size := utf8.DecodeRuneInString(r.s[r.i:])
			b.WriteString(r.s[r.i : r.i+size])
			r.advance(size)
		}
	}
}

func (r *reader) number() (*tree.Node, error) {
	line, col := r.line, r.col
	start := r.i
	if r.s[r.i] == '-' {
		r.advance(1)
	}
	digits := func() int {
		k := 0
		for r.i < len(r.s) && r.s[r.i] >= '0' && r.s[r.i] <= '9' {
			r.advance(1)
			k++
		}
		return k
	}
	if r.i < len(r.s) && r.s[r.i] == '0' {
		r.advance(1)
	} else if digits() == 0 {
		return nil, r.errf("a number is expected")
	}
	if r.i < len(r.s) && r.s[r.i] == '.' {
		r.advance(1)
		if digits() == 0 {
			return nil, r.errf("a digit is expected after the decimal point")
		}
	}
	if r.i < len(r.s) && (r.s[r.i] == 'e' || r.s[r.i] == 'E') {
		r.advance(1)
		if r.i < len(r.s) && (r.s[r.i] == '+' || r.s[r.i] == '-') {
			r.advance(1)
		}
		if digits() == 0 {
			return nil, r.errf("a digit is expected in the exponent")
		}
	}
	return &tree.Node{Kind: tree.Scalar, Type: tree.Number, Value: r.s[start:r.i], Line: line, Col: col}, nil
}
