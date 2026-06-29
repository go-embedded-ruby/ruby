// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	stdtime "time"

	gotime "github.com/go-composites/time/src"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// posInf, negInf and nan name the float specials the loader maps the YAML
// keywords .inf / -.inf / .nan onto.
func posInf() float64 { return math.Inf(1) }
func negInf() float64 { return math.Inf(-1) }
func nan() float64    { return math.NaN() }

// stdParseTime parses value with the Go reference layout, exposed so the
// timestamp grammar stays in one place.
func stdParseTime(layout, value string) (stdtime.Time, error) {
	return stdtime.Parse(layout, value)
}

// yamlResolveClass resolves a (possibly `::`-qualified) class name used in a
// `!ruby/object:Name` tag to its RClass, registering a fresh placeholder class
// (a plain subclass of Object) under the top-level name when the program has not
// defined it — so loading an object whose class is unknown still yields a typed
// instance rather than failing.
func (vm *VM) yamlResolveClass(name string) *RClass {
	parts := strings.Split(name, "::")
	cur := vm.cObject
	var resolved *RClass
	for i, p := range parts {
		v, ok := vm.resolveConst(cur, p)
		if !ok {
			resolved = nil
			break
		}
		c, isClass := v.(*RClass)
		if !isClass {
			resolved = nil
			break
		}
		resolved = c
		if i < len(parts)-1 {
			cur = c
		}
	}
	if resolved != nil {
		return resolved
	}
	// Unknown class: register a placeholder under its qualified name so repeated
	// loads reuse one class object.
	if v, ok := vm.cObject.consts[name]; ok {
		if c, isClass := v.(*RClass); isClass {
			return c
		}
	}
	c := newClass(name, vm.cObject)
	vm.cObject.consts[name] = c
	return c
}

// yamlLoader parses the Psych-compatible YAML subset that yamlEncoder emits — and
// that Puppet's local persistence (state.yaml / last_run_summary.yaml) round-trips
// — back into Ruby values. It covers block mappings and sequences, flow `[]` /
// `{}`, plain / single- / double-quoted / literal-block scalars, the Psych scalar
// keywords (null, booleans, integers, floats, ISO-8601 timestamps), Symbols
// (`:name`), and the `!ruby/symbol` / `!ruby/object:` tags with `&anchor` / `*alias`
// references, so YAML.load(YAML.dump(x)) round-trips the state and report graphs.
type yamlLoader struct {
	vm      *VM
	lines   []yamlLine // significant (non-blank, non-comment) input lines
	pos     int        // index of the next unconsumed line
	anchors map[string]object.Value
}

// yamlLine is one physical input line split into its leading indentation (count
// of spaces) and the remaining content (indentation stripped).
type yamlLine struct {
	indent  int
	content string
	raw     string // the full original line, used by literal block scalars
}

// yamlLoad parses a complete YAML document string into a Ruby value. A blank or
// marker-only document loads as nil (matching Psych's empty-document behaviour).
func yamlLoad(vm *VM, src string) object.Value {
	l := &yamlLoader{vm: vm, anchors: map[string]object.Value{}}
	l.tokenize(src)
	if len(l.lines) == 0 {
		return object.NilV
	}
	// A leading "---" document marker carries its value (or a tag) inline or on the
	// following lines; strip the marker and parse what follows.
	first := l.lines[0]
	if first.content == "---" || strings.HasPrefix(first.content, "--- ") {
		rest := ""
		if len(first.content) > 4 {
			rest = strings.TrimSpace(first.content[4:])
		}
		if rest == "" {
			// No inline value: either an empty document (nil) or a header whose body
			// follows on the next lines.
			l.pos = 1
			if l.peek() == nil {
				return object.NilV
			}
			return l.parseNode(0)
		}
		if style, chomp, ok := blockScalarTag(rest); ok {
			// A document-level literal/folded block scalar ("--- |-").
			l.pos = 1
			return l.parseBlockScalar(style, chomp, first.indent)
		}
		if tag, _, content := splitTagAnchor(rest); tag != "" && content == "" {
			// A document-level tag ("--- !ruby/object:Foo") whose body is the
			// following lines at the document indent (0) — Psych does not indent a
			// root object's mapping. Parse that body directly.
			l.pos = 1
			if l.peek() == nil {
				return l.taggedEmpty(tag)
			}
			if isSeqEntry(l.peek().content) {
				return l.parseSequence(l.peek().indent, tag)
			}
			return l.parseMapping(l.peek().indent, tag)
		}
		l.lines[0].content = rest
		l.lines[0].indent = first.indent
		// An inline value (scalar, flow collection, or an inline-tagged scalar like
		// "--- !ruby/regexp /x/") parses from this line as a node at indent 0.
		return l.parseNode(0)
	}
	return l.parseNode(0)
}

