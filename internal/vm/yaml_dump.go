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
	b strings.Builder
}

// yamlDump renders v as a complete YAML document beginning with the "---"
// directive, matching Psych.dump's output for the supported value shapes.
func yamlDump(v object.Value) string {
	e := &yamlEncoder{}
	e.b.WriteString("---")
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
			e.b.WriteString(pad)
			e.b.WriteString(e.scalar(k))
			e.b.WriteByte(':')
			e.writeMapChild(val, indent)
		}
	}
}

// writeMapChild emits the value of a mapping entry (after "key:"): a scalar
// inline, or a nested collection on the following lines. A block sequence stays
// at the key's indent; a nested mapping is indented two deeper.
func (e *yamlEncoder) writeMapChild(v object.Value, indent int) {
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
			if i > 0 {
				e.b.WriteString(pad)
			}
			e.b.WriteString(e.scalar(k))
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
		// Psych emits a Time as an unquoted ISO-8601 timestamp.
		return n.t.Format("2006-01-02 15:04:05.000000000 -07:00")
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

// yamlSymbol renders a Ruby Symbol. A plain symbol is `:name`; one whose name
// needs quoting (spaces, specials) is rendered `:"name"`.
func yamlSymbol(name string) string {
	if name != "" && plainSymbolName(name) {
		return ":" + name
	}
	return ":" + yamlDoubleQuote(name)
}

var plainSymbolRe = regexp.MustCompile(`\A[A-Za-z_][A-Za-z0-9_]*[!?=]?\z`)

func plainSymbolName(s string) bool { return plainSymbolRe.MatchString(s) }

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
// timestamp keyword) or contains a YAML indicator.
func needsSingleQuote(s string) bool {
	if yamlReservedWord(s) || looksNumericYAML(s) || strings.ContainsAny(s, ":#") || strings.Contains(s, ": ") {
		return true
	}
	// A trailing colon or a value containing flow/indicator characters.
	if strings.HasSuffix(s, ":") {
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
