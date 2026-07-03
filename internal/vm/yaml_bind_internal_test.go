// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math/big"
	"testing"
	stdtime "time"

	yaml "github.com/go-ruby-yaml/yaml"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// rubyErr runs fn and returns the RubyError it panics with (failing the test if
// it does not panic with one). It is the white-box analogue of the black-box
// runErr helper, used to assert the binding's raise() paths directly.
func rubyErr(t *testing.T, fn func()) RubyError {
	t.Helper()
	var got RubyError
	func() {
		defer func() {
			re, ok := recover().(RubyError)
			if !ok {
				t.Fatalf("expected a RubyError panic, got %v", recover())
			}
			got = re
		}()
		fn()
	}()
	return got
}

// TestYAMLBindErrMessage covers yamlErrMessage's SyntaxError and generic-error
// branches.
func TestYAMLBindErrMessage(t *testing.T) {
	if got := yamlErrMessage(&yaml.SyntaxError{Message: "boom"}); got != "boom" {
		t.Errorf("SyntaxError message=%q", got)
	}
	if got := yamlErrMessage(errors.New("plain")); got != "plain" {
		t.Errorf("plain error message=%q", got)
	}
}

// TestYAMLBindLoadSyntaxError covers the yamlLoad / yamlSafeLoad error branches:
// a tab character used for indentation makes the library return a SyntaxError,
// which the binding raises as Psych::SyntaxError.
func TestYAMLBindLoadSyntaxError(t *testing.T) {
	vm := New(nil)
	doc := "---\na:\n\t- 1\n" // a tab under "a:" is invalid indentation
	for _, call := range []struct {
		name string
		fn   func()
	}{
		{"load", func() { yamlLoad(vm, doc) }},
		{"safe_load", func() { yamlSafeLoad(vm, doc, nil) }},
		{"safe_load/permitted", func() { yamlSafeLoad(vm, doc, []string{"Foo"}) }},
	} {
		re := rubyErr(t, call.fn)
		if re.Class != "Psych::SyntaxError" {
			t.Errorf("%s: class=%q message=%q", call.name, re.Class, re.Message)
		}
	}
}

// TestYAMLBindDumpUnmapped covers toYAML's default branch (a value with no Psych
// representation) surfacing as the dump TypeError.
func TestYAMLBindDumpUnmapped(t *testing.T) {
	vm := New(nil)
	// A Proc has no library mapping; toYAML returns it as-is and the library's
	// Dump rejects it, which yamlDump turns into a TypeError.
	re := rubyErr(t, func() { yamlDump(vm, object.Wrap(&Proc{})) })
	if re.Class != "TypeError" {
		t.Errorf("class=%q message=%q", re.Class, re.Message)
	}
}

// TestYAMLBindToScalars covers the scalar arms of the rbgo->library mapping that
// the document tests do not all reach directly (bignum, float, plain Go nil).
func TestYAMLBindToScalars(t *testing.T) {
	vm := New(nil)
	c := &yamlToCtx{vm: vm, seen: map[object.Value]yaml.Value{}}
	big30 := object.NormInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil))
	if v, ok := c.conv(big30).(*big.Int); !ok || v.Sign() <= 0 {
		t.Errorf("bignum -> %T %v", c.conv(big30), c.conv(big30))
	}
	if v := c.conv(object.FloatValue(float64(object.Float(1.5)))); v != float64(1.5) {
		t.Errorf("float -> %v", v)
	}
	// A Go-nil object.Value maps to a library nil (the defensive first arm).
	if v := c.conv(object.NilVal()); v != nil {
		t.Errorf("nil -> %v", v)
	}
	// convBound's nil and non-nil arms.
	if v := c.convBound(object.NilVal()); v != nil {
		t.Errorf("convBound(nil) -> %v", v)
	}
	if v := c.convBound(object.IntValue(int64(object.Integer(3)))); v != int64(3) {
		t.Errorf("convBound(3) -> %v", v)
	}
}

