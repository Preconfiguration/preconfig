// Package yaml is a small, strict YAML reader. It reads the block and flow
// styles that configuration files use (maps, lists, plain, quoted and block
// scalars, anchors and aliases) into a tree that keeps key order and line
// numbers. Anything it does not understand is an error with a line number,
// never a silent guess.
package yaml

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"preconfiguration.com/preconfig/internal/tree"
)

// Error is a parse error at a place in the file.
type Error struct {
	Line int
	Col  int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line == 0 {
		return e.Msg
	}
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// Options change how strict the reader is.
type Options struct {
	// AllowDuplicateKeys keeps the last value instead of failing.
	AllowDuplicateKeys bool
}

type line struct {
	num    int
	indent int
	text   string // after the indentation
	raw    string // the whole line, without the line break
}

type parser struct {
	lines   []line
	i       int
	anchors map[string]*tree.Node
	opts    Options
	depth   int
}

const maxDepth = 200

// Parse reads one YAML document. An empty document gives a null scalar.
func Parse(src string) (*tree.Node, error) {
	return ParseWithOptions(src, Options{})
}

// ParseWithOptions reads one YAML document with the given options.
func ParseWithOptions(src string, opts Options) (n *tree.Node, err error) {
	if !utf8.ValidString(src) {
		return nil, &Error{Msg: "the file is not valid UTF-8"}
	}
	src = strings.TrimPrefix(src, "\ufeff")
	p := &parser{anchors: map[string]*tree.Node{}, opts: opts}
	if err := p.split(src); err != nil {
		return nil, err
	}
	p.skipPrologue()
	p.skipBlank()
	if p.eof() || p.isDocEnd() {
		return &tree.Node{Kind: tree.Scalar, Type: tree.Null, Line: 1, Col: 1}, nil
	}
	n, err = p.parseBlock(-1)
	if err != nil {
		return nil, err
	}
	p.skipBlank()
	if !p.eof() {
		ln := p.lines[p.i]
		if ln.indent == 0 && (ln.text == "..." || strings.HasPrefix(ln.text, "...")) {
			return n, nil
		}
		if ln.indent == 0 && (ln.text == "---" || strings.HasPrefix(ln.text, "--- ")) {
			return nil, &Error{Line: ln.num, Col: 1, Msg: "a second document starts here; only one document per file is supported"}
		}
		return nil, &Error{Line: ln.num, Col: ln.indent + 1, Msg: "unexpected content after the end of the document (check the indentation)"}
	}
	return n, nil
}

func (p *parser) split(src string) error {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	raw := strings.Split(src, "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	p.lines = make([]line, len(raw))
	for i, r := range raw {
		r = strings.TrimSuffix(r, "\r")
		ind := 0
		for ind < len(r) && r[ind] == ' ' {
			ind++
		}
		text := r[ind:]
		if strings.HasPrefix(text, "\t") && strings.TrimSpace(text) != "" && !strings.HasPrefix(strings.TrimLeft(text, " \t"), "#") {
			// A tab in the indentation of a line with content.
			p.lines = p.lines[:i]
			return &Error{Line: i + 1, Col: ind + 1, Msg: "a tab is used for indentation; YAML allows only spaces"}
		}
		p.lines[i] = line{num: i + 1, indent: ind, text: strings.TrimRight(text, " \t"), raw: r}
	}
	return nil
}

func (p *parser) eof() bool { return p.i >= len(p.lines) }

func (p *parser) isDocEnd() bool {
	if p.eof() {
		return true
	}
	ln := p.lines[p.i]
	return ln.indent == 0 && (ln.text == "..." || strings.HasPrefix(ln.text, "... "))
}

// skipPrologue skips directives and a leading document start marker.
func (p *parser) skipPrologue() {
	for !p.eof() {
		ln := p.lines[p.i]
		if ln.indent == 0 && strings.HasPrefix(ln.text, "%") {
			p.i++
			continue
		}
		if isBlankText(ln.text) {
			p.i++
			continue
		}
		break
	}
	if !p.eof() {
		ln := &p.lines[p.i]
		if ln.indent == 0 && (ln.text == "---" || strings.HasPrefix(ln.text, "--- ") || strings.HasPrefix(ln.text, "---\t")) {
			rest := strings.TrimSpace(strings.TrimPrefix(ln.text, "---"))
			if rest == "" || strings.HasPrefix(rest, "#") {
				p.i++
			} else {
				// "--- value" on the marker line: keep the value.
				ln.text = rest
				ln.indent = 4
			}
		}
	}
}

func isBlankText(t string) bool {
	return t == "" || strings.HasPrefix(t, "#")
}

func (p *parser) skipBlank() {
	for !p.eof() && isBlankText(p.lines[p.i].text) {
		p.i++
	}
}

func (p *parser) errf(ln line, col int, format string, args ...any) error {
	return &Error{Line: ln.num, Col: col, Msg: fmt.Sprintf(format, args...)}
}

func nullAt(ln line, col int) *tree.Node {
	return &tree.Node{Kind: tree.Scalar, Type: tree.Null, Line: ln.num, Col: col}
}

func isSeqEntry(t string) bool {
	return t == "-" || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "-\t")
}

// parseBlock reads the node that starts on the current line, which must be
// indented more than parent.
func (p *parser) parseBlock(parent int) (*tree.Node, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		ln := p.lines[min(p.i, len(p.lines)-1)]
		return nil, p.errf(ln, 1, "the document is nested too deeply")
	}
	p.skipBlank()
	if p.eof() || p.isDocEnd() {
		last := line{num: len(p.lines)}
		return nullAt(last, 1), nil
	}
	ln := p.lines[p.i]
	if ln.indent <= parent {
		return nullAt(ln, ln.indent+1), nil
	}
	if isSeqEntry(ln.text) {
		return p.parseSeq(ln.indent)
	}
	if strings.HasPrefix(ln.text, "? ") || ln.text == "?" {
		return nil, p.errf(ln, ln.indent+1, "complex keys (\"? \") are not supported")
	}
	if _, _, ok := splitKey(ln.text); ok {
		return p.parseMap(ln.indent)
	}
	// A scalar or flow collection standing on its own line.
	p.i++
	return p.parseInlineValue(ln, ln.text, ln.indent+1, parent, false)
}