// tokenize splits src into significant lines, recording each line's indentation
// and content. Blank lines and whole-line comments are dropped; the document-end
// marker "..." is treated as end-of-input.
func (l *yamlLoader) tokenize(src string) {
	for _, raw := range strings.Split(src, "\n") {
		trimmed := strings.TrimRight(raw, "\r")
		content := strings.TrimLeft(trimmed, " ")
		if content == "" {
			continue
		}
		if content == "..." {
			break
		}
		if strings.HasPrefix(content, "#") {
			continue
		}
		indent := len(trimmed) - len(content)
		l.lines = append(l.lines, yamlLine{indent: indent, content: content, raw: trimmed})
	}
}

// blockScalarTag reports whether content is a literal/folded block-scalar
// indicator ("|", "|-", "|+", ">", ">-", ">+"), returning the style byte ('|' or
// '>') and the chomp indicator ('-' strip, '+' keep, 0 clip).
func blockScalarTag(content string) (style, chomp byte, ok bool) {
	if content == "" {
		return 0, 0, false
	}
	if content[0] != '|' && content[0] != '>' {
		return 0, 0, false
	}
	rest := content[1:]
	if rest == "" {
		return content[0], 0, true
	}
	if rest == "-" || rest == "+" {
		return content[0], rest[0], true
	}
	return 0, 0, false
}

// parseBlockScalar reads a literal/folded block scalar whose indicator is on the
// current "key:"/dash line: the body is the following lines indented deeper than
// parentIndent. It supports the literal style "|" (newlines preserved) and the
// folded style ">" (single newlines become spaces), with strip/clip/keep chomp.
func (l *yamlLoader) parseBlockScalar(style, chomp byte, parentIndent int) object.Value {
	var body []string
	bodyIndent := -1
	for l.pos < len(l.lines) {
		line := l.lines[l.pos]
		if line.indent <= parentIndent {
			break
		}
		if bodyIndent < 0 {
			bodyIndent = line.indent
		}
		// Strip the block's base indentation, clamping for a continuation line that
		// is less indented than the first (defensive: malformed but tolerated).
		cut := bodyIndent
		if cut > len(line.raw) {
			cut = len(line.raw)
		}
		body = append(body, line.raw[cut:])
		l.pos++
	}
	var s string
	if style == '>' {
		s = strings.Join(body, " ")
	} else {
		s = strings.Join(body, "\n")
	}
	switch chomp {
	case '-': // strip: no trailing newline
	case '+': // keep: a single trailing newline (the emitter writes one block line)
		s += "\n"
	default: // clip: exactly one trailing newline
		s += "\n"
	}
	return object.NewString(s)
}

// parseNode parses the node whose first line is l.lines[l.pos], expected at the
// given minimum indentation. It dispatches on the line's shape: a sequence entry
// ("- …"), a mapping entry ("key: …"), or a bare scalar/flow value.
func (l *yamlLoader) parseNode(minIndent int) object.Value {
	if l.pos >= len(l.lines) {
		return object.NilV
	}
	line := l.lines[l.pos]
	// A node may carry a tag and/or anchor as a prefix on its first line
	// ("!ruby/object:Foo", "&1 …"). Peel those before deciding the shape.
	tag, anchorName, content := splitTagAnchor(line.content)
	if content == "" {
		// The tag/anchor stood alone: the value is the indented block that follows.
		l.lines[l.pos].content = ""
		l.pos++
		v := l.parseBlock(line.indent, tag)
		l.bind(anchorName, v)
		return v
	}
	// Rewrite the working line to its content (tag/anchor stripped) and dispatch.
	l.lines[l.pos].content = content
	if isSeqEntry(content) {
		v := l.parseSequence(line.indent, tag)
		l.bind(anchorName, v)
		return v
	}
	// A flow collection ("[...]" / "{...}") or any other lone scalar is parsed by
	// scalarValue; only a genuine block-mapping line ("key: ...", not opening with
	// a flow bracket) is parsed as a mapping.
	if !isFlowStart(content) {
		if _, _, ok := splitMapEntry(content); ok {
			v := l.parseMapping(line.indent, tag)
			l.bind(anchorName, v)
			return v
		}
	}
	// A lone scalar (or flow collection) on this line.
	l.pos++
	v := l.scalarValue(content, tag)
	l.bind(anchorName, v)
	return v
}

