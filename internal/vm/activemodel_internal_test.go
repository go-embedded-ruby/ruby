// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	activemodel "github.com/go-ruby-activemodel/activemodel"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestActiveModelHandleMethods covers the Go-side ToS / Inspect / Truthy of the
// ActiveModel value handles, which the VM reaches only through its default
// formatting fallbacks (Name defines Ruby to_s/inspect, so its Go methods have no
// Ruby trigger).
func TestActiveModelHandleMethods(t *testing.T) {
	name := &ASModelName{n: activemodel.NewName("Person")}
	if name.ToS() != "Person" || name.Inspect() != "#<ActiveModel::Name Person>" || !name.Truthy() {
		t.Errorf("name handle: to_s=%q inspect=%q", name.ToS(), name.Inspect())
	}

	errs := &ASModelErrors{e: activemodel.NewErrors(nil, activemodel.NewName("Person"))}
	if errs.ToS() != "#<ActiveModel::Errors>" || errs.Inspect() != "#<ActiveModel::Errors>" || !errs.Truthy() {
		t.Errorf("errors handle: to_s=%q inspect=%q", errs.ToS(), errs.Inspect())
	}

	e := errs.e.Add("name", activemodel.Symbol("blank"), nil)
	one := &ASModelError{e: e}
	if one.ToS() != "Name can't be blank" || one.Inspect() != "#<ActiveModel::Error attribute=name>" || !one.Truthy() {
		t.Errorf("error handle: to_s=%q inspect=%q", one.ToS(), one.Inspect())
	}
}

// TestActiveModelSet covers the Attr write half (amModel.Set), which ActiveModel's
// read-only validation core never calls, so it has no Ruby trigger.
func TestActiveModelSet(t *testing.T) {
	obj := &RObject{ivars: map[string]object.Value{}}
	m := &amModel{inst: obj}
	m.Set("count", int64(7))
	if got := getIvar(obj, "@count"); got != object.IntValue(7) {
		t.Errorf("Set: @count = %v", got)
	}
}

// TestActiveModelGoOfRuby exhaustively covers the Ruby->Go value mapping,
// including the arms a validator never happens to hit and the terminal to_s
// fallback (a Range).
func TestActiveModelGoOfRuby(t *testing.T) {
	if goOfRuby(object.NilV) != nil {
		t.Error("nil arm")
	}
	if goOfRuby(nil) != nil {
		t.Error("untyped nil arm")
	}
	if goOfRuby(object.Bool(true)) != true {
		t.Error("bool arm")
	}
	if goOfRuby(object.Integer(3)) != int64(3) {
		t.Error("int arm")
	}
	if goOfRuby(object.Float(1.5)) != float64(1.5) {
		t.Error("float arm")
	}
	if goOfRuby(object.NewString("x")) != "x" {
		t.Error("string arm")
	}
	if goOfRuby(object.Symbol("s")) != "s" {
		t.Error("symbol arm")
	}
	arr := goOfRuby(object.NewArrayFromSlice([]object.Value{object.Integer(1)}))
	if s, ok := arr.([]any); !ok || len(s) != 1 || s[0] != int64(1) {
		t.Errorf("array arm: %v", arr)
	}
	h := object.NewHash()
	h.Set(object.Symbol("k"), object.Integer(2))
	m := goOfRuby(h)
	if mm, ok := m.(map[any]any); !ok || mm["k"] != int64(2) {
		t.Errorf("hash arm: %v", m)
	}
	// terminal arm: an unmapped Ruby value degrades to its to_s.
	if got := goOfRuby(object.NewRange(object.Integer(1), object.Integer(3), false)); got != "1..3" {
		t.Errorf("default arm: %v", got)
	}
}

// TestActiveModelRubyOfGo exhaustively covers the Go->Ruby value mapping,
// including the terminal fmt.Sprint fallback.
func TestActiveModelRubyOfGo(t *testing.T) {
	if rubyOfGo(nil) != object.NilV {
		t.Error("nil arm")
	}
	if rubyOfGo(true) != object.Bool(true) {
		t.Error("bool arm")
	}
	if rubyOfGo(int(3)) != object.IntValue(3) {
		t.Error("int arm")
	}
	if rubyOfGo(int64(4)) != object.IntValue(4) {
		t.Error("int64 arm")
	}
	if rubyOfGo(float64(1.5)) != object.Float(1.5) {
		t.Error("float arm")
	}
	if s, ok := rubyOfGo("x").(*object.String); !ok || s.Str() != "x" {
		t.Error("string arm")
	}
	if rubyOfGo(activemodel.Symbol("s")) != object.Symbol("s") {
		t.Error("symbol arm")
	}
	if a, ok := rubyOfGo([]any{int64(1)}).(*object.Array); !ok || len(a.Elems) != 1 {
		t.Error("slice arm")
	}
	if _, ok := rubyOfGo(map[any]any{"k": int64(1)}).(*object.Hash); !ok {
		t.Error("map arm")
	}
	// terminal arm: an unmapped Go value renders through fmt.Sprint.
	if s, ok := rubyOfGo(uint(9)).(*object.String); !ok || s.Str() != "9" {
		t.Errorf("default arm: %v", rubyOfGo(uint(9)))
	}
}

// TestActiveModelStripExtended covers the /x free-spacing lowering, exercising the
// escaped-whitespace, character-class and #-comment branches a simple Ruby pattern
// does not reach.
func TestActiveModelStripExtended(t *testing.T) {
	// "a b" drops the space; "\ " keeps an escaped space; "[ x]" keeps the class
	// whitespace; "# c\n" is a stripped comment; trailing "d" resumes.
	got := amStripExtended("a b\\ c # note\n[ x]d")
	want := "ab\\ c[ x]d"
	if got != want {
		t.Errorf("amStripExtended = %q, want %q", got, want)
	}
}

// TestActiveModelGoFloat covers the numeric-range endpoint coercion, including its
// non-numeric (false) arm.
func TestActiveModelGoFloat(t *testing.T) {
	if f, ok := goFloat(object.Integer(2)); !ok || f != 2 {
		t.Error("int arm")
	}
	if f, ok := goFloat(object.Float(2.5)); !ok || f != 2.5 {
		t.Error("float arm")
	}
	if _, ok := goFloat(object.NewString("x")); ok {
		t.Error("non-numeric arm should be false")
	}
}