// splitKey finds a mapping key at the start of text. It returns the key text,
// the rest after the colon, and whether text is a "key: value" line.
func splitKey(t string) (key string, rest string, ok bool) {
	if t == "" || isSeqEntry(t) || t[0] == '#' || t[0] == '[' || t[0] == '{' || t[0] == '|' || t[0] == '>' || t[0] == '*' && !strings.Contains(t, ":") {
		return "", "", false
	}
	if t[0] == '"' || t[0] == '\'' {
		end := quotedEnd(t)
		if end < 0 {
			return "", "", false
		}
		after := strings.TrimLeft(t[end+1:], " ")
		if strings.HasPrefix(after, ":") && (len(after) == 1 || after[1] == ' ' || after[1] == '\t') {
			return t[:end+1], strings.TrimLeft(after[1:], " \t"), true
		}
		return "", "", false
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c == '#' && i > 0 && (t[i-1] == ' ' || t[i-1] == '\t') {
			return "", "", false
		}
		if c == ':' && (i+1 == len(t) || t[i+1] == ' ' || t[i+1] == '\t') {
			k := strings.TrimRight(t[:i], " \t")
			if k == "" {
				return "", "", false
			}
			return k, strings.TrimLeft(t[i+1:], " \t"), true
		}
	}
	return "", "", false
}

// quotedEnd returns the index of the closing quote of a quoted scalar that
// starts at t[0] and ends on the same line, or -1.
func quotedEnd(t string) int {
	q := t[0]
	for i := 1; i < len(t); i++ {
		if q == '\'' && t[i] == '\'' {
			if i+1 < len(t) && t[i+1] == '\'' {
				i++
				continue
			}
			return i
		}
		if q == '"' {
			if t[i] == '\\' {
				i++
				continue
			}
			if t[i] == '"' {
				return i
			}
		}
	}
	return -1
}

func (p *parser) parseMap(indent int) (*tree.Node, error) {
	first := p.lines[p.i]
	m := &tree.Node{Kind: tree.Map, Line: first.num, Col: indent + 1}
	seen := map[string]int{}
	for {
		p.skipBlank()
		if p.eof() || p.isDocEnd() {
			break
		}
		ln := p.lines[p.i]
		if ln.indent < indent {
			break
		}
		if ln.indent == 0 && (ln.text == "---" || strings.HasPrefix(ln.text, "--- ")) {
			break
		}
		if ln.indent > indent {
			return nil, p.errf(ln, ln.indent+1, "unexpected indentation: this line is indented more than the keys above it")
		}
		if isSeqEntry(ln.text) {
			return nil, p.errf(ln, ln.indent+1, "a list item (\"- \") where a key was expected; check the indentation")
		}
		keyText, rest, ok := splitKey(ln.text)
		if !ok {
			return nil, p.errf(ln, ln.indent+1, "expected \"key: value\" here")
		}
		key, err := p.keyNode(ln, keyText, indent+1)
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[key.Value]; dup {
			if !p.opts.AllowDuplicateKeys {
				return nil, p.errf(ln, indent+1, "the key %q appears twice (first on line %d)", key.Value, prev)
			}
			for j, k := range m.Keys {
				if k.Value == key.Value {
					m.Keys = append(m.Keys[:j], m.Keys[j+1:]...)
					m.Values = append(m.Values[:j], m.Values[j+1:]...)
					break
				}
			}
		}
		seen[key.Value] = ln.num
		p.i++
		valCol := ln.indent + len(ln.text) - len(rest) + 1
		val, err := p.parseValue(ln, rest, valCol, indent, true)
		if err != nil {
			return nil, err
		}
		m.Keys = append(m.Keys, key)
		m.Values = append(m.Values, val)
	}
	return m, nil
}

