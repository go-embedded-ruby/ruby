// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// yamlEncoder serialises a tree of plain Ruby values (Hash, Array, String,
// Symbol, Integer, Float, true/false/nil, Time) to a Psych-compatible YAML
// document. It deliberately covers only the value shapes Puppet's local
// persistence writes — the run-summary Hash (last_run_summary.yaml) and the
// agent state Hash (state.yaml) — and raises a Ruby TypeError for any other
// object, so the indirector that wraps it degrades cleanly (its rescue logs a
// Puppet.err) for the full !ruby/object: report graph that a complete Psych
// emitter would be needed to dump.
type yamlEncoder struct {
	b    strings.Builder
	vm   *VM
	root object.Value // the value being dumped, used to compute the reference graph
	// anchors maps an already-emitted reference value (by identity) to its anchor
	// number, so a shared / cyclic node in the report graph is written once with
	// "&N" and referenced thereafter with "*N".
	anchors map[object.Value]int
	// refcount holds how many times each anchorable node is reachable from root;
	// a count above one forces an anchor. Computed lazily on the first shared()
	// query and cached.
	refcount map[object.Value]int
	seq      int
}

// yamlDump renders v as a complete YAML document beginning with the "---"
// directive, matching Psych.dump's output for the supported value shapes. vm
// supplies class names and ivar tables for generic object emission; it may be nil
// for the plain-value tests, in which case an arbitrary object raises TypeError.
func yamlDump(vm *VM, v object.Value) string {
	e := &yamlEncoder{vm: vm, root: v, anchors: map[object.Value]int{}}
	e.b.WriteString("---")
	if isYAMLComplex(v) {
		// A tagged complex value (object/range) writes "--- <tag>" then its mapping
		// body at indent 0.
		e.b.WriteByte(' ')
		e.writeAnchorTag(v)
		if e.tagBodyEmpty(v) {
			e.b.WriteString(" {}\n")
			return e.b.String()
		}
		e.b.WriteByte('\n')
		e.encodePairs(e.tagBody(v), 0)
		return e.b.String()
	}
	if isYAMLInline(v) {
		// A scalar, or an empty collection, follows "--- " on the document line.
		e.b.WriteByte(' ')
		e.b.WriteString(e.inlineEmpty(v))
		e.b.WriteByte('\n')
		return e.b.String()
	}
	e.b.WriteByte('\n')
	e.encodeNode(v, 0)
	return e.b.String()
}

// isYAMLInline reports whether v renders on the document line: any scalar, or an
// empty Array/Hash (Psych writes "--- []" / "--- {}").
func isYAMLInline(v object.Value) bool {
	switch n := v.(type) {
	case *object.Array:
		return len(n.Elems) == 0
	case *object.Hash:
		return n.Len() == 0
	}
	return true
}

// inlineEmpty renders the document-line value: "[]"/"{}" for an empty collection,
// otherwise the scalar form.
func (e *yamlEncoder) inlineEmpty(v object.Value) string {
	switch n := v.(type) {
	case *object.Array:
		_ = n
		return "[]"
	case *object.Hash:
		_ = n
		return "{}"
	}
	return e.scalar(v)
}

// encodeNode writes a non-empty collection at the given indentation. It follows
// Psych's default block style: a block sequence that is a mapping value sits at
// the SAME indent as its key (the dashes align under the key), while a nested
// mapping is indented two spaces deeper; under a sequence dash the child's first
// line continues on the dash line ("- - 1", "- a: 1").
func (e *yamlEncoder) encodeNode(v object.Value, indent int) {
	pad := strings.Repeat(" ", indent)
	switch n := v.(type) {
	case *object.Array:
		for _, el := range n.Elems {
			e.b.WriteString(pad)
			e.b.WriteByte('-')
			e.writeSeqChild(el, indent)
		}
	case *object.Hash:
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			if e.writeComplexKey(k, val, indent, pad) {
				continue
			}
			e.b.WriteString(pad)
			e.b.WriteString(e.keyScalar(k))
			e.b.WriteByte(':')
			e.writeMapChild(val, indent)
		}
	}
}

// keyScalar renders a mapping key scalar. It differs from scalar only for a nil
// key, which Psych writes as "! ”" (the plain empty form scalar would emit is
// ambiguous as a key), so the "key:" line always parses.
func (e *yamlEncoder) keyScalar(k object.Value) string {
	if _, ok := k.(object.Nil); ok {
		return "! ''"
	}
	return e.scalar(k)
}

// writeComplexKey emits a mapping entry whose key is itself a non-scalar value
// (an Array, Hash or object), using Psych's explicit "? <key>" / ": <value>"
// block form, and reports whether it handled the entry. openPad is written before
// the "?" (empty when it already sits on a parent dash line); the ":" line is
// always written at the mapping's own indent. A scalar key returns false so the
// caller uses the inline "key:" form.
func (e *yamlEncoder) writeComplexKey(k, val object.Value, indent int, openPad string) bool {
	if !isComplexKey(k) {
		return false
	}
	e.b.WriteString(openPad)
	e.b.WriteByte('?')
	// The key block opens on the "?" line exactly as a sequence-dash child would
	// (object tag / nested collection), reusing writeSeqChild's layout.
	e.writeSeqChild(k, indent)
	e.b.WriteString(strings.Repeat(" ", indent))
	e.b.WriteByte(':')
	e.writeMapChild(val, indent)
	return true
}

