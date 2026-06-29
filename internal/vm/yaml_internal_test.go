// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestYAMLUnquoteDouble covers every escape the double-quoted decoder reverses,
// plus the dangling-backslash and unknown-escape fallbacks.
func TestYAMLUnquoteDouble(t *testing.T) {
	cases := map[string]string{
		`"a\nb"`:   "a\nb",
		`"a\tb"`:   "a\tb",
		`"a\rb"`:   "a\rb",
		`"a\0b"`:   "a\x00b",
		`"a\"b"`:   "a\"b",
		`"a\\b"`:   "a\\b",
		`"a\x41b"`: "aAb",
		`"a\qb"`:   "aqb",     // unknown escape drops the backslash
		`"a\x"`:    "ax",      // truncated \x falls back to a literal x
		`"trail\"`: "trail\\", // a trailing backslash with no following char
		`plain`:    "plain",   // not quoted: returned as-is
		`""`:       "",
	}
	for in, want := range cases {
		if got := unquoteDouble(in); got != want {
			t.Errorf("unquoteDouble(%q)=%q want %q", in, got, want)
		}
	}
}

// TestYAMLUnquoteSingleAndScalar covers single-quote decoding and the
// unquoteScalar dispatcher (single / double / bare).
func TestYAMLUnquoteSingleAndScalar(t *testing.T) {
	if got := unquoteSingle("'it''s'"); got != "it's" {
		t.Errorf("unquoteSingle: %q", got)
	}
	if got := unquoteSingle("bare"); got != "bare" {
		t.Errorf("unquoteSingle bare: %q", got)
	}
	if got := unquoteScalar(`'x'`); got != "x" {
		t.Errorf("unquoteScalar single: %q", got)
	}
	if got := unquoteScalar(`"x"`); got != "x" {
		t.Errorf("unquoteScalar double: %q", got)
	}
	if got := unquoteScalar(`x`); got != "x" {
		t.Errorf("unquoteScalar bare: %q", got)
	}
}

// TestYAMLSplitFlow covers flow-body splitting across quotes and nested
// brackets / braces.
func TestYAMLSplitFlow(t *testing.T) {
	got := splitFlow(`1, "a, b", [2, 3], {x: 4}`)
	want := []string{"1", `"a, b"`, "[2, 3]", "{x: 4}"}
	if len(got) != len(want) {
		t.Fatalf("splitFlow len=%d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitFlow[%d]=%q want %q", i, got[i], want[i])
		}
	}
	// A single-quoted span hides a comma too.
	if parts := splitFlow(`'a, b', c`); len(parts) != 2 {
		t.Errorf("splitFlow single-quote: %v", parts)
	}
}

// TestYAMLSplitTagAnchor covers peeling tag and anchor prefixes in either order,
// and firstWord's no-space branch.
func TestYAMLSplitTagAnchor(t *testing.T) {
	tag, anc, rest := splitTagAnchor("&1 !ruby/object:Foo a: 1")
	if tag != "!ruby/object:Foo" || anc != "1" || rest != "a: 1" {
		t.Errorf("anchor-then-tag: tag=%q anc=%q rest=%q", tag, anc, rest)
	}
	tag, anc, rest = splitTagAnchor("!ruby/symbol &2 sym")
	if tag != "!ruby/symbol" || anc != "2" || rest != "sym" {
		t.Errorf("tag-then-anchor: tag=%q anc=%q rest=%q", tag, anc, rest)
	}
	// A lone anchor with nothing after it (firstWord no-space branch).
	tag, anc, rest = splitTagAnchor("&9")
	if tag != "" || anc != "9" || rest != "" {
		t.Errorf("lone anchor: tag=%q anc=%q rest=%q", tag, anc, rest)
	}
}