func (p *parser) keyNode(ln line, keyText string, col int) (*tree.Node, error) {
	k := &tree.Node{Kind: tree.Scalar, Type: tree.String, Line: ln.num, Col: col}
	switch keyText[0] {
	case '"':
		s, err := unescapeDouble(keyText[1:len(keyText)-1], ln)
		if err != nil {
			return nil, err
		}
		k.Value, k.Style = s, tree.Double
	case '\'':
		k.Value, k.Style = strings.ReplaceAll(keyText[1:len(keyText)-1], "''", "'"), tree.Single
	case '&', '!', '*':
		return nil, p.errf(ln, col, "anchors, aliases and tags on keys are not supported")
	default:
		k.Value = keyText
	}
	return k, nil
}

func (p *parser) parseSeq(indent int) (*tree.Node, error) {
	first := p.lines[p.i]
	s := &tree.Node{Kind: tree.Seq, Line: first.num, Col: indent + 1}
	for {
		p.skipBlank()
		if p.eof() || p.isDocEnd() {
			break
		}
		ln := &p.lines[p.i]
		if ln.indent < indent {
			break
		}
		if ln.indent > indent {
			return nil, p.errf(*ln, ln.indent+1, "unexpected indentation inside a list")
		}
		if !isSeqEntry(ln.text) {
			break
		}
		after := ln.text[1:]
		sp := 0
		for sp < len(after) && (after[sp] == ' ' || after[sp] == '\t') {
			sp++
		}
		content := after[sp:]
		if content == "" || strings.HasPrefix(content, "#") {
			cur := *ln
			p.i++
			item, err := p.parseBlock(indent)
			if err != nil {
				return nil, err
			}
			if item.Line == 0 {
				item.Line, item.Col = cur.num, indent+2
			}
			s.Items = append(s.Items, item)
			continue
		}
		itemIndent := indent + 1 + sp
		if _, _, isKey := splitKey(content); isKey || isSeqEntry(content) {
			// A compact nested collection: "- key: value" or "- - item".
			ln.indent = itemIndent
			ln.text = content
			item, err := p.parseBlock(indent)
			if err != nil {
				return nil, err
			}
			s.Items = append(s.Items, item)
			continue
		}
		cur := *ln
		p.i++
		item, err := p.parseValue(cur, content, itemIndent+1, indent, false)
		if err != nil {
			return nil, err
		}
		s.Items = append(s.Items, item)
	}
	return s, nil
}

// parseValue reads the value that follows "key:" or "- " on line ln. rest is
// the text after the indicator, col its column, and indent the indentation of
// the key or the dash.
func (p *parser) parseValue(ln line, rest string, col, indent int, inMap bool) (*tree.Node, error) {
	anchor, tag := "", ""
	for {
		if strings.HasPrefix(rest, "&") {
			name, r := splitProp(rest[1:])
			if name == "" {
				return nil, p.errf(ln, col, "an anchor (&) needs a name")
			}
			anchor, rest = name, r
			continue
		}
		if strings.HasPrefix(rest, "!") {
			name, r := splitProp(rest[1:])
			tag, rest = "!"+name, r
			continue
		}
		break
	}
	if strings.HasPrefix(rest, "*") {
		name, r := splitProp(rest[1:])
		if r != "" && !strings.HasPrefix(r, "#") {
			return nil, p.errf(ln, col, "unexpected text after the alias *%s", name)
		}
		target, ok := p.anchors[name]
		if !ok {
			return nil, p.errf(ln, col, "the alias *%s refers to an anchor that is not defined above it", name)
		}
		return target, nil
	}
	var n *tree.Node
	var err error
	if rest == "" || strings.HasPrefix(rest, "#") {
		p.skipBlank()
		switch {
		case p.eof() || p.isDocEnd():
			n = nullAt(ln, col)
		case p.lines[p.i].indent > indent:
			n, err = p.parseBlock(indent)
		case inMap && p.lines[p.i].indent == indent && isSeqEntry(p.lines[p.i].text):
			n, err = p.parseSeq(indent)
		default:
			n = nullAt(ln, col)
		}
	} else {
		n, err = p.parseInlineValue(ln, rest, col, indent, true)
	}
	if err != nil {
		return nil, err
	}
	if tag != "" && n.Kind == tree.Scalar {
		switch tag {
		case "!!str", "!str":
			n.Type = tree.String
		case "!!int", "!!float":
			n.Type = tree.Number
		case "!!bool":
			n.Type = tree.Bool
		case "!!null":
			n.Type = tree.Null
		}
	}
	if anchor != "" {
		n.Anchor = anchor
		p.anchors[anchor] = n
	}
	return n, nil
}