// TestYAMLBindToSharedCache covers the identity cache hit on the dump side for a
// shared Array / Hash / Range / RObject / Set referenced twice (the cached
// branches), so each maps to the very same library value (enabling the library's
// anchor emission).
func TestYAMLBindToSharedCache(t *testing.T) {
	vm := New(nil)
	check := func(name string, v object.Value) {
		c := &yamlToCtx{vm: vm, seen: map[object.Value]yaml.Value{}}
		first := c.conv(v)
		second := c.conv(v)
		// For pointer-typed library values, identity is the pointer; for a slice it
		// is the backing array (same len + same first element address).
		switch f := first.(type) {
		case *yaml.Map:
			if f != second.(*yaml.Map) {
				t.Errorf("%s: map not cached", name)
			}
		case *yaml.Range:
			if f != second.(*yaml.Range) {
				t.Errorf("%s: range not cached", name)
			}
		case *yaml.Object:
			if f != second.(*yaml.Object) {
				t.Errorf("%s: object not cached", name)
			}
		case []yaml.Value:
			s := second.([]yaml.Value)
			if len(f) == 0 || len(f) != len(s) || &f[0] != &s[0] {
				t.Errorf("%s: slice not cached", name)
			}
		}
	}
	arr := &object.Array{Elems: []object.Value{object.IntValue(int64(object.Integer(1)))}}
	check("array", object.Wrap(arr))
	h := object.NewHash()
	h.Set(object.Wrap(object.NewString("k")), object.IntValue(int64(object.Integer(1))))
	check("hash", object.Wrap(h))
	check("range", object.Wrap(&object.Range{Lo: object.IntValue(int64(object.Integer(1))), Hi: object.IntValue(int64(object.Integer(5)))}))
	check("object", object.Wrap(&RObject{class: newClass("O", vm.cObject), ivars: map[string]object.Value{"@a": object.IntValue(int64(object.Integer(1)))}}))
	set := newSet()
	set.add(object.IntValue(int64(object.Integer(1))))
	check("set", object.Wrap(set))
}

// TestYAMLBindIvarBareName covers both arms of ivarBareName (a leading "@" is
// stripped; a name without one is returned verbatim).
func TestYAMLBindIvarBareName(t *testing.T) {
	if got := ivarBareName("@x"); got != "x" {
		t.Errorf("@x -> %q", got)
	}
	if got := ivarBareName("y"); got != "y" {
		t.Errorf("y -> %q", got)
	}
	if got := ivarBareName(""); got != "" {
		t.Errorf("empty -> %q", got)
	}
}

// TestYAMLBindFromScalars covers the library->rbgo scalar arms not reached by the
// round-trip tests: a *big.Int, a float64, a time.Time, a Class, a Module, a
// *Regexp, and the unmapped default (nil).
func TestYAMLBindFromScalars(t *testing.T) {
	vm := New(nil)
	c := &yamlFromCtx{vm: vm, seen: map[yaml.Value]object.Value{}}
	bg := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	if v, ok := object.KindOK[*object.Bignum](c.conv(bg)); !ok || v.I.Cmp(bg) != 0 {
		t.Errorf("*big.Int -> %T", c.conv(bg))
	}
	if v, ok := object.AsFloatOK(c.conv(float64(2.5))); !ok || float64(v) != 2.5 {
		t.Errorf("float64 -> %v", c.conv(float64(2.5)))
	}
	tm := c.conv(stdtime.Unix(0, 0).UTC())
	if rt, ok := object.KindOK[*Time](tm); !ok || rt.t.ToUnix() != 0 {
		t.Errorf("time.Time -> %T", tm)
	}
	if cl, ok := object.KindOK[*RClass](c.conv(yaml.Class("String"))); !ok || cl.ToS() != "String" {
		t.Errorf("Class -> %T", c.conv(yaml.Class("String")))
	}
	if md, ok := object.KindOK[*RClass](c.conv(yaml.Module("Comparable"))); !ok || md.ToS() != "Comparable" {
		t.Errorf("Module -> %T", c.conv(yaml.Module("Comparable")))
	}
	if rx, ok := object.KindOK[*Regexp](c.conv(&yaml.Regexp{Source: "ab", Flags: "i"})); !ok || rx.source != "ab" {
		t.Errorf("Regexp -> %T", c.conv(&yaml.Regexp{Source: "ab"}))
	}
	// An unmodelled value maps to nil (the defensive final arm). A bare int (not
	// int64) is never produced by Load, so it exercises the default.
	if v := c.conv(123); !object.IsNil(v) {
		t.Errorf("unmapped -> %v", v)
	}
}

// TestYAMLBindFromRangeAndBound covers the library->rbgo Range mapping and both
// convBound arms (a present bound and a nil beginless / endless bound).
func TestYAMLBindFromRangeAndBound(t *testing.T) {
	vm := New(nil)
	c := &yamlFromCtx{vm: vm, seen: map[yaml.Value]object.Value{}}
	r := c.conv(&yaml.Range{Begin: nil, End: int64(5), Exclusive: true})
	rng, ok := object.KindOK[*object.Range](r)
	if !ok || !object.IsNil(rng.Lo) || !rng.Exclusive {
		t.Fatalf("range -> %T %v", r, r)
	}
	if hi, ok := object.AsIntegerOK(rng.Hi); !ok || int64(hi) != 5 {
		t.Errorf("range end -> %v", rng.Hi)
	}
}