// TestYAMLMapColon covers the key/value separator scan, including a quoted ":"
// that is not a separator and a missing separator.
func TestYAMLMapColon(t *testing.T) {
	if i := mapColon(`a: 1`); i != 1 {
		t.Errorf("mapColon plain: %d", i)
	}
	if i := mapColon(`"a:b": 1`); i != 5 {
		t.Errorf("mapColon quoted: %d", i)
	}
	if i := mapColon(`'x': 1`); i != 3 {
		t.Errorf("mapColon single-quoted: %d", i)
	}
	// A trailing ":" is a separator.
	if i := mapColon(`k:`); i != 1 {
		t.Errorf("mapColon trailing: %d", i)
	}
	// A ":" not followed by a space is not a separator (resource ref).
	if i := mapColon(`File[/x]`); i != -1 {
		t.Errorf("mapColon none: %d", i)
	}
	if _, _, ok := splitMapEntry(`nosep`); ok {
		t.Errorf("splitMapEntry should fail on a non-entry")
	}
}

// TestYAMLRubyObjectTag covers the bare and qualified !ruby/object tag forms and
// a non-matching tag.
func TestYAMLRubyObjectTag(t *testing.T) {
	if n, ok := rubyObjectTag("!ruby/object"); !ok || n != "Object" {
		t.Errorf("bare: %q %v", n, ok)
	}
	if n, ok := rubyObjectTag("!ruby/object:Foo::Bar"); !ok || n != "Foo::Bar" {
		t.Errorf("qualified: %q %v", n, ok)
	}
	if _, ok := rubyObjectTag("!ruby/symbol"); ok {
		t.Errorf("non-object tag matched")
	}
}

// TestYAMLBlockScalarTag covers each block indicator and the non-block input.
func TestYAMLBlockScalarTag(t *testing.T) {
	for _, c := range []struct {
		in           string
		style, chomp byte
		ok           bool
	}{
		{"|", '|', 0, true},
		{"|-", '|', '-', true},
		{"|+", '|', '+', true},
		{">", '>', 0, true},
		{">-", '>', '-', true},
		{"", 0, 0, false},
		{"plain", 0, 0, false},
		{"|x", 0, 0, false}, // a trailing non-chomp char is not a block indicator
	} {
		style, chomp, ok := blockScalarTag(c.in)
		if style != c.style || chomp != c.chomp || ok != c.ok {
			t.Errorf("blockScalarTag(%q)=(%q,%q,%v) want (%q,%q,%v)", c.in, style, chomp, ok, c.style, c.chomp, c.ok)
		}
	}
}

// TestYAMLParseYAMLInteger covers decimal, signed, underscored, hex and the
// non-integer fallback.
func TestYAMLParseYAMLInteger(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int64
		ok   bool
	}{
		{"42", 42, true},
		{"-7", -7, true},
		{"1_000", 1000, true},
		{"0x1A", 26, true},
		{"+0xff", 255, true},
		{"-0x10", -16, true},
		{"", 0, false},
		{"0xZZ", 0, false},
		{"notnum", 0, false},
	} {
		v, ok := parseYAMLInteger(c.in)
		if ok != c.ok {
			t.Errorf("parseYAMLInteger(%q) ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if ok {
			if iv, isInt := v.(object.Integer); !isInt || int64(iv) != c.want {
				t.Errorf("parseYAMLInteger(%q)=%v want %d", c.in, v, c.want)
			}
		}
	}
}

// TestYAMLParseFloatAndTime covers the float / timestamp grammar fallbacks.
func TestYAMLParseFloatAndTime(t *testing.T) {
	if f, ok := parseYAMLFloat("1.5"); !ok || f != 1.5 {
		t.Errorf("parseYAMLFloat 1.5: %v %v", f, ok)
	}
	if _, ok := parseYAMLFloat("123"); ok {
		t.Error("parseYAMLFloat should reject a bare integer")
	}
	if _, ok := parseYAMLFloat("1.x"); ok {
		t.Error("parseYAMLFloat should reject 1.x")
	}
	if _, ok := parseYAMLTime("not a time"); ok {
		t.Error("parseYAMLTime should reject a non-timestamp")
	}
	if _, ok := parseYAMLTime("2026-06-29 05:18:32 Z"); !ok {
		t.Error("parseYAMLTime should accept a seconds-only Z timestamp")
	}
}

// TestYAMLFloatSpecials covers posInf / negInf / nan and the float keyword scalar
// parse.
func TestYAMLFloatSpecials(t *testing.T) {
	if !math.IsInf(posInf(), 1) || !math.IsInf(negInf(), -1) || !math.IsNaN(nan()) {
		t.Error("float specials wrong")
	}
	if v := parsePlainScalar("+.inf"); !math.IsInf(float64(v.(object.Float)), 1) {
		t.Error("+.inf not parsed")
	}
}