func splitProp(s string) (name, rest string) {
	i := 0
	for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != ',' && s[i] != ']' && s[i] != '}' {
		i++
	}
	return s[:i], strings.TrimLeft(s[i:], " \t")
}

// parseInlineValue reads a value written on the current line (and, for block
// scalars, quoted scalars, flow collections and plain scalars, possibly the
// lines after it).
func (p *parser) parseInlineValue(ln line, rest string, col, indent int, allowContinuation bool) (*tree.Node, error) {
	switch rest[0] {
	case '|', '>':
		return p.parseBlockScalar(ln, rest, col, indent)
	case '[', '{':
		return p.parseFlowValue(ln, col)
	case '"', '\'':
		return p.parseQuoted(ln, col, indent)
	case '@', '`':
		return nil, p.errf(ln, col, "a value cannot start with %q; put it in quotes", rest[0])
	case '%':
		if col == 1 {
			return nil, p.errf(ln, col, "unexpected directive")
		}
	}
	text := stripComment(rest)
	if strings.Contains(text, ": ") || strings.HasSuffix(text, ":") {
		return nil, p.errf(ln, col, "a value contains \": \"; put the value in quotes")
	}
	start := ln
	// Plain scalars may continue on more indented lines.
	if allowContinuation {
		var parts []string
		parts = append(parts, text)
		blankRun := 0
		for j := p.i; j < len(p.lines); j++ {
			nx := p.lines[j]
			if nx.text == "" {
				blankRun++
				continue
			}
			if strings.HasPrefix(nx.text, "#") || nx.indent <= indent {
				break
			}
			if _, _, isKey := splitKey(nx.text); isKey {
				return nil, p.errf(nx, nx.indent+1, "unexpected \"key: value\" inside a text value; check the indentation")
			}
			for ; blankRun > 0; blankRun-- {
				parts = append(parts, "\n")
			}
			parts = append(parts, stripComment(nx.text))
			p.i = j + 1
		}
		if len(parts) > 1 {
			var b strings.Builder
			for k, s := range parts {
				if s == "\n" {
					b.WriteString("\n")
					continue
				}
				if k > 0 && parts[k-1] != "\n" {
					b.WriteByte(' ')
				}
				b.WriteString(s)
			}
			text = b.String()
		}
	}
	return plainScalar(text, start.num, col), nil
}

// stripComment removes a trailing " # comment" from plain text.
func stripComment(t string) string {
	for i := 0; i < len(t); i++ {
		if t[i] == '#' && i > 0 && (t[i-1] == ' ' || t[i-1] == '\t') {
			return strings.TrimRight(t[:i], " \t")
		}
	}
	return strings.TrimRight(t, " \t")
}

func plainScalar(text string, lineNum, col int) *tree.Node {
	n := &tree.Node{Kind: tree.Scalar, Value: text, Line: lineNum, Col: col, Style: tree.Plain}
	n.Type = resolvePlain(text)
	if n.Type == tree.Null {
		n.Value = ""
	}
	return n
}

// resolvePlain applies the YAML 1.2 core schema to a plain scalar.
func resolvePlain(s string) tree.Type {
	switch s {
	case "", "~", "null", "Null", "NULL":
		return tree.Null
	case "true", "True", "TRUE", "false", "False", "FALSE":
		return tree.Bool
	case ".inf", ".Inf", ".INF", "-.inf", "-.Inf", "-.INF", "+.inf", ".nan", ".NaN", ".NAN":
		return tree.Number
	}
	if isNumber(s) {
		return tree.Number
	}
	return tree.String
}