// parseBlock parses the block that follows a standalone tag / anchor. The tag has
// already consumed its own line; its body is the following lines indented at
// least to parentIndent (Psych aligns an inline-tagged object's mapping under the
// position where the tag began, i.e. the same indent — not strictly deeper).
func (l *yamlLoader) parseBlock(parentIndent int, tag string) object.Value {
	if l.pos >= len(l.lines) || l.lines[l.pos].indent < parentIndent {
		// No body at this depth: an empty tagged node (e.g. "!ruby/object:Foo" with
		// no body) is an empty object/mapping.
		return l.taggedEmpty(tag)
	}
	child := l.lines[l.pos]
	if isSeqEntry(child.content) {
		return l.parseSequence(child.indent, tag)
	}
	return l.parseMapping(child.indent, tag)
}

// parseSequence parses a block sequence: consecutive "- …" lines at exactly
// indent. Each dash either carries an inline value or opens a nested block.
func (l *yamlLoader) parseSequence(indent int, tag string) object.Value {
	arr := &object.Array{}
	for l.pos < len(l.lines) {
		line := l.lines[l.pos]
		if line.indent != indent || !isSeqEntry(line.content) {
			break
		}
		rest := strings.TrimPrefix(line.content, "-")
		rest = strings.TrimPrefix(rest, " ")
		if rest == "" {
			// "-" alone: the element is the indented block on the following lines, or
			// nil when nothing deeper follows.
			l.pos++
			if l.peek() != nil && l.peek().indent > indent {
				arr.Elems = append(arr.Elems, l.parseBlock(indent, ""))
			} else {
				arr.Elems = append(arr.Elems, object.NilV)
			}
			continue
		}
		if style, chomp, ok := blockScalarTag(rest); ok {
			l.pos++
			arr.Elems = append(arr.Elems, l.parseBlockScalar(style, chomp, indent))
			continue
		}
		// The element starts on the dash line. Re-point the current line at the
		// content after "- " (its effective indent is indent+2) and parse it.
		l.lines[l.pos].content = rest
		l.lines[l.pos].indent = indent + 2
		arr.Elems = append(arr.Elems, l.parseNode(indent+1))
	}
	return l.applySeqTag(arr, tag)
}

// parseMapping parses a block mapping: consecutive "key: …" lines at exactly
// indent. Each value is inline (after the colon) or an indented block.
func (l *yamlLoader) parseMapping(indent int, tag string) object.Value {
	h := object.NewHash()
	for l.pos < len(l.lines) {
		line := l.lines[l.pos]
		if line.indent != indent {
			break
		}
		keyStr, val, ok := splitMapEntry(line.content)
		if !ok {
			break
		}
		key := l.scalarValue(keyStr, "")
		if strings.TrimSpace(val) == "" {
			// The value is on the following lines, or nil if none belong to it. A
			// block sequence value sits at the SAME indent as its key (Psych aligns
			// the dashes under the key), whereas any other nested block is deeper.
			l.pos++
			if next := l.peek(); next != nil && (next.indent > indent || (next.indent == indent && isSeqEntry(next.content))) {
				h.Set(key, l.parseNode(indent))
			} else {
				h.Set(key, object.NilV)
			}
			continue
		}
		if style, chomp, ok := blockScalarTag(strings.TrimSpace(val)); ok {
			l.pos++
			h.Set(key, l.parseBlockScalar(style, chomp, indent))
			continue
		}
		// Inline value on the same line.
		l.lines[l.pos].content = strings.TrimSpace(val)
		l.lines[l.pos].indent = indent + 1
		h.Set(key, l.parseNode(indent+1))
	}
	return l.applyMapTag(h, tag)
}

