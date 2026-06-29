// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"sort"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file extends the YAML emitter (yaml_dump.go) to serialise arbitrary Ruby
// objects the way Psych does — as a `!ruby/object:ClassName` mapping of their
// instance variables, recursively, plus the `!ruby/range` mapping for a Range.
// It is the generic-object half the report terminus needs: Object#to_yaml and a
// Psych.dump of the report graph (which holds repeated references to resource
// statuses) work by tagging each value and emitting shared / cyclic nodes once
// behind a YAML anchor, referencing them with an alias thereafter.

// isYAMLComplex reports whether v is a complex value the emitter writes with a
// block tag (an ordinary object or a Range). Scalars (including Regexp, which is
// an inline tagged scalar) and plain collections are not complex.
func isYAMLComplex(v object.Value) bool {
	switch v.(type) {
	case *RObject:
		return true
	case *object.Range:
		return true
	case *Set:
		return true
	}
	return false
}

// openTag returns the tag string to write on the opening line of a complex value
// ("!ruby/object:Foo", "!ruby/range"), or "" when v is not a tagged complex
// value. It also reserves an anchor number the first time a shareable reference
// value is seen, so writeComplexChild can decide between "&N <tag>" and "*N".
func (e *yamlEncoder) openTag(v object.Value, _ int) string {
	switch n := v.(type) {
	case *RObject:
		return "!ruby/object" + objectClassSuffix(e.vm, n)
	case *object.Range:
		return "!ruby/range"
	case *Set:
		_ = n
		return "!ruby/object:Set"
	}
	return ""
}

// objectClassSuffix renders the ":ClassName" suffix of a !ruby/object tag, or ""
// for a bare/anonymous Object (Psych writes "!ruby/object" without a suffix when
// the class is Object itself).
func objectClassSuffix(vm *VM, o *RObject) string {
	name := ""
	if o.class != nil {
		name = o.class.name
	}
	if name == "" || name == "Object" {
		return ""
	}
	return ":" + name
}

// tagBodyEmpty reports whether the complex value has no body lines (an object
// with no instance variables), which Psych writes inline as "<tag> {}".
func (e *yamlEncoder) tagBodyEmpty(v object.Value) bool {
	switch n := v.(type) {
	case *RObject:
		return len(n.ivars) == 0
	case *object.Range:
		return false
	case *Set:
		_ = n
		return false // always has a "hash" ivar entry, even when empty
	}
	return false
}

// tagBody builds the Hash whose entries form the block body of a complex value:
// the instance variables of an object (keyed by their bare names, in a stable
// order), or the begin/end/excl triple of a Range.
func (e *yamlEncoder) tagBody(v object.Value) object.Value {
	switch n := v.(type) {
	case *RObject:
		return ivarHash(n.ivars)
	case *object.Range:
		h := object.NewHash()
		h.Set(object.NewString("begin"), rangeBound(n.Lo))
		h.Set(object.NewString("end"), rangeBound(n.Hi))
		h.Set(object.NewString("excl"), object.Bool(n.Exclusive))
		return h
	case *Set:
		// MRI's Set#encode_with writes its backing hash (each element mapped to
		// true) under the "hash" ivar, so it round-trips as a !ruby/object:Set.
		inner := object.NewHash()
		for _, k := range n.order {
			inner.Set(n.vals[k], object.Bool(true))
		}
		h := object.NewHash()
		h.Set(object.NewString("hash"), inner)
		return h
	}
	return object.NewHash()
}

// rangeBound maps a Range endpoint, where a beginless / endless bound is nil.
func rangeBound(v object.Value) object.Value {
	if v == nil {
		return object.NilV
	}
	return v
}

// ivarHash turns an ivar table into an ordered String-keyed Hash (the leading
// "@" stripped), sorted by name for deterministic output since the underlying
// map has no insertion order.
func ivarHash(ivars map[string]object.Value) *object.Hash {
	names := make([]string, 0, len(ivars))
	for k := range ivars {
		names = append(names, k)
	}
	sort.Strings(names)
	h := object.NewHash()
	for _, k := range names {
		h.Set(object.NewString(strings.TrimPrefix(k, "@")), ivars[k])
	}
	return h
}