func isNumber(s string) bool {
	if strings.HasPrefix(s, "0x") {
		_, err := strconv.ParseInt(s[2:], 16, 64)
		return err == nil
	}
	if strings.HasPrefix(s, "0o") {
		_, err := strconv.ParseInt(s[2:], 8, 64)
		return err == nil
	}
	t := strings.TrimLeft(s, "+-")
	if t == "" || strings.ContainsAny(t, "_ ") {
		return false
	}
	if t[0] != '.' && (t[0] < '0' || t[0] > '9') {
		return false
	}
	_, err := strconv.ParseFloat(t, 64)
	return err == nil
}

func (p *parser) parseBlockScalar(ln line, header string, col, indent int) (*tree.Node, error) {
	kind := header[0]
	chomp := byte(0)
	explicit := 0
	h := stripComment(header[1:])
	for _, c := range []byte(h) {
		switch {
		case c == '+' || c == '-':
			if chomp != 0 {
				return nil, p.errf(ln, col, "a block scalar header has two chomping indicators")
			}
			chomp = c
		case c >= '1' && c <= '9':
			if explicit != 0 {
				return nil, p.errf(ln, col, "a block scalar header has two indentation indicators")
			}
			explicit = int(c - '0')
		default:
			return nil, p.errf(ln, col, "unexpected text after the block scalar indicator %q", string(kind))
		}
	}
	contentIndent := -1
	if explicit > 0 {
		contentIndent = max(indent, 0) + explicit
		if indent < 0 {
			contentIndent = explicit
		}
	}
	var body []string
	j := p.i
	for ; j < len(p.lines); j++ {
		nx := p.lines[j]
		if strings.TrimSpace(nx.raw) == "" {
			body = append(body, "")
			continue
		}
		if contentIndent < 0 {
			if nx.indent <= indent {
				break
			}
			contentIndent = nx.indent
		}
		if leadingSpaces(nx.raw) < contentIndent {
			break
		}
		body = append(body, nx.raw[contentIndent:])
	}
	// Trailing blank lines belong to the scalar only with "keep" chomping; the
	// parser moves past them either way.
	p.i = j
	// Separate trailing empty lines.
	end := len(body)
	for end > 0 && body[end-1] == "" {
		end--
	}
	content, trailing := body[:end], len(body)-end
	var text string
	if kind == '|' {
		text = strings.Join(content, "\n")
	} else {
		text = fold(content)
	}
	switch chomp {
	case '-':
	case '+':
		if len(content) > 0 {
			text += "\n"
		}
		text += strings.Repeat("\n", trailing)
	default:
		if len(content) > 0 {
			text += "\n"
		}
	}
	style := tree.Literal
	if kind == '>' {
		style = tree.Folded
	}
	return &tree.Node{Kind: tree.Scalar, Type: tree.String, Value: text, Style: style, Line: ln.num, Col: col}, nil
}

func leadingSpaces(s string) int {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i
}

