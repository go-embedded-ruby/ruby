// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestModuleWave20ClassVariables covers the class-variable reflection methods
// searching the whole ancestor chain (an included module, not only the
// superclass chain) — Module#class_variable_get / _set / _defined? and
// #class_variables. Asserted against MRI 4.0.
func TestModuleWave20ClassVariables(t *testing.T) {
	cases := []struct{ src, want string }{
		// class_variable_get sees a variable defined in an INCLUDED module.
		{`module MV; @@mv = :mv; end
c = Class.new { include MV }
p c.class_variable_get(:@@mv)`, ":mv\n"},
		// class_variable_defined? sees an included module's variable.
		{`module MV2; @@mv = 1; end
c = Class.new { include MV2 }
p c.class_variable_defined?(:@@mv)`, "true\n"},
		// class_variable_set writes THROUGH to the module that owns the variable.
		{`module MV3; @@mv = 1; end
c = Class.new { include MV3 }
c.class_variable_set(:@@mv, 99)
p MV3.class_variable_get(:@@mv)`, "99\n"},
		// class_variable_set with no owner in the chain writes on the receiver.
		{`c = Class.new
c.class_variable_set(:@@fresh, 7)
p c.class_variable_get(:@@fresh)`, "7\n"},
		// class_variables (inherit, the default) lists an included module's variable;
		// class_variables(false) is own-only.
		{`module MV4; @@mv = 1; end
c = Class.new { include MV4 }
p [c.class_variables, c.class_variables(false)]`, "[[:@@mv], []]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A class variable defined nowhere in the ancestry is a NameError.
	if err := runErr(t, `Class.new.class_variable_get(:@@nope)`); err == nil ||
		!strings.Contains(err.Error(), "NameError") {
		t.Errorf("missing cvar: got %v, want NameError", err)
	}
}

// TestModuleWave20ExtendObject covers Module#extend_object: it is a private
// instance method that mixes the module into an object's singleton class
// (methods and constants), is undefined on Class, raises TypeError when rebound
// onto a Class receiver, and raises FrozenError for a frozen object. Asserted
// against MRI 4.0.
func TestModuleWave20ExtendObject(t *testing.T) {
	cases := []struct{ src, want string }{
		// extend_object mixes the module's methods AND constants onto an object.
		{`module EM; C = :test; def hi; "hello"; end; end
o = Object.new
EM.send(:extend_object, o)
p [o.hi, o.singleton_class.const_get(:C)]`, `["hello", :test]` + "\n"},
		// It is a private instance method of Module, undefined on Class.
		{`p [Module.private_instance_methods(false).include?(:extend_object),
    Class.private_instance_methods(true).include?(:extend_object)]`, "[true, false]\n"},
		// Extending a class/module object mixes into its singleton (class methods).
		{`module EM2; def cm; :cm; end; end
c = Class.new
EM2.send(:extend_object, c)
p c.cm`, ":cm\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	errs := []struct{ src, want string }{
		// Rebinding onto a Class receiver raises TypeError (Class is not a Module).
		{`Module.instance_method(:extend_object).bind(Class.new).call(Object.new)`, "TypeError"},
		// A frozen object raises FrozenError (a RuntimeError) before extending.
		{`Module.new.send(:extend_object, Object.new.freeze)`, "FrozenError"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestModuleWave20ConstSet covers Module#const_set firing const_added and
// warning on the redefinition of an already-initialised constant (gated by
// $VERBOSE), plus Module#remove_const being a private method. Asserted against
// MRI 4.0.
func TestModuleWave20ConstSet(t *testing.T) {
	cases := []struct{ src, want string }{
		// const_set fires the const_added hook with the new name.
		{`m = Module.new do
  def self.const_added(n); ($ca ||= []) << n; end
end
m.const_set(:Foo, 1)
p $ca`, "[:Foo]\n"},
		// A fresh const_set on a module with no const_added override is a no-op hook.
		{`m = Module.new
m.const_set(:Foo, 1)
p m.const_get(:Foo)`, "1\n"},
		// Redefining an initialised constant warns under $VERBOSE.
		{`require "stringio"
c = Class.new
c.const_set(:X, 1)
$VERBOSE = true
o = $stderr; $stderr = StringIO.new
c.const_set(:X, 2)
w = $stderr.string; $stderr = o
p [c.const_get(:X), w.include?("already initialized constant")]`, "[2, true]\n"},
		// The same redefinition does NOT warn when $VERBOSE is nil.
		{`require "stringio"
c = Class.new
c.const_set(:X, 1)
$VERBOSE = nil
o = $stderr; $stderr = StringIO.new
c.const_set(:X, 2)
w = $stderr.string; $stderr = o
p w.empty?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// remove_const is private: an explicit receiver raises NoMethodError, while
	// #send reaches it.
	if err := runErr(t, `class C; X = 1; end; C.remove_const(:X)`); err == nil ||
		!strings.Contains(err.Error(), "NoMethodError") {
		t.Errorf("remove_const explicit: got %v, want NoMethodError", err)
	}
	if got := eval(t, `class C; X = 1; end; p C.send(:remove_const, :X)`); got != "1\n" {
		t.Errorf("remove_const send: got %q, want %q", got, "1\n")
	}
}

// TestModuleWave20Constants covers the Module.constants SINGLETON method: with
// no argument it returns every top-level constant (equal to Object.constants),
// with an argument it behaves like Module#constants on the receiver. Asserted
// against MRI 4.0.
func TestModuleWave20Constants(t *testing.T) {
	cases := []struct{ src, want string }{
		// No argument: a superset of the core class names, and +1 when a top-level
		// module is added.
		{`p [Module.constants.include?(:Array), Module.constants.include?(:String)]`, "[true, true]\n"},
		{`c = Module.constants.size
module TopLevelAdded20; end
r = Module.constants.size == c + 1
Object.send(:remove_const, :TopLevelAdded20)
p r`, "true\n"},
		// With an argument it is Module#constants on Module itself (own + inherited),
		// so a freshly added Module constant shows up under the false form.
		{`before = Module.constants(false)
class Module; MODULE_C20 = :x; end
r = (Module.constants(false) - before) == [:MODULE_C20]
Module.send(:remove_const, :MODULE_C20)
p r`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestModuleWave20ConstSourceLocation covers Module#const_source_location: rbgo
// does not track constant source positions, so it returns nil for a resolvable
// name, but still enforces MRI's name validation (NameError for a malformed name
// or a Symbol carrying a scope path; TypeError for a failed #to_str). Asserted
// against MRI 4.0.
func TestModuleWave20ConstSourceLocation(t *testing.T) {
	cases := []struct{ src, want string }{
		// A well-formed name (resolvable or not) yields nil (no location tracked).
		{`class C20; K = 1; end; p C20.const_source_location(:K)`, "nil\n"},
		{`p Object.const_source_location(:CS_ABSENT_20)`, "nil\n"},
		// A String or Symbol both accepted; a valid scoped path is well-formed.
		{`p Module.const_source_location("Object")`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	errs := []struct{ src, want string }{
		// A name not starting with a capital letter is a NameError.
		{`Module.const_source_location("name")`, "NameError"},
		// A malformed segment (trailing '=') is a NameError.
		{`Module.const_source_location("K=")`, "NameError"},
		// A Symbol carrying a scope path is a NameError.
		{`Module.const_source_location(:"A::B")`, "NameError"},
		// An empty path segment ("::") is a NameError.
		{`Module.const_source_location("::")`, "NameError"},
		// A non-String, non-Symbol without #to_str is a TypeError.
		{`Module.const_source_location(123)`, "TypeError"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestModuleWave20DefinitionHooks covers Module#method_removed / #method_undefined
// firing on an ordinary class, and the singleton-class variants routing to the
// singleton hooks instead. Also covers Module.new / Class.new yielding the new
// module/class to their block. Asserted against MRI 4.0.
func TestModuleWave20DefinitionHooks(t *testing.T) {
	cases := []struct{ src, want string }{
		// remove_method fires method_removed on an ordinary class.
		{`c = Class.new do
  def self.method_removed(n); ($mr ||= []) << n; end
  def foo; end
end
c.send(:remove_method, :foo)
p $mr`, "[:foo]\n"},
		// undef_method fires method_undefined on an ordinary class.
		{`c = Class.new do
  def self.method_undefined(n); ($mu ||= []) << n; end
  def bar; end
end
c.send(:undef_method, :bar)
p $mu`, "[:bar]\n"},
		// On a singleton class, remove/undef route to the singleton hooks, NOT the
		// module method_removed/method_undefined — so the module counters stay unset.
		{`$smr = nil
o = Object.new
def o.singleton_method_removed(n); $smr = n; end
def o.gone; end
class << o; remove_method :gone; end
p $smr`, ":gone\n"},
		{`$smu = nil
o = Object.new
def o.singleton_method_undefined(n); $smu = n; end
def o.gone; end
class << o; undef_method :gone; end
p $smu`, ":gone\n"},
		// Module.new yields the new module to its block.
		{`m = Module.new { |mod| $mod = mod }
p m.equal?($mod)`, "true\n"},
		// Class.new yields the new class to its block (and to its superclass form).
		{`c = Class.new { |cls| $cls = cls }
p c.equal?($cls)`, "true\n"},
		{`c = Class.new(String) { |cls| $cls2 = cls }
p [c.equal?($cls2), c.superclass]`, "[true, String]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