// scalarValue parses a single scalar token (already stripped of any block tag) to
// a Ruby value, honouring an explicit tag (`!ruby/symbol`, `*alias`) and the
// implicit Psych scalar grammar otherwise.
func (l *yamlLoader) scalarValue(s string, tag string) object.Value {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "*") {
		// An alias to a previously-anchored node.
		if v, ok := l.anchors[strings.TrimSpace(s[1:])]; ok {
			return v
		}
		return object.NilV
	}
	switch {
	case s == "[]":
		return l.applySeqTag(&object.Array{}, tag)
	case s == "{}":
		// An inline "<tag> {}" empty mapping (e.g. "!ruby/object:Foo {}") builds the
		// tagged empty value.
		return l.applyMapTag(object.NewHash(), tag)
	case strings.HasPrefix(s, "["):
		return l.parseFlowSeq(s)
	case strings.HasPrefix(s, "{"):
		return l.parseFlowMap(s)
	}
	switch tag {
	case "!ruby/symbol", "!ruby/sym":
		return object.Symbol(unquoteScalar(s))
	case "!ruby/string", "!str", "tag:yaml.org,2002:str":
		return object.NewString(unquoteScalar(s))
	}
	return parsePlainScalar(s)
}

// parseFlowSeq parses a single-line flow sequence "[a, b, c]".
func (l *yamlLoader) parseFlowSeq(s string) object.Value {
	inner := strings.TrimSpace(s[1 : len(s)-1])
	arr := &object.Array{}
	if inner == "" {
		return arr
	}
	for _, item := range splitFlow(inner) {
		arr.Elems = append(arr.Elems, l.scalarValue(item, ""))
	}
	return arr
}

// parseFlowMap parses a single-line flow mapping "{a: 1, b: 2}".
func (l *yamlLoader) parseFlowMap(s string) object.Value {
	inner := strings.TrimSpace(s[1 : len(s)-1])
	h := object.NewHash()
	if inner == "" {
		return h
	}
	for _, item := range splitFlow(inner) {
		k, v, ok := splitMapEntry(item)
		if !ok {
			continue
		}
		h.Set(l.scalarValue(k, ""), l.scalarValue(v, ""))
	}
	return h
}

// peek returns the next unconsumed line, or nil at end of input.
func (l *yamlLoader) peek() *yamlLine {
	if l.pos >= len(l.lines) {
		return nil
	}
	return &l.lines[l.pos]
}

// bind records v under anchorName for later *alias references (no-op for "").
func (l *yamlLoader) bind(anchorName string, v object.Value) {
	if anchorName != "" {
		l.anchors[anchorName] = v
	}
}

// applySeqTag adapts a parsed sequence to its tag. Only the plain sequence is
// meaningful for the round-trip set; an unknown tag is ignored (the array stands).
func (l *yamlLoader) applySeqTag(arr *object.Array, _ string) object.Value {
	return arr
}

// applyMapTag adapts a parsed mapping to its tag: a `!ruby/object:Class` mapping
// becomes an instance of Class whose ivars are the mapping entries; other tags
// leave the Hash as-is (Puppet only round-trips Hash/Array/scalars locally).
func (l *yamlLoader) applyMapTag(h *object.Hash, tag string) object.Value {
	if cls, ok := rubyObjectTag(tag); ok {
		return l.buildObject(cls, h)
	}
	return h
}

// taggedEmpty builds the value for a tag with no body: an empty object for
// `!ruby/object`, otherwise an empty Hash.
func (l *yamlLoader) taggedEmpty(tag string) object.Value {
	if cls, ok := rubyObjectTag(tag); ok {
		return l.buildObject(cls, object.NewHash())
	}
	return object.NewHash()
}