// fold joins the lines of a folded block scalar.
func fold(lines []string) string {
	var b strings.Builder
	prevMore := false
	for i, l := range lines {
		more := strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")
		if i > 0 {
			switch {
			case l == "":
				b.WriteByte('\n')
				continue
			case lines[i-1] == "":
				if more || prevMore {
					b.WriteByte('\n')
				}
			case more || prevMore:
				b.WriteByte('\n')
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteString(l)
		prevMore = more
	}
	return b.String()
}

// parseQuoted reads a quoted scalar that starts at column col of line ln and may
// continue on the following lines.
func (p *parser) parseQuoted(ln line, col, indent int) (*tree.Node, error) {
	start := col - 1 - ln.indent // offset of the quote in ln.text
	if start < 0 || start >= len(ln.text) {
		start = strings.IndexAny(ln.text, "\"'")
	}
	q := ln.text[start]
	// Gather the text up to the closing quote across lines.
	var segs []string
	cur := ln.text[start+1:]
	lineIdx := p.i - 1 // the line of the opening quote
	for {
		end := -1
		for i := 0; i < len(cur); i++ {
			if q == '"' && cur[i] == '\\' {
				i++
				continue
			}
			if cur[i] == q {
				if q == '\'' && i+1 < len(cur) && cur[i+1] == '\'' {
					i++
					continue
				}
				end = i
				break
			}
		}
		if end >= 0 {
			segs = append(segs, cur[:end])
			after := strings.TrimSpace(cur[end+1:])
			if after != "" && !strings.HasPrefix(after, "#") {
				l := p.lines[lineIdx]
				if strings.HasPrefix(after, ":") {
					return nil, p.errf(l, col, "unexpected \":\" after a quoted value")
				}
				return nil, p.errf(l, col, "unexpected text after the closing quote")
			}
			break
		}
		segs = append(segs, cur)
		lineIdx++
		if lineIdx >= len(p.lines) {
			return nil, p.errf(ln, col, "a quoted value is never closed")
		}
		cur = strings.TrimLeft(p.lines[lineIdx].raw, " \t")
		p.i = lineIdx + 1
	}
	// Fold line breaks: a break between two lines becomes a space, an empty
	// line becomes a break, and in double quotes a backslash at the end of a
	// line joins the lines with nothing.
	var raw strings.Builder
	if len(segs) == 1 {
		raw.WriteString(segs[0])
	} else {
		space := false
		for k, s := range segs {
			last := k == len(segs)-1
			t := s
			if !last {
				t = strings.TrimRight(t, " \t")
			}
			if k > 0 && t == "" && !last {
				raw.WriteString("\n")
				space = false
				continue
			}
			if space {
				raw.WriteString(" ")
			}
			joined := false
			if !last && q == '"' && endsWithEscape(t) {
				t = t[:len(t)-1]
				joined = true
			}
			raw.WriteString(t)
			space = !joined
		}
	}
	n := &tree.Node{Kind: tree.Scalar, Type: tree.String, Line: ln.num, Col: col}
	if q == '\'' {
		n.Style = tree.Single
		n.Value = strings.ReplaceAll(raw.String(), "''", "'")
		return n, nil
	}
	n.Style = tree.Double
	v, err := unescapeDouble(raw.String(), ln)
	if err != nil {
		return nil, err
	}
	n.Value = v
	return n, nil
}

// endsWithEscape reports whether s ends with an odd number of backslashes.
func endsWithEscape(s string) bool {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

func unescapeDouble(s string, ln line) (string, error) {
	if !strings.Contains(s, "\\") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			return "", &Error{Line: ln.num, Msg: "a backslash ends a quoted value"}
		}
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't', '\t':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '0':
			b.WriteByte(0)
		case 'a':
			b.WriteByte(7)
		case 'b':
			b.WriteByte(8)
		case 'e':
			b.WriteByte(0x1b)
		case 'f':
			b.WriteByte(0x0c)
		case 'v':
			b.WriteByte(0x0b)
		case ' ', '"', '/', '\\':
			b.WriteByte(s[i])
		case 'x', 'u', 'U':
			width := 2
			if s[i] == 'u' {
				width = 4
			} else if s[i] == 'U' {
				width = 8
			}
			if i+width >= len(s) {
				return "", &Error{Line: ln.num, Msg: "a \\" + string(s[i]) + " escape is too short"}
			}
			hex := s[i+1 : i+1+width]
			v, err := strconv.ParseUint(hex, 16, 32)
			if err != nil {
				return "", &Error{Line: ln.num, Msg: "a \\" + string(s[i]) + " escape has an invalid code"}
			}
			b.WriteRune(rune(v))
			i += width
		case 'N':
			b.WriteString("\u0085")
		case '_':
			b.WriteString("\u00a0")
		case 'L':
			b.WriteString("\u2028")
		case 'P':
			b.WriteString("\u2029")
		default:
			return "", &Error{Line: ln.num, Msg: fmt.Sprintf("unknown escape \\%c in a double-quoted value", s[i])}
		}
	}
	return b.String(), nil
}

// ---- flow collections ----

type flowPos struct{ line, col int }

type flowReader struct {
	p    *parser
	text []byte
	pos  []flowPos
	i    int
}