// isComplexKey reports whether a mapping key must be written with the explicit
// "?"/":" block form (any non-scalar value).
func isComplexKey(v object.Value) bool {
	switch n := v.(type) {
	case *object.Array:
		return len(n.Elems) > 0
	case *object.Hash:
		return n.Len() > 0
	}
	return isYAMLComplex(v)
}

// writeMapChild emits the value of a mapping entry (after "key:"): a scalar
// inline, or a nested collection on the following lines. A block sequence stays
// at the key's indent; a nested mapping is indented two deeper.
func (e *yamlEncoder) writeMapChild(v object.Value, indent int) {
	if e.writeComplexChild(v, indent) {
		return
	}
	switch n := v.(type) {
	case *object.Array:
		if len(n.Elems) == 0 {
			e.b.WriteString(" []\n")
			return
		}
		e.b.WriteByte('\n')
		e.encodeNode(v, indent) // sequence aligns under the key
	case *object.Hash:
		if n.Len() == 0 {
			e.b.WriteString(" {}\n")
			return
		}
		e.b.WriteByte('\n')
		e.encodeNode(v, indent+2)
	default:
		e.writeInlineScalar(v)
	}
}

// writeSeqChild emits the value following a sequence dash. A scalar is inline;
// a nested collection continues on the dash line, its remaining lines indented
// two spaces deeper (Psych's "- - 1" / "- a: 1" layout).
func (e *yamlEncoder) writeSeqChild(v object.Value, indent int) {
	if e.writeComplexSeqChild(v, indent) {
		return
	}
	switch n := v.(type) {
	case *object.Array:
		if len(n.Elems) == 0 {
			e.b.WriteString(" []\n")
			return
		}
		e.b.WriteByte(' ')
		e.encodeInlineFirst(v, indent+2)
	case *object.Hash:
		if n.Len() == 0 {
			e.b.WriteString(" {}\n")
			return
		}
		e.b.WriteByte(' ')
		e.encodeInlineFirst(v, indent+2)
	default:
		e.writeInlineScalar(v)
	}
}

// encodeInlineFirst renders a collection whose first line was already opened on
// the parent's dash line (the cursor sits just after "- "): the first element is
// written without its leading indent, and subsequent elements at the given
// indent.
func (e *yamlEncoder) encodeInlineFirst(v object.Value, indent int) {
	pad := strings.Repeat(" ", indent)
	switch n := v.(type) {
	case *object.Array:
		for i, el := range n.Elems {
			if i > 0 {
				e.b.WriteString(pad)
			}
			e.b.WriteByte('-')
			e.writeSeqChild(el, indent)
		}
	case *object.Hash:
		for i, k := range n.Keys {
			val, _ := n.Get(k)
			// The "?" opener of a complex key carries the row indent for every entry
			// except the first (which already sits on the parent dash line).
			openPad := pad
			if i == 0 {
				openPad = ""
			}
			if e.writeComplexKey(k, val, indent, openPad) {
				continue
			}
			if i > 0 {
				e.b.WriteString(pad)
			}
			e.b.WriteString(e.keyScalar(k))
			e.b.WriteByte(':')
			e.writeMapChild(val, indent)
		}
	}
}

// writeInlineScalar writes a scalar value on the current line after a "-" or
// "key:". A nil renders as nothing after the indicator (Psych's bare "key:").
func (e *yamlEncoder) writeInlineScalar(v object.Value) {
	s := e.scalar(v)
	if s == "" {
		e.b.WriteByte('\n')
		return
	}
	e.b.WriteByte(' ')
	e.b.WriteString(s)
	e.b.WriteByte('\n')
}

// scalar renders a single non-collection value to its Psych scalar form.
func (e *yamlEncoder) scalar(v object.Value) string {
	switch n := v.(type) {
	case object.Nil:
		return ""
	case object.Bool:
		if bool(n) {
			return "true"
		}
		return "false"
	case object.Integer:
		return n.ToS()
	case *object.Bignum:
		return n.ToS()
	case object.Float:
		return yamlFloat(float64(n))
	case object.Symbol:
		return yamlSymbol(string(n))
	case *object.String:
		return yamlString(n.Str())
	case *Time:
		// Psych emits a Time as an unquoted ISO-8601 timestamp, using "Z" for a
		// UTC instant and the numeric "+HH:MM" offset otherwise.
		ts := n.t.Format("2006-01-02 15:04:05.000000000 -07:00")
		if strings.HasSuffix(ts, " +00:00") {
			ts = strings.TrimSuffix(ts, " +00:00") + " Z"
		}
		return ts
	case *Regexp:
		// Psych emits a Regexp inline as "!ruby/regexp /source/flags".
		return "!ruby/regexp /" + n.source + "/" + orderFlags(n.flags)
	case *RClass:
		// Psych emits a Class / Module reference inline as a single-quoted name
		// behind the !ruby/class or !ruby/module tag.
		tag := "!ruby/class "
		if n.isModule {
			tag = "!ruby/module "
		}
		return tag + "'" + n.ToS() + "'"
	}
	raise("TypeError", "can't dump %s to YAML", classNameOf(v))
	return ""
}