// TestYAMLBindFromSharedCache covers the library->rbgo identity cache hit for a
// shared *Map / *Object referenced twice (the cached branches), so an aliased
// node round-trips to one rbgo instance.
func TestYAMLBindFromSharedCache(t *testing.T) {
	vm := New(nil)
	c := &yamlFromCtx{vm: vm, seen: map[yaml.Value]object.Value{}}
	m := yaml.NewMap()
	m.Set("k", int64(1))
	if c.conv(m) != c.conv(m) {
		t.Error("map not cached on load side")
	}
	o := &yaml.Object{Class: "Foo", IVars: map[string]yaml.Value{"a": int64(1)}}
	if c.conv(o) != c.conv(o) {
		t.Error("object not cached on load side")
	}
	// A bare object (empty class) resolves to the Object class.
	bare := c.conv(&yaml.Object{Class: "", IVars: map[string]yaml.Value{}})
	if ro, ok := object.KindOK[*RObject](bare); !ok || ro.class.name != "Object" {
		t.Errorf("bare object -> %T", bare)
	}
}

// TestYAMLBindResolveClass covers yamlResolveClass through the Class/Module load
// path: a known top-level class is returned, a qualified known class resolves,
// and an unknown name registers a reusable placeholder.
func TestYAMLBindResolveClass(t *testing.T) {
	vm := New(nil)
	if c := vm.yamlResolveClass("String"); c == nil || c.ToS() != "String" {
		t.Errorf("known class -> %v", c)
	}
	outer := newClass("Outer", vm.cObject)
	inner := newClass("Inner", vm.cObject)
	outer.consts["Inner"] = object.Wrap(inner)
	vm.cObject.consts["Outer"] = object.Wrap(outer)
	if c := vm.yamlResolveClass("Outer::Inner"); c != inner {
		t.Errorf("qualified known -> %v", c)
	}
	first := vm.yamlResolveClass("Made::Up")
	if second := vm.yamlResolveClass("Made::Up"); second != first {
		t.Error("placeholder not reused")
	}
	// A name that resolves to a non-class constant degrades to a placeholder.
	vm.cObject.consts["NotAClass"] = object.IntValue(int64(object.Integer(5)))
	if c := vm.yamlResolveClass("NotAClass"); c == nil || c.name != "NotAClass" {
		t.Errorf("non-class const -> %v", c)
	}
	if c := vm.yamlResolveClass("NotAClass"); c == nil || c.name != "NotAClass" {
		t.Error("non-class placeholder not reused")
	}
	// A qualified name whose first segment is a non-class const also degrades.
	if c := vm.yamlResolveClass("NotAClass::Y"); c == nil || c.name != "NotAClass::Y" {
		t.Errorf("qualified non-class -> %v", c)
	}
}

// TestYAMLBindPermittedClassesArg covers permittedClassesArg's branches: no
// trailing args, a trailing non-Hash, a Hash without the keyword, a non-Array
// value, and the populated case with a Class element and a string element.
func TestYAMLBindPermittedClassesArg(t *testing.T) {
	if got := permittedClassesArg(nil); got != nil {
		t.Errorf("no args -> %v", got)
	}
	if got := permittedClassesArg([]object.Value{object.IntValue(int64(object.Integer(1)))}); got != nil {
		t.Errorf("non-hash trailing -> %v", got)
	}
	if got := permittedClassesArg([]object.Value{object.Wrap(object.NewHash())}); got != nil {
		t.Errorf("hash without keyword -> %v", got)
	}
	// permitted_classes whose value is not an Array is ignored.
	h := object.NewHash()
	h.Set(object.SymVal(string(object.Symbol("permitted_classes"))), object.IntValue(int64(object.Integer(7))))
	if got := permittedClassesArg([]object.Value{object.Wrap(h)}); got != nil {
		t.Errorf("non-array value -> %v", got)
	}
	// A populated list: a Class element lists by name, a String element by to_s.
	vm := New(nil)
	h2 := object.NewHash()
	arr := &object.Array{Elems: []object.Value{vm.consts["String"], object.Wrap(object.NewString("Symbol"))}}
	h2.Set(object.SymVal(string(object.Symbol("permitted_classes"))), object.Wrap(arr))
	got := permittedClassesArg([]object.Value{object.Wrap(h2)})
	if len(got) != 2 || got[0] != "String" || got[1] != "Symbol" {
		t.Errorf("populated -> %v", got)
	}
}

// TestYAMLBindFromTimeConstruct guards the time round-trip helper independently
// (a non-zero instant) so the gotime.FromUnix arm is exercised with a real value.
func TestYAMLBindFromTimeConstruct(t *testing.T) {
	vm := New(nil)
	c := &yamlFromCtx{vm: vm, seen: map[yaml.Value]object.Value{}}
	want := int64(1_600_000_000)
	v := c.conv(stdtime.Unix(want, 0).UTC())
	rt, ok := object.KindOK[*Time](v)
	if !ok || rt.t.ToUnix() != want {
		t.Fatalf("time round trip -> %T %v", v, v)
	}
}