// parseFlowValue gathers the text of a flow collection that starts on line ln
// at column col, possibly over several lines, and parses it.
func (p *parser) parseFlowValue(ln line, col int) (*tree.Node, error) {
	fr := &flowReader{p: p}
	// Start from the column of the bracket on the current line.
	startIdx := p.i - 1
	start := col - 1
	if start < 0 || start >= len(p.lines[startIdx].raw) {
		start = strings.IndexAny(p.lines[startIdx].raw, "[{")
	}
	depth := 0
	inQuote := byte(0)
	done := false
	j := startIdx
	for ; j < len(p.lines) && !done; j++ {
		raw := p.lines[j].raw
		from := 0
		if j == startIdx {
			from = start
		}
		for k := from; k < len(raw); k++ {
			c := raw[k]
			if inQuote == 0 && c == '#' && k > 0 && (raw[k-1] == ' ' || raw[k-1] == '\t') {
				break // comment to the end of the line
			}
			fr.text = append(fr.text, c)
			fr.pos = append(fr.pos, flowPos{p.lines[j].num, k + 1})
			switch {
			case inQuote != 0:
				if inQuote == '"' && c == '\\' && k+1 < len(raw) {
					k++
					fr.text = append(fr.text, raw[k])
					fr.pos = append(fr.pos, flowPos{p.lines[j].num, k + 1})
				} else if c == inQuote {
					if inQuote == '\'' && k+1 < len(raw) && raw[k+1] == '\'' {
						k++
						fr.text = append(fr.text, raw[k])
						fr.pos = append(fr.pos, flowPos{p.lines[j].num, k + 1})
					} else {
						inQuote = 0
					}
				}
			case c == '"' || c == '\'':
				inQuote = c
			case c == '[' || c == '{':
				depth++
			case c == ']' || c == '}':
				depth--
				if depth == 0 {
					rest := strings.TrimSpace(raw[k+1:])
					if rest != "" && !strings.HasPrefix(rest, "#") {
						return nil, &Error{Line: p.lines[j].num, Col: k + 2, Msg: "unexpected text after the closing bracket"}
					}
					done = true
				}
			}
			if done {
				break
			}
		}
		if !done {
			fr.text = append(fr.text, '\n')
			fr.pos = append(fr.pos, flowPos{p.lines[j].num, len(raw) + 1})
		}
	}
	if !done {
		return nil, p.errf(ln, col, "a [ or { is never closed")
	}
	p.i = j
	n, err := fr.node()
	if err != nil {
		return nil, err
	}
	fr.space()
	if fr.i < len(fr.text) {
		return nil, fr.err("unexpected text after the closing bracket")
	}
	return n, nil
}

func (fr *flowReader) err(msg string) error {
	if fr.i < len(fr.pos) {
		ps := fr.pos[fr.i]
		return &Error{Line: ps.line, Col: ps.col, Msg: msg}
	}
	if len(fr.pos) > 0 {
		ps := fr.pos[len(fr.pos)-1]
		return &Error{Line: ps.line, Col: ps.col, Msg: msg}
	}
	return &Error{Msg: msg}
}

func (fr *flowReader) here() flowPos {
	if fr.i < len(fr.pos) {
		return fr.pos[fr.i]
	}
	return fr.pos[len(fr.pos)-1]
}

func (fr *flowReader) space() {
	for fr.i < len(fr.text) {
		c := fr.text[fr.i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			fr.i++
			continue
		}
		break
	}
}

func (fr *flowReader) node() (*tree.Node, error) {
	fr.p.depth++
	defer func() { fr.p.depth-- }()
	if fr.p.depth > maxDepth {
		return nil, fr.err("the document is nested too deeply")
	}
	fr.space()
	if fr.i >= len(fr.text) {
		return nil, fr.err("a value is missing")
	}
	at := fr.here()
	anchor := ""
	if fr.text[fr.i] == '&' {
		j := fr.i + 1
		for j < len(fr.text) && !strings.ContainsRune(" \t\n,]}", rune(fr.text[j])) {
			j++
		}
		anchor = string(fr.text[fr.i+1 : j])
		fr.i = j
		fr.space()
		at = fr.here()
	}
	var n *tree.Node
	var err error
	switch fr.text[fr.i] {
	case '[':
		n, err = fr.seq()
	case '{':
		n, err = fr.mapping()
	case '"', '\'':
		n, err = fr.quoted()
	case '*':
		j := fr.i + 1
		for j < len(fr.text) && !strings.ContainsRune(" \t\n,]}", rune(fr.text[j])) {
			j++
		}
		name := string(fr.text[fr.i+1 : j])
		t, ok := fr.p.anchors[name]
		if !ok {
			return nil, fr.err("the alias *" + name + " refers to an anchor that is not defined above it")
		}
		fr.i = j
		return t, nil
	default:
		n, err = fr.plain()
	}
	if err != nil {
		return nil, err
	}
	if n.Line == 0 {
		n.Line, n.Col = at.line, at.col
	}
	if anchor != "" {
		n.Anchor = anchor
		fr.p.anchors[anchor] = n
	}
	return n, nil
}

func (fr *flowReader) seq() (*tree.Node, error) {
	at := fr.here()
	fr.i++ // [
	s := &tree.Node{Kind: tree.Seq, Line: at.line, Col: at.col}
	for {
		fr.space()
		if fr.i >= len(fr.text) {
			return nil, fr.err("a [ is never closed")
		}
		if fr.text[fr.i] == ']' {
			fr.i++
			return s, nil
		}
		item, err := fr.node()
		if err != nil {
			return nil, err
		}
		fr.space()
		// A single "key: value" pair inside a list.
		if fr.i < len(fr.text) && fr.text[fr.i] == ':' {
			fr.i++
			v, err := fr.node()
			if err != nil {
				return nil, err
			}
			pair := &tree.Node{Kind: tree.Map, Line: item.Line, Col: item.Col, Keys: []*tree.Node{item}, Values: []*tree.Node{v}}
			item = pair
			fr.space()
		}
		s.Items = append(s.Items, item)
		if fr.i >= len(fr.text) {
			return nil, fr.err("a [ is never closed")
		}
		switch fr.text[fr.i] {
		case ',':
			fr.i++
		case ']':
		default:
			return nil, fr.err("expected \",\" or \"]\" in a list")
		}
	}
}

