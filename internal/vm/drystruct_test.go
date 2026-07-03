// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	drystruct "github.com/go-ruby-dry-struct/dry-struct"
	drytypes "github.com/go-ruby-dry-types/dry-types"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestDryStructBasic covers a Dry::Struct subclass: attribute declaration,
// coercing .new, generated readers, to_h, and the error class.
func TestDryStructBasic(t *testing.T) {
	prelude := `require "dry/types"
require "dry/struct"
class User < Dry::Struct
  attribute :name, Dry::Types["strict.string"]
  attribute :age, Dry::Types["coercible.integer"]
end
`
	cases := []struct{ src, want string }{
		{prelude + `u = User.new(name: "Ada", age: "30"); p u.name`, "\"Ada\"\n"},
		{prelude + `u = User.new(name: "Ada", age: "30"); p u.age`, "30\n"},
		{prelude + `u = User.new(name: "Ada", age: 1); p u.to_h`, "{name: \"Ada\", age: 1}\n"},
		{prelude + `u = User.new(name: "Ada", age: 1); p u.to_hash`, "{name: \"Ada\", age: 1}\n"},
		{prelude + `u = User.new(name: "Ada", age: 1); p u.attributes`, "{name: \"Ada\", age: 1}\n"},
		{prelude + `u = User.new(name: "Ada", age: 1); p u[:name]`, "\"Ada\"\n"},
		{prelude + `u = User.new(name: "Ada", age: 1); p u.inspect.include?("User")`, "true\n"},
		{prelude + `u = User.new(name: "Ada", age: 1); p u.to_s.include?("User")`, "true\n"},
		// with returns a new struct with a replaced attribute.
		{prelude + `u = User.new(name: "Ada", age: 1); p u.with(age: 2).age`, "2\n"},
		// with no arguments returns an equal copy (empty changes).
		{prelude + `u = User.new(name: "Ada", age: 1); p u.with == u`, "true\n"},
		// == / eql? compare by value.
		{prelude + `a = User.new(name: "x", age: 1); b = User.new(name: "x", age: 1); p a == b`, "true\n"},
		{prelude + `a = User.new(name: "x", age: 1); b = User.new(name: "y", age: 1); p a.eql?(b)`, "false\n"},
		{prelude + `a = User.new(name: "x", age: 1); p a == 5`, "false\n"},
		{prelude + `a = User.new(name: "x", age: 1); p a.send(:eql?)`, "false\n"},
		// Error tree.
		{prelude + `p Dry::Struct::Error < StandardError`, "true\n"},
		// Dry::Struct::Value is an alias of the base.
		{prelude + `p Dry::Struct::Value.equal?(Dry::Struct)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryStructOptionalAndTransform covers attribute? (optional), transform_keys,
// no-arg .new (empty input) and a nil reader for an absent optional attribute.
func TestDryStructOptionalAndTransform(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dry/types"
require "dry/struct"
class P < Dry::Struct
  attribute? :nick, Dry::Types["strict.string"]
end
p P.new({}).nick`, "nil\n"},
		{`require "dry/types"
require "dry/struct"
class Q < Dry::Struct
  transform_keys :symbolize
  attribute :a, Dry::Types["strict.integer"]
end
p Q.new("a" => 1).a`, "1\n"},
		{`require "dry/types"
require "dry/struct"
class R < Dry::Struct
  transform_keys :stringify
  attribute :a, Dry::Types["strict.integer"]
end
p R.new(a: 1).a`, "1\n"},
		// transform_keys with no/unknown arg falls back to KeyNone.
		{`require "dry/types"
require "dry/struct"
class S < Dry::Struct
  transform_keys
  attribute :a, Dry::Types["strict.integer"]
end
p S.new(a: 1).a`, "1\n"},
		// no-arg .new builds from an empty map.
		{`require "dry/types"
require "dry/struct"
class E < Dry::Struct
end
p E.new.to_h`, "{}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryStructNested covers a nested struct attribute and struct-typed
// pass-through (.new given an existing instance of the same type).
func TestDryStructNested(t *testing.T) {
	prelude := `require "dry/types"
require "dry/struct"
class Addr < Dry::Struct
  attribute :city, Dry::Types["strict.string"]
end
class Person < Dry::Struct
  attribute :addr, Addr
end
`
	cases := []struct{ src, want string }{
		{prelude + `p Person.new(addr: {city: "Paris"}).addr.city`, "\"Paris\"\n"},
		// An existing struct passes through .new unchanged.
		{prelude + `a = Addr.new(city: "Lyon"); p Person.new(addr: {city: "Lyon"}).addr == a`, "true\n"},
		{prelude + `a = Addr.new(city: "X"); p Addr.new(a).city`, "\"X\"\n"},
		// optional nested attribute (attribute?).
		{`require "dry/types"
require "dry/struct"
class B < Dry::Struct
  attribute :n, Dry::Types["strict.integer"]
end
class C < Dry::Struct
  attribute? :b, B
end
p C.new({}).b`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryStructInheritance covers a subclass of a struct subclass inheriting the
// parent's attributes.
func TestDryStructInheritance(t *testing.T) {
	src := `require "dry/types"
require "dry/struct"
class Base < Dry::Struct
  attribute :a, Dry::Types["strict.integer"]
end
class Sub < Base
  attribute :b, Dry::Types["strict.integer"]
end
u = Sub.new(a: 1, b: 2)
p [u.a, u.b]`
	if got := eval(t, src); got != "[1, 2]\n" {
		t.Errorf("inheritance: %q", got)
	}
}

// TestDryStructErrors covers the coercion-failure and argument-guard paths.
func TestDryStructErrors(t *testing.T) {
	prelude := `require "dry/types"
require "dry/struct"
class U < Dry::Struct
  attribute :name, Dry::Types["strict.string"]
end
`
	// Missing / invalid attribute raises Dry::Struct::Error.
	if class, _ := evalErr(t, prelude+`U.new({})`); class != "Dry::Struct::Error" {
		t.Errorf("missing attr class %q", class)
	}
	if class, _ := evalErr(t, prelude+`U.new(name: 5)`); class != "Dry::Struct::Error" {
		t.Errorf("invalid attr class %q", class)
	}
	// with an unknown/invalid change raises.
	if class, _ := evalErr(t, prelude+`U.new(name: "x").with(name: 5)`); class != "Dry::Struct::Error" {
		t.Errorf("with-invalid class %q", class)
	}
	// [] on an unknown attribute raises.
	if class, _ := evalErr(t, prelude+`U.new(name: "x")[:nope]`); class != "Dry::Struct::Error" {
		t.Errorf("[] unknown class %q", class)
	}
	// [] and attribute arity.
	if class, _ := evalErr(t, prelude+`U.new(name: "x").send(:[])`); class != "ArgumentError" {
		t.Errorf("[] arity class %q", class)
	}
	// attribute with too few arguments.
	if class, _ := evalErr(t, prelude+`class V < Dry::Struct; attribute :x; end`); class != "ArgumentError" {
		t.Errorf("attribute arity class %q", class)
	}
	// attribute with a bad type raises TypeError.
	if class, _ := evalErr(t, prelude+`class W < Dry::Struct; attribute :x, 5; end`); class != "TypeError" {
		t.Errorf("attribute bad-type class %q", class)
	}
}

// TestDryStructValueMeta covers the DryStruct display methods and the opaque
// schema-holder value's own display methods (never surfaced to Ruby, but part of
// the object.Value contract).
func TestDryStructValueMeta(t *testing.T) {
	m := &dryStructMeta{st: drystruct.New("X")}
	if m.ToS() == "" || m.Inspect() == "" || !m.Truthy() {
		t.Error("dryStructMeta display")
	}
	empty := drystruct.New("Z")
	inst, err := empty.New(nil)
	if err != nil {
		t.Fatalf("empty struct: %v", err)
	}
	ds := &DryStruct{s: inst, cls: nil}
	if ds.ToS() == "" || ds.Inspect() == "" || !ds.Truthy() {
		t.Error("DryStruct display")
	}
}

// TestDryStructDirectGuards covers the branches unreachable through the Ruby
// surface: attribute called on a non-class receiver, dryStructType's anonymous-
// name fallback, and setDryStructType initialising a nil ivar table.
func TestDryStructDirectGuards(t *testing.T) {
	vm := New(io.Discard)

	// attribute on a non-RClass self raises TypeError.
	mustRaise(t, "TypeError", func() {
		vm.dryStructAttribute(object.NilVal(), []object.Value{object.SymVal(string(object.Symbol("x"))), object.Wrap(&DryType{t: drytypes.StrictInteger()})}, false)
	})

	// An anonymous class (empty name) gets the "Dry::Struct" fallback name and a
	// fresh ivar table via setDryStructType.
	anon := newClass("", vm.cObject)
	anon.ivars = nil
	st := vm.dryStructType(anon)
	if st == nil {
		t.Fatal("anon struct type nil")
	}
	if anon.ivars == nil {
		t.Error("setDryStructType did not init ivars")
	}
}