// TestYAMLKeyName covers the Symbol / String / other key-name reductions.
func TestYAMLKeyName(t *testing.T) {
	if keyName(object.Symbol("s")) != "s" {
		t.Error("symbol key name")
	}
	if keyName(object.NewString("str")) != "str" {
		t.Error("string key name")
	}
	if keyName(object.Integer(3)) != "" {
		t.Error("other key name should be empty")
	}
}

// TestYAMLDumpScalarRClassNil covers keyScalar's nil branch and that dumping nil
// as a value differs from nil as a key.
func TestYAMLDumpNilKeyVsValue(t *testing.T) {
	e := &yamlEncoder{}
	if got := e.keyScalar(object.NilV); got != "! ''" {
		t.Errorf("keyScalar(nil)=%q", got)
	}
	if got := e.keyScalar(object.Integer(1)); got != "1" {
		t.Errorf("keyScalar(1)=%q", got)
	}
}

// TestYAMLObjectClassSuffix covers the tag-suffix reductions: a named class, the
// bare Object class, an anonymous class (empty name), and a nil class.
func TestYAMLObjectClassSuffix(t *testing.T) {
	if s := objectClassSuffix(&RObject{class: newClass("Foo", nil)}); s != ":Foo" {
		t.Errorf("named: %q", s)
	}
	if s := objectClassSuffix(&RObject{class: newClass("Object", nil)}); s != "" {
		t.Errorf("Object: %q", s)
	}
	if s := objectClassSuffix(&RObject{class: newClass("", nil)}); s != "" {
		t.Errorf("anonymous: %q", s)
	}
	if s := objectClassSuffix(&RObject{class: nil}); s != "" {
		t.Errorf("nil class: %q", s)
	}
}

// TestYAMLRangeBound covers the nil (beginless / endless) and present bounds.
func TestYAMLRangeBound(t *testing.T) {
	if rangeBound(nil) != object.NilV {
		t.Error("nil bound should map to NilV")
	}
	if v := rangeBound(object.Integer(3)); v != object.Integer(3) {
		t.Errorf("present bound: %v", v)
	}
}

// TestYAMLCountRefs covers the reference-count walk over each anchorable container
// (Array, Hash, RObject, Range, Set) and the nil short-circuit, asserting a shared
// node is counted more than once.
func TestYAMLCountRefs(t *testing.T) {
	shared := &RObject{class: newClass("N", nil), ivars: map[string]object.Value{}}
	// The same object reachable via a Set element, a Range bound, an RObject ivar,
	// an Array and a Hash — five references in one graph.
	set := newSet()
	set.add(shared)
	// A beginless range exercises countRefs's Go-nil bound short-circuit.
	rng := &object.Range{Lo: nil, Hi: shared}
	holder := &RObject{class: newClass("H", nil), ivars: map[string]object.Value{"@x": shared}}
	arr := &object.Array{Elems: []object.Value{shared, object.NilV}}
	h := object.NewHash()
	h.Set(object.NewString("k"), shared)
	root := &object.Array{Elems: []object.Value{set, rng, holder, arr, h}}

	e := &yamlEncoder{root: root}
	if !e.shared(shared) {
		t.Error("shared object should be detected as shared")
	}
	if e.refcount[shared] < 2 {
		t.Errorf("shared refcount=%d, want >= 2", e.refcount[shared])
	}
	// A node seen once is not shared.
	once := object.NewString("solo")
	if e.refcount[once] > 1 {
		t.Error("a non-shared node should not be counted twice")
	}
}

// TestYAMLEncoderEmptyBodies covers a Set and a Range emitted directly through
// the encoder so the openTag / tagBody Set+Range default branches run.
func TestYAMLEncoderComplexBodies(t *testing.T) {
	// An empty Set: "hash: {}".
	if got := yamlDump(nil, newSet()); got != "--- !ruby/object:Set\nhash: {}\n" {
		t.Errorf("empty set: %q", got)
	}
	// A Range value.
	rng := &object.Range{Lo: object.Integer(1), Hi: object.Integer(5)}
	if got := yamlDump(nil, rng); got != "--- !ruby/range\nbegin: 1\nend: 5\nexcl: false\n" {
		t.Errorf("range: %q", got)
	}
}