func (fr *flowReader) mapping() (*tree.Node, error) {
	at := fr.here()
	fr.i++ // {
	m := &tree.Node{Kind: tree.Map, Line: at.line, Col: at.col}
	seen := map[string]bool{}
	for {
		fr.space()
		if fr.i >= len(fr.text) {
			return nil, fr.err("a { is never closed")
		}
		if fr.text[fr.i] == '}' {
			fr.i++
			return m, nil
		}
		k, err := fr.node()
		if err != nil {
			return nil, err
		}
		if k.Kind != tree.Scalar {
			return nil, fr.err("a key must be text")
		}
		if seen[k.Value] && !fr.p.opts.AllowDuplicateKeys {
			return nil, &Error{Line: k.Line, Col: k.Col, Msg: fmt.Sprintf("the key %q appears twice", k.Value)}
		}
		seen[k.Value] = true
		k.Type = tree.String
		fr.space()
		var v *tree.Node
		if fr.i < len(fr.text) && fr.text[fr.i] == ':' {
			fr.i++
			fr.space()
			if fr.i < len(fr.text) && (fr.text[fr.i] == ',' || fr.text[fr.i] == '}') {
				v = &tree.Node{Kind: tree.Scalar, Type: tree.Null, Line: k.Line, Col: k.Col}
			} else {
				v, err = fr.node()
				if err != nil {
					return nil, err
				}
			}
		} else {
			v = &tree.Node{Kind: tree.Scalar, Type: tree.Null, Line: k.Line, Col: k.Col}
		}
		m.Keys = append(m.Keys, k)
		m.Values = append(m.Values, v)
		fr.space()
		if fr.i >= len(fr.text) {
			return nil, fr.err("a { is never closed")
		}
		switch fr.text[fr.i] {
		case ',':
			fr.i++
		case '}':
		default:
			return nil, fr.err("expected \",\" or \"}\" in a mapping")
		}
	}
}

func (fr *flowReader) quoted() (*tree.Node, error) {
	at := fr.here()
	q := fr.text[fr.i]
	fr.i++
	var b strings.Builder
	for {
		if fr.i >= len(fr.text) {
			return nil, fr.err("a quoted value is never closed")
		}
		c := fr.text[fr.i]
		if q == '"' && c == '\\' && fr.i+1 < len(fr.text) {
			b.WriteByte(c)
			b.WriteByte(fr.text[fr.i+1])
			fr.i += 2
			continue
		}
		if c == q {
			if q == '\'' && fr.i+1 < len(fr.text) && fr.text[fr.i+1] == '\'' {
				b.WriteByte('\'')
				fr.i += 2
				continue
			}
			fr.i++
			break
		}
		if c == '\n' {
			c = ' '
		}
		b.WriteByte(c)
		fr.i++
	}
	n := &tree.Node{Kind: tree.Scalar, Type: tree.String, Line: at.line, Col: at.col}
	if q == '\'' {
		n.Style = tree.Single
		n.Value = b.String()
		return n, nil
	}
	n.Style = tree.Double
	v, err := unescapeDouble(b.String(), line{num: at.line})
	if err != nil {
		return nil, err
	}
	n.Value = v
	return n, nil
}

func (fr *flowReader) plain() (*tree.Node, error) {
	at := fr.here()
	start := fr.i
	for fr.i < len(fr.text) {
		c := fr.text[fr.i]
		if c == ',' || c == ']' || c == '}' || c == '[' || c == '{' || c == '\n' {
			break
		}
		if c == ':' && (fr.i+1 >= len(fr.text) || strings.ContainsRune(" \t\n,]}", rune(fr.text[fr.i+1]))) {
			break
		}
		fr.i++
	}
	text := strings.TrimSpace(string(fr.text[start:fr.i]))
	if text == "" && fr.i < len(fr.text) && (fr.text[fr.i] == '[' || fr.text[fr.i] == '{') {
		return nil, fr.err("unexpected bracket")
	}
	return plainScalar(text, at.line, at.col), nil
}