// buildObject materialises a `!ruby/object:Class` instance: an RObject of the
// named class (resolved through the VM, or a freshly registered placeholder
// class if unknown) with one ivar per mapping entry. It is the loader half of
// the generic object emitter, so a round-tripped report graph reconstructs.
func (l *yamlLoader) buildObject(className string, h *object.Hash) object.Value {
	cls := l.vm.yamlResolveClass(className)
	obj := &RObject{class: cls, ivars: map[string]object.Value{}}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		obj.ivars["@"+keyName(k)] = v
	}
	return obj
}

// keyName renders a mapping key as the bare ivar name (a Symbol or String key,
// both used by Psych object mappings).
func keyName(k object.Value) string {
	switch kk := k.(type) {
	case object.Symbol:
		return string(kk)
	case *object.String:
		return kk.Str()
	}
	return ""
}

// --- scalar grammar -------------------------------------------------------

// parsePlainScalar maps an unquoted / quoted scalar token to a Ruby value using
// Psych's implicit typing: null, booleans, symbols, integers, floats, timestamps,
// or a String otherwise. Quoted forms are always Strings.
func parsePlainScalar(s string) object.Value {
	if s == "" {
		return object.NilV
	}
	if s[0] == '\'' {
		return object.NewString(unquoteSingle(s))
	}
	if s[0] == '"' {
		return object.NewString(unquoteDouble(s))
	}
	switch s {
	case "~", "null", "Null", "NULL":
		return object.NilV
	case "true", "True", "TRUE":
		return object.Bool(true)
	case "false", "False", "FALSE":
		return object.Bool(false)
	case ".inf", ".Inf", ".INF", "+.inf":
		return object.Float(posInf())
	case "-.inf", "-.Inf", "-.INF":
		return object.Float(negInf())
	case ".nan", ".NaN", ".NAN":
		return object.Float(nan())
	}
	if strings.HasPrefix(s, ":") {
		return object.Symbol(unquoteScalar(s[1:]))
	}
	if t, ok := parseYAMLTime(s); ok {
		return t
	}
	if v, ok := parseYAMLInteger(s); ok {
		return v
	}
	if fv, ok := parseYAMLFloat(s); ok {
		return object.Float(fv)
	}
	return object.NewString(s)
}

// parseYAMLInteger parses a YAML integer scalar (decimal, with optional sign and
// underscore separators, or 0x hex) to an Integer or, on overflow, a Bignum,
// reporting failure for anything else.
func parseYAMLInteger(s string) (object.Value, bool) {
	clean := strings.ReplaceAll(s, "_", "")
	if clean == "" {
		return nil, false
	}
	base := 10
	digits := clean
	if hx := stripHexPrefix(clean); hx != "" {
		base, digits = 16, hx
	}
	bi, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return nil, false
	}
	return object.NormInt(bi), true
}

// stripHexPrefix returns the hex digits of a "0x" / "-0x" / "+0x" literal
// (preserving the sign), or "" when s is not a hex literal.
func stripHexPrefix(s string) string {
	sign := ""
	body := s
	if len(body) > 0 && (body[0] == '+' || body[0] == '-') {
		if body[0] == '-' {
			sign = "-"
		}
		body = body[1:]
	}
	if !strings.HasPrefix(body, "0x") && !strings.HasPrefix(body, "0X") {
		return ""
	}
	return sign + body[2:]
}