// writeComplexChild emits a complex value (object / range) as a mapping entry's
// value (after "key:"). It honours shared references via anchors: the first time
// a reference value is written it carries "&N" before its tag; a later sighting
// is written as " *N". Returns false when v is not complex so the scalar /
// collection path handles it.
func (e *yamlEncoder) writeComplexChild(v object.Value, indent int) bool {
	if !isYAMLComplex(v) {
		return false
	}
	if n, ok := e.alias(v); ok {
		e.b.WriteString(" *")
		e.b.WriteString(itoa(n))
		e.b.WriteByte('\n')
		return true
	}
	e.b.WriteByte(' ')
	e.writeAnchorTag(v)
	if e.tagBodyEmpty(v) {
		e.b.WriteString(" {}\n")
		return true
	}
	e.b.WriteByte('\n')
	e.encodeNode(e.tagBody(v), indent+2)
	return true
}

// writeComplexSeqChild emits a complex value following a sequence dash ("- ").
func (e *yamlEncoder) writeComplexSeqChild(v object.Value, indent int) bool {
	if !isYAMLComplex(v) {
		return false
	}
	if n, ok := e.alias(v); ok {
		e.b.WriteString(" *")
		e.b.WriteString(itoa(n))
		e.b.WriteByte('\n')
		return true
	}
	e.b.WriteByte(' ')
	e.writeAnchorTag(v)
	if e.tagBodyEmpty(v) {
		e.b.WriteString(" {}\n")
		return true
	}
	e.b.WriteByte('\n')
	e.encodeNode(e.tagBody(v), indent+2)
	return true
}

// writeAnchorTag writes a value's opening tag, prefixed by "&N " when the value
// is shared (appears more than once in the graph). The anchor number is assigned
// on this first emission so later sightings alias it.
func (e *yamlEncoder) writeAnchorTag(v object.Value) {
	if e.shared(v) {
		e.seq++
		if e.anchors == nil {
			e.anchors = map[object.Value]int{}
		}
		e.anchors[v] = e.seq
		e.b.WriteByte('&')
		e.b.WriteString(itoa(e.seq))
		e.b.WriteByte(' ')
	}
	e.b.WriteString(e.openTag(v, 0))
}

// alias reports whether v has already been emitted under an anchor (so it should
// be written as an alias) and returns that anchor's number.
func (e *yamlEncoder) alias(v object.Value) (int, bool) {
	n, ok := e.anchors[v]
	return n, ok
}

// shared reports whether v occurs more than once in the value graph, in which
// case it must be emitted behind an anchor. The reference count is computed once
// per dump and cached.
func (e *yamlEncoder) shared(v object.Value) bool {
	if e.refcount == nil {
		e.refcount = map[object.Value]int{}
		e.countRefs(e.root, map[object.Value]bool{})
	}
	return e.refcount[v] > 1
}

// countRefs walks the value graph from v, incrementing the reference count of
// every reference-typed (anchorable) node and stopping recursion at a node it has
// already entered (so cycles terminate).
func (e *yamlEncoder) countRefs(v object.Value, seen map[object.Value]bool) {
	if v == nil {
		return
	}
	if !anchorable(v) {
		// Still descend into plain collections to reach the objects they hold.
		switch n := v.(type) {
		case *object.Array:
			for _, el := range n.Elems {
				e.countRefs(el, seen)
			}
		case *object.Hash:
			for _, k := range n.Keys {
				val, _ := n.Get(k)
				e.countRefs(val, seen)
			}
		}
		return
	}
	e.refcount[v]++
	if seen[v] {
		return // already descended through this node; avoid re-walking a cycle
	}
	seen[v] = true
	switch n := v.(type) {
	case *object.Array:
		for _, el := range n.Elems {
			e.countRefs(el, seen)
		}
	case *object.Hash:
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			e.countRefs(val, seen)
		}
	case *RObject:
		for _, iv := range n.ivars {
			e.countRefs(iv, seen)
		}
	case *object.Range:
		e.countRefs(n.Lo, seen)
		e.countRefs(n.Hi, seen)
	case *Set:
		for _, k := range n.order {
			e.countRefs(n.vals[k], seen)
		}
	}
}

// anchorable reports whether v is a reference type that can be shared behind a
// YAML anchor (collections and objects). Scalars are value-typed and never get
// anchors.
func anchorable(v object.Value) bool {
	switch v.(type) {
	case *object.Array, *object.Hash, *RObject, *object.Range, *Set:
		return true
	}
	return false
}