// TestYAMLLoaderParseBlockEmpty covers parseBlock's empty-body branch (a
// standalone object tag with no following lines) and taggedEmpty's object branch.
func TestYAMLLoaderParseBlockEmpty(t *testing.T) {
	vm := New(nil)
	// A document that is just a standalone !ruby/object tag with no body.
	v := yamlLoad(vm, "--- !ruby/object:Lonely\n")
	o, ok := v.(*RObject)
	if !ok || o.class.name != "Lonely" {
		t.Fatalf("standalone object tag: %T", v)
	}
	if len(o.ivars) != 0 {
		t.Errorf("expected no ivars, got %v", o.ivars)
	}
}

// TestYAMLLoaderBlockScalarShortLine covers parseBlockScalar's clamp branch for a
// continuation line whose raw length is shorter than the block's base indent (a
// malformed but tolerated under-indented line yields an empty content line).
func TestYAMLLoaderBlockScalarShortLine(t *testing.T) {
	vm := New(nil)
	// First body line "    aaaa" sets the base indent at 4; the next line "  z" has
	// raw length 3 (< 4), so the clamp slices it to the empty string.
	doc := "---\nx: |-\n    aaaa\n  z\n"
	v := yamlLoad(vm, doc)
	h := v.(*object.Hash)
	got, _ := h.Get(object.NewString("x"))
	s := got.(*object.String)
	if s.Str() != "aaaa\n" {
		t.Errorf("block scalar short line: %q", s.Str())
	}
}

// TestYAMLLoaderStructuralEdges covers the loader's structural break / EOF /
// fallthrough branches that the round-trip cases do not reach.
func TestYAMLLoaderStructuralEdges(t *testing.T) {
	vm := New(nil)
	// A bare document with no "---" marker (yamlLoad fallthrough to parseNode).
	if v := yamlLoad(vm, "a: 1\n"); v.(*object.Hash).Len() != 1 {
		t.Error("bare-document mapping")
	}
	// A document-root tag whose body is a SEQUENCE (an unusual / defensive shape:
	// the tag is ignored for a sequence body, which loads as a plain Array).
	v := yamlLoad(vm, "--- !something\n- 1\n- 2\n")
	if a, ok := v.(*object.Array); !ok || len(a.Elems) != 2 {
		t.Errorf("root tag with seq body: %T", v)
	}
	// A block scalar followed by a sibling at the parent indent (parseBlockScalar
	// break on indent <= parentIndent).
	doc := "---\nx: |-\n  a\ny: 2\n"
	h := yamlLoad(vm, doc).(*object.Hash)
	if h.Len() != 2 {
		t.Errorf("block scalar then sibling: len=%d", h.Len())
	}
	// A sequence that ends because the next line is at a shallower indent.
	d2 := "---\nk:\n- 1\nm: 2\n"
	h2 := yamlLoad(vm, d2).(*object.Hash)
	if h2.Len() != 2 {
		t.Errorf("seq then sibling key: len=%d", h2.Len())
	}
	// A standalone "-" dash whose value is a nested mapping (parseBlock seq via the
	// dash, plus parseSequence's dash-with-block element).
	d3 := "---\n-\n  a: 1\n- 2\n"
	a := yamlLoad(vm, d3).(*object.Array)
	if len(a.Elems) != 2 {
		t.Errorf("dash block element: %d", len(a.Elems))
	}
}