// parseYAMLFloat parses a YAML float scalar (it must carry a '.' or exponent so a
// bare integer does not match here).
func parseYAMLFloat(s string) (float64, bool) {
	if !strings.ContainsAny(s, ".eE") {
		return 0, false
	}
	clean := strings.ReplaceAll(s, "_", "")
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// parseYAMLTime parses the Psych ISO-8601 timestamp the emitter writes
// ("2026-06-29 05:18:32.000000000 Z" / "… -07:00"), to whole-second resolution.
func parseYAMLTime(s string) (object.Value, bool) {
	layouts := []string{
		"2006-01-02 15:04:05.000000000 Z",
		"2006-01-02 15:04:05.000000000 -07:00",
		"2006-01-02 15:04:05 Z",
		"2006-01-02 15:04:05 -07:00",
		"2006-01-02T15:04:05Z07:00",
	}
	for _, layout := range layouts {
		if t, err := stdParseTime(layout, s); err == nil {
			return &Time{t: gotime.FromUnix(t.Unix())}, true
		}
	}
	return nil, false
}

// --- token helpers --------------------------------------------------------

// isSeqEntry reports whether content is a block-sequence entry: "-" alone or
// "- value".
func isSeqEntry(content string) bool {
	return content == "-" || strings.HasPrefix(content, "- ")
}

// isFlowStart reports whether content opens a flow collection ("[" or "{"), which
// is parsed as a scalar value rather than a block mapping.
func isFlowStart(content string) bool {
	return strings.HasPrefix(content, "[") || strings.HasPrefix(content, "{")
}

// splitMapEntry splits "key: value" into its key and (possibly empty) value,
// honouring quoted keys so a ":" inside quotes is not a separator. It reports
// false when content is not a mapping entry.
func splitMapEntry(content string) (key, value string, ok bool) {
	i := mapColon(content)
	if i < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(content[:i])
	value = strings.TrimSpace(content[i+1:])
	return key, value, true
}

// mapColon returns the index of the key/value separator ": " (or a trailing ":")
// at the top flow level, skipping over quoted spans, or -1 if there is none.
func mapColon(content string) int {
	var quote byte
	for i := 0; i < len(content); i++ {
		c := content[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case ':':
			if i == len(content)-1 || content[i+1] == ' ' {
				return i
			}
		}
	}
	return -1
}

// splitTagAnchor peels a leading "!tag" and/or "&anchor" from a node's first
// line, returning the tag (with leading '!'), the anchor name, and the remaining
// content. Either or both may be absent.
func splitTagAnchor(content string) (tag, anchor, rest string) {
	rest = content
	for {
		rest = strings.TrimLeft(rest, " ")
		switch {
		case strings.HasPrefix(rest, "&"):
			anchor, rest = firstWord(rest[1:])
		case strings.HasPrefix(rest, "!"):
			tag, rest = firstWord(rest)
		default:
			return tag, anchor, strings.TrimLeft(rest, " ")
		}
	}
}

// firstWord splits s at its first space, returning the word and the remainder.
func firstWord(s string) (word, rest string) {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// rubyObjectTag reports whether tag is a `!ruby/object[:Class]` tag and returns
// the class name ("Object" for the bare form).
func rubyObjectTag(tag string) (string, bool) {
	const p = "!ruby/object"
	if tag == p {
		return "Object", true
	}
	if strings.HasPrefix(tag, p+":") {
		return tag[len(p)+1:], true
	}
	return "", false
}

// splitFlow splits a flow-collection body on top-level commas, respecting quotes
// and nested brackets/braces.
func splitFlow(s string) []string {
	var parts []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

// unquoteScalar strips surrounding single/double quotes (and applies their
// escaping) if present, returning the bare string otherwise.
func unquoteScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' {
		return unquoteSingle(s)
	}
	if len(s) >= 2 && s[0] == '"' {
		return unquoteDouble(s)
	}
	return s
}

// unquoteSingle decodes a single-quoted YAML scalar (” is a literal quote).
func unquoteSingle(s string) string {
	body := s
	if len(body) >= 2 && body[0] == '\'' && body[len(body)-1] == '\'' {
		body = body[1 : len(body)-1]
	}
	return strings.ReplaceAll(body, "''", "'")
}

// unquoteDouble decodes a double-quoted YAML scalar, reversing the escapes the
// emitter writes (\\, \", \n, \t, \r, \0, \xNN).
func unquoteDouble(s string) string {
	body := s
	if len(body) >= 2 && body[0] == '"' && body[len(body)-1] == '"' {
		body = body[1 : len(body)-1]
	}
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' || i+1 >= len(body) {
			b.WriteByte(c)
			continue
		}
		i++
		switch body[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '0':
			b.WriteByte(0)
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'x':
			if i+2 < len(body) {
				if n, err := strconv.ParseUint(body[i+1:i+3], 16, 8); err == nil {
					b.WriteByte(byte(n))
					i += 2
					continue
				}
			}
			b.WriteByte('x')
		default:
			b.WriteByte(body[i])
		}
	}
	return b.String()
}