// yamlFloat renders a Float the way Psych does (.inf / .nan, and a trailing
// ".0" for integral values).
func yamlFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return ".inf"
	case math.IsInf(f, -1):
		return "-.inf"
	case math.IsNaN(f):
		return ".nan"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// yamlSymbol renders a Ruby Symbol as `:name`. Psych writes the bare `:name`
// form for the symbols that occur in Puppet's persisted state (`:checked`,
// `:synced`, and even names with spaces/dashes like `:a b`); a name carrying a
// newline is escaped via the double-quoted form. This covers the realistic
// symbol shapes; the rare empty / control-bearing symbol is not exercised.
func yamlSymbol(name string) string {
	if strings.ContainsAny(name, "\n\t") {
		return ":" + yamlDoubleQuote(name)
	}
	return ":" + name
}

// yamlString renders a Ruby String following Psych's plain/single/double-quote
// and block-scalar selection.
func yamlString(s string) string {
	if s == "" {
		return "''"
	}
	if strings.Contains(s, "\n") {
		return yamlBlockScalar(s)
	}
	if needsDoubleQuote(s) {
		return yamlDoubleQuote(s)
	}
	if needsSingleQuote(s) {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return s
}

// yamlBlockScalar renders a multi-line string as a literal block scalar (`|-`,
// stripping the trailing newline chomp), indented two spaces under its key.
func yamlBlockScalar(s string) string {
	chomp := "-"
	body := s
	if strings.HasSuffix(s, "\n") {
		chomp = ""
		body = strings.TrimRight(s, "\n")
	}
	var b strings.Builder
	b.WriteString("|" + chomp)
	for _, line := range strings.Split(body, "\n") {
		b.WriteString("\n  ")
		b.WriteString(line)
	}
	return b.String()
}

// needsDoubleQuote reports whether a string contains a byte that forces
// double-quoting (control characters or a non-printable rune).
func needsDoubleQuote(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	// Leading indicator characters that single-quoting cannot rescue safely are
	// rendered by Psych with double quotes.
	switch s[0] {
	case '-', '?', ':', '@', '`', '*', '&', '!', '%', '#', '~', '|', '>', '"', '\'', '{', '}', '[', ']', ',':
		// Psych double-quotes a leading "-" only when not a valid plain token; it is
		// simpler and always valid to double-quote these leading-indicator strings.
		return true
	}
	return false
}

// needsSingleQuote reports whether a string that is otherwise plain must be
// single-quoted because it would round-trip as a non-string (a bool/null/number/
// timestamp keyword) or carries a YAML indicator that breaks the plain flow. A
// "#" only needs quoting at the start or after a space (a comment indicator), and
// a ":" only when it ends the string or is followed by a space (a mapping
// indicator) — Psych leaves "a#b" and "File[/x]" unquoted.
func needsSingleQuote(s string) bool {
	if yamlReservedWord(s) || looksNumericYAML(s) {
		return true
	}
	if strings.HasPrefix(s, "#") || strings.Contains(s, " #") {
		return true
	}
	if strings.HasSuffix(s, ":") || strings.Contains(s, ": ") {
		return true
	}
	return false
}

var yamlReserved = map[string]bool{
	"yes": true, "no": true, "true": true, "false": true,
	"null": true, "~": true, "on": true, "off": true,
	"y": true, "n": true,
}

func yamlReservedWord(s string) bool { return yamlReserved[strings.ToLower(s)] }

var yamlNumRe = regexp.MustCompile(`\A[-+]?(\d[\d_]*)(\.\d*)?([eE][-+]?\d+)?\z`)
var yamlDateRe = regexp.MustCompile(`\A\d{4}-\d{2}-\d{2}`)
var yamlTimeRe = regexp.MustCompile(`\A\d{1,2}:\d{2}`)
var yamlHexRe = regexp.MustCompile(`\A0x[0-9A-Fa-f]+\z`)

// looksNumericYAML reports whether a plain string would be parsed back as a
// number, timestamp, or hex literal and so must be quoted to stay a string.
func looksNumericYAML(s string) bool {
	return yamlNumRe.MatchString(s) || yamlDateRe.MatchString(s) || yamlTimeRe.MatchString(s) || yamlHexRe.MatchString(s)
}

// yamlDoubleQuote renders s as a double-quoted YAML scalar, escaping the
// characters Psych escapes.
func yamlDoubleQuote(s string) string {
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
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case 0:
			b.WriteString(`\0`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