// TestYAMLLoaderMoreEdges covers the last structural branches: an inline tag
// whose body is a sequence, a tag with no body at all, a "-" dash at the very end
// of input, and a complex key that is not a mapping's first entry.
func TestYAMLLoaderMoreEdges(t *testing.T) {
	vm := New(nil)
	// A mapping value that is a tagged node whose body is a SEQUENCE (parseBlock's
	// sequence branch).
	v := yamlLoad(vm, "---\nk: !something\n  - 1\n  - 2\n")
	h := v.(*object.Hash)
	val, _ := h.Get(object.NewString("k"))
	if a, ok := val.(*object.Array); !ok || len(a.Elems) != 2 {
		t.Errorf("inline tag seq body: %T", val)
	}
	// A mapping value that is a tag with no body (parseBlock empty -> taggedEmpty).
	v2 := yamlLoad(vm, "---\nk: !ruby/object:Bodyless\nj: 1\n")
	h2 := v2.(*object.Hash)
	kv, _ := h2.Get(object.NewString("k"))
	if _, ok := kv.(*RObject); !ok {
		t.Errorf("bodyless inline tag: %T", kv)
	}
	// A trailing "-" dash with no value at end of input (parseNode at EOF -> nil
	// element).
	a := yamlLoad(vm, "---\n- 1\n-\n").(*object.Array)
	if len(a.Elems) != 2 || a.Elems[1] != object.NilV {
		t.Errorf("trailing dash: %v", a.Elems)
	}
	// A nested mapping whose block ends because the next same-indent line is not a
	// "key:" entry (parseMapping's splitMapEntry !ok break): here a deeper sequence
	// dash ends the inner mapping.
	d4 := "---\nouter:\n  a: 1\n- top\n"
	h4 := yamlLoad(vm, d4).(*object.Hash)
	if _, ok := h4.Get(object.NewString("outer")); !ok {
		t.Error("nested mapping then dash")
	}
	// parseNode at end-of-input returns nil (the defensive EOF guard).
	empty := &yamlLoader{vm: vm, anchors: map[string]object.Value{}}
	if empty.parseNode(0) != object.NilV {
		t.Error("parseNode at EOF should be nil")
	}
}

// TestYAMLLoaderEmptyAndPlain covers parsePlainScalar's empty-string branch and
// parseNode at end-of-input.
func TestYAMLLoaderEmptyAndPlain(t *testing.T) {
	if v := parsePlainScalar(""); v != object.NilV {
		t.Errorf("empty plain scalar: %v", v)
	}
	// A "key:" with nothing after it and nothing deeper -> nil value (parseNode at
	// the end of input through parseMapping).
	vm := New(nil)
	h := yamlLoad(vm, "---\nk:\n").(*object.Hash)
	val, _ := h.Get(object.NewString("k"))
	if val != object.NilV {
		t.Errorf("trailing empty value: %v", val)
	}
}

// TestYAMLResolveClassKnownQualified covers resolving a multi-segment class name
// whose segments all exist, and reusing a const that already holds a class.
func TestYAMLResolveClassKnownQualified(t *testing.T) {
	vm := New(nil)
	outer := newClass("Outer", vm.cObject)
	inner := newClass("Inner", vm.cObject)
	outer.consts["Inner"] = inner
	vm.cObject.consts["Outer"] = outer
	if c := vm.yamlResolveClass("Outer::Inner"); c != inner {
		t.Errorf("qualified known class: %v", c)
	}
	// A placeholder registered under a qualified name is returned as a class on a
	// second resolution (the consts[name] is-a-class reuse path).
	first := vm.yamlResolveClass("Made::Up::Name")
	if second := vm.yamlResolveClass("Made::Up::Name"); second != first {
		t.Error("qualified placeholder not reused")
	}
}

// TestYAMLResolveClassNonClassConst covers yamlResolveClass when a name resolves
// to a non-class constant (falls back to a placeholder) and when a placeholder is
// reused on a second load.
func TestYAMLResolveClassEdges(t *testing.T) {
	vm := New(nil)
	vm.cObject.consts["NotAClass"] = object.Integer(5)
	c1 := vm.yamlResolveClass("NotAClass")
	if c1 == nil || c1.name != "NotAClass" {
		t.Fatalf("placeholder for non-class const: %v", c1)
	}
	// A second resolution returns the same placeholder.
	if c2 := vm.yamlResolveClass("NotAClass"); c2 != c1 {
		t.Error("placeholder not reused")
	}
	// A qualified name whose first segment is a non-class const also degrades.
	vm.cObject.consts["X"] = object.Integer(1)
	if c := vm.yamlResolveClass("X::Y"); c == nil || c.name != "X::Y" {
		t.Errorf("qualified non-class: %v", c)
	}
}
