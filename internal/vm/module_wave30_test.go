// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestModuleWave30VisibilityArrayArgument covers MRI's single-Array form of
// every directive that routes through set_method_visibility (ruby/ruby v3_4_0
// vm_method.c:2400): private / public / protected and their _class_method
// counterparts all take `[:a, :b]` as the name list. Asserted against MRI 4.0.
func TestModuleWave30VisibilityArrayArgument(t *testing.T) {
	cases := []struct{ src, want string }{
		// private_class_method with an Array argument marks each element.
		{`c = Class.new do
  def self.foo() "foo" end
  private_class_method [:foo]
end
begin; c.foo; p :called; rescue NoMethodError; p :blocked; end`, ":blocked\n"},
		// public_class_method with an Array argument un-marks each element.
		{`c = Class.new do
  def self.bar() "bar" end
  private_class_method :bar
  public_class_method [:bar]
end
p c.bar`, "\"bar\"\n"},
		// Several names in one Array.
		{`c = Class.new do
  def self.a() end
  def self.b() end
  private_class_method [:a, :b]
end
p [c.singleton_methods.sort, (begin; c.a; rescue NoMethodError; :blocked; end)]`,
			"[[], :blocked]\n"},
		// The instance-method directives keep taking an Array too, and still
		// return that array (MRI's set_visibility returns argv[0] when argc == 1).
		{`mod = Module.new do
  def t1() end
  def t2() end
end
p mod.send(:private, [:t1, :t2])
p mod.private_instance_methods(false).sort`, "[:t1, :t2]\n[:t1, :t2]\n"},
		// A non-Array single argument is still a plain name.
		{`c = Class.new do
  def self.solo() end
  private_class_method :solo
end
p (begin; c.solo; rescue NoMethodError; :blocked; end)`, ":blocked\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// An Array element that is not a name is still a TypeError, and an unknown
	// name inside the Array is still a NameError — the Array only supplies the
	// list, it does not relax the per-name checks.
	if err := runErr(t, `Class.new { def self.f; end; private_class_method [123] }`); err == nil ||
		!strings.Contains(err.Error(), "TypeError") {
		t.Errorf("array with a non-name: got %v, want TypeError", err)
	}
	if err := runErr(t, `Class.new { private_class_method [:nope] }`); err == nil ||
		!strings.Contains(err.Error(), "NameError") {
		t.Errorf("array with an unknown name: got %v, want NameError", err)
	}
}

// TestModuleWave30VisibilityAlreadyAtThatLevel covers rb_export_method's
// `if (METHOD_ENTRY_VISI(me) != visi)` guard (ruby/ruby v3_4_0 vm_method.c:1751):
// asking for the level a method already has changes nothing, so no ZSUPER entry
// is cloned onto the receiver and Module#method_added does not fire. Asserted
// against MRI 4.0.
func TestModuleWave30VisibilityAlreadyAtThatLevel(t *testing.T) {
	cases := []struct{ src, want string }{
		// Re-declaring an inherited PRIVATE method private adds nothing to the child.
		{`parent = Module.new { def tm; end; private(:tm) }
child = Module.new { include parent; private(:tm) }
p child.private_instance_methods(false)`, "[]\n"},
		// Same for protected …
		{`parent = Module.new { def tm; end; protected(:tm) }
child = Module.new { include parent; protected(:tm) }
p child.protected_instance_methods(false)`, "[]\n"},
		// … and for public, the default level.
		{`parent = Module.new { def tm; end }
child = Module.new { include parent; public(:tm) }
p child.public_instance_methods(false)`, "[]\n"},
		// A CHANGE of level still records the override on the child.
		{`parent = Module.new { def tm; end }
child = Module.new { include parent; private(:tm) }
p child.private_instance_methods(false)`, "[:tm]\n"},
		// The same, up the superclass chain rather than through an include.
		{`parent = Class.new { def tm; end; private(:tm) }
child = Class.new(parent) { private(:tm) }
p child.private_instance_methods(false)`, "[]\n"},
		// A no-op directive does not fire method_added …
		{`$fired = []
parent = Module.new { def tm; end; private(:tm) }
child = Module.new do
  include parent
  def self.method_added(n) = $fired << n
  private(:tm)
end
p $fired`, "[]\n"},
		// … but a real change does.
		{`$fired = []
parent = Module.new { def tm; end }
child = Module.new do
  include parent
  def self.method_added(n) = $fired << n
  private(:tm)
end
p $fired`, "[:tm]\n"},
		// Re-declaring an OWN method at its current level is also inert, and the
		// method keeps that level.
		{`c = Class.new do
  def own; end
  private :own
  private :own
end
p c.private_instance_methods(false)`, "[:own]\n"},
		// An own method DOES change level when asked for a different one.
		{`c = Class.new do
  def own; end
  private :own
  public :own
end
p [c.private_instance_methods(false), c.public_instance_methods(false)]`,
			"[[], [:own]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A name that resolves nowhere is still a NameError, on a Class …
	if err := runErr(t, `Class.new { private :nope }`); err == nil ||
		!strings.Contains(err.Error(), "NameError") {
		t.Errorf("unknown name on a class: got %v, want NameError", err)
	}
	// … and on a Module whose Object fallback also misses.
	if err := runErr(t, `Module.new { private :definitely_nope_30 }`); err == nil ||
		!strings.Contains(err.Error(), "NameError") {
		t.Errorf("unknown name on a module: got %v, want NameError", err)
	}
	// A Module reaches Object for a name its own ancestors lack (the
	// rb_export_method rb_cObject fallback) and records the override there — but
	// only when Object's entry is at a DIFFERENT level: Kernel#puts is already
	// private, so `private :puts` in a module body adds nothing.
	if got := eval(t, `m = Module.new { private :frozen? }
p m.private_instance_methods(false)`); got != "[:frozen?]\n" {
		t.Errorf("Object fallback: got %q", got)
	}
	if got := eval(t, `m = Module.new { private :puts }
p m.private_instance_methods(false)`); got != "[]\n" {
		t.Errorf("Object fallback, same level: got %q", got)
	}
}

// TestModuleWave30ModuleDup covers Module#dup / #clone / #initialize_copy.
// rbgo's dupValue has no class case, so Object#dup used to hand back the
// RECEIVER for a module or class; MRI's rb_mod_init_copy (ruby/ruby v3_4_0
// class.c) allocates a fresh one and copies its tables. Asserted against MRI 4.0.
func TestModuleWave30ModuleDup(t *testing.T) {
	cases := []struct{ src, want string }{
		// A copy is a DIFFERENT object, for a module and for a class.
		{`m = Module.new; p m.dup.equal?(m)`, "false\n"},
		{`c = Class.new; p c.dup.equal?(c)`, "false\n"},
		{`m = Module.new; p m.clone.equal?(m)`, "false\n"},
		// Its constant table is independent of the original's.
		{`m = Module.new
m.const_set(:A, 1)
d = m.dup
m.const_set(:B, 2)
d.const_set(:C, 3)
p [d.const_defined?(:A, false), d.const_defined?(:B, false), m.const_defined?(:C, false)]`,
			"[true, false, false]\n"},
		// So is its method table, and a copied method still finds `super`.
		{`class W30P; def v; :p; end; end
class W30C < W30P; def v; [:c, super]; end; end
d = W30C.dup
d.send(:define_method, :extra) { :extra }
p [d.new.v, W30C.method_defined?(:extra)]`, "[[:c, :p], false]\n"},
		// Class methods, instance variables and the superclass come across.
		{`class W30S
  def self.sm; :sm; end
  @iv = 7
  def im; :im; end
end
d = W30S.dup
p [d.sm, d.instance_variable_get(:@iv), d.superclass, d.new.im]`,
			"[:sm, 7, Object, :im]\n"},
		// The copy is ANONYMOUS: MRI never copies the classpath.
		{`module W30Named; end
p W30Named.dup.name`, "nil\n"},
		{`module W30Named2; end
p W30Named2.clone.name`, "nil\n"},
		// The mixin chain comes across.
		{`module W30M; def mm; :mm; end; end
k = Class.new { include W30M }
d = k.dup
p [d.new.mm, d.include?(W30M)]`, "[:mm, true]\n"},
		// A prepended module keeps its place ahead of the copy's own method.
		{`module W30Pre; def hi; "pre-" + super; end; end
class W30Base; def hi; "base"; end; prepend W30Pre; end
p W30Base.dup.new.hi`, "\"pre-base\"\n"},
		// A module mixed in with #extend lives on the METACLASS, and comes across
		// with it (the jruby/jruby#3686 case ruby/spec pins for Struct).
		{`k = Struct.new(:foo)
mod = Module.new { def hello; "hello"; end }
k.extend(mod)
p k.dup.hello`, "\"hello\"\n"},
		// A Struct subclass keeps its member layout (MRI carries the allocator).
		{`s = Struct.new(:a, :b).dup
x = s.new(1, 2)
p [x.a, x.b, s.members]`, "[1, 2, [:a, :b]]\n"},
		// A Data subclass too.
		{`d = Data.define(:q).dup
p d.new(q: 5).q`, "5\n"},
		// A pending autoload is shared with the copy (ruby/spec relies on this).
		{`m = Module.new
m.send(:autoload, :T, "/nonexistent/w30.rb")
p m.dup.autoload?(:T)`, "\"/nonexistent/w30.rb\"\n"},
		// Visibility overrides and private-constant marks come across.
		{`parent = Module.new { def tm; end }
m = Module.new
m.send(:include, parent)
m.send(:private, :tm)
m.const_set(:X, 1)
m.send(:private_constant, :X)
d = m.dup
p [d.private_instance_methods(false), (begin; d::X; rescue NameError; :priv; end)]`,
			"[[:tm], :priv]\n"},
		// dup leaves the copy unfrozen; clone copies the frozen state, and
		// freeze: overrides it either way.
		{`f = Class.new.freeze
p [f.dup.frozen?, f.clone.frozen?, f.clone(freeze: false).frozen?]`,
			"[false, true, false]\n"},
		{`c = Class.new
p [c.clone.frozen?, c.clone(freeze: true).frozen?, c.clone(freeze: nil).frozen?]`,
			"[false, true, false]\n"},
		// initialize_copy from the receiver itself is inert.
		{`m = Module.new
p m.send(:initialize_copy, m).equal?(m)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A singleton class refuses to be copied, through dup and through clone …
	for _, src := range []string{
		`Class.new.singleton_class.dup`,
		`Class.new.singleton_class.clone`,
		`Object.new.singleton_class.dup`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "can't copy singleton class") {
			t.Errorf("%s: got %v, want TypeError can't copy singleton class", src, err)
		}
	}
	// … and so does BasicObject, the root class.
	if err := runErr(t, `BasicObject.dup`); err == nil ||
		!strings.Contains(err.Error(), "can't copy the root class") {
		t.Errorf("BasicObject.dup: got %v, want TypeError can't copy the root class", err)
	}
	// initialize_copy needs a module/class argument …
	if err := runErr(t, `Module.new.send(:initialize_copy, 1)`); err == nil ||
		!strings.Contains(err.Error(), "initialize_copy should take same class object") {
		t.Errorf("non-module original: got %v, want TypeError", err)
	}
	// … and refuses to write into a frozen receiver.
	if err := runErr(t, `Module.new.freeze.send(:initialize_copy, Module.new)`); err == nil ||
		!strings.Contains(err.Error(), "FrozenError") {
		t.Errorf("frozen receiver: got %v, want FrozenError", err)
	}
	// clone rejects an unknown keyword and a non-boolean freeze:.
	if err := runErr(t, `Class.new.clone(nope: 1)`); err == nil ||
		!strings.Contains(err.Error(), "unknown keyword") {
		t.Errorf("unknown keyword: got %v, want ArgumentError", err)
	}
	if err := runErr(t, `Class.new.clone(freeze: 1)`); err == nil ||
		!strings.Contains(err.Error(), "unexpected value for freeze") {
		t.Errorf("bad freeze value: got %v, want ArgumentError", err)
	}
}

// TestModuleWave30ConstSourceLocation covers Module#const_source_location's
// SEARCH and its nil / [] / NameError contract — MRI's rb_mod_const_source_location
// (ruby/ruby v3_4_0 object.c) over rb_const_location (variable.c). rbgo records
// no definition site yet, so a constant that resolves answers [] (MRI's answer
// for one defined in C); what is asserted here is which names resolve at all.
// Asserted against MRI 4.0, comparing nil / empty / non-nil rather than the
// path.
func TestModuleWave30ConstSourceLocation(t *testing.T) {
	// A constant this VM defines in Go answers [], exactly as MRI does for one
	// defined in C.
	cases := []struct{ src, want string }{
		{`p Object.const_source_location(:String)`, "[]\n"},
		// An absent name is nil, a present one is not.
		{`module W30A; K = 1; end
p [W30A.const_source_location(:K), W30A.const_source_location(:NOPE)]`, "[[], nil]\n"},
		// With inherit (the default) a class reads its superclass …
		{`class W30SP; P1 = 1; end
class W30SC < W30SP; end
p [W30SC.const_source_location(:P1), W30SC.const_source_location(:P1, false)]`,
			"[[], nil]\n"},
		// … and Object, which is how a class receiver reports a toplevel constant.
		{`W30TOP = 1
class W30SC2; end
p [W30SC2.const_source_location(:W30TOP), W30SC2.const_source_location(:W30TOP, false)]`,
			"[[], nil]\n"},
		// A MODULE receiver never reaches Object through its ancestors, so MRI
		// searches Object explicitly — but only when inherit is set.
		{`W30TOP2 = 1
module W30MM; end
p [W30MM.const_source_location(:W30TOP2), W30MM.const_source_location(:W30TOP2, false)]`,
			"[[], nil]\n"},
		// An included module is on the search path.
		{`module W30Mix; MX = 1; end
class W30Host; include W30Mix; end
p W30Host.const_source_location(:MX)`, "[]\n"},
		// A leading "::" reads the top level.
		{`W30TOP3 = 1
module W30MM3; end
p W30MM3.const_source_location("::W30TOP3")`, "[]\n"},
		// A scoped path resolves every segment but the last with const_get, and
		// the inherit flag is spent on the FIRST segment: the last one reads the
		// reached module's OWN table, so an inherited name there is nil.
		{`module W30Outer
  module W30Inner; J = 2; end
end
class W30IP; IPC = 1; end
class W30IC < W30IP; end
p [W30Outer.const_source_location("W30Inner::J"), Object.const_source_location("W30IC::IPC")]`,
			"[[], nil]\n"},
		// The singleton class of a class is NOT searched.
		{`class W30S2; class << self; SC = 1; end; end
p W30S2.const_source_location(:SC)`, "nil\n"},
		// Nor is the lexically containing scope.
		{`module W30Cont
  CONT = 1
  class W30Child; end
end
p W30Cont::W30Child.const_source_location(:CONT)`, "nil\n"},
		// A private constant IS reported: rb_const_location passes visibility=0.
		{`module W30Priv; end
W30Priv.const_set(:PC, 1)
W30Priv.send(:private_constant, :PC)
p W30Priv.const_source_location(:PC)`, "[]\n"},
		// A pending autoload counts as an entry (MRI reserves the name).
		{`module W30Auto; end
W30Auto.send(:autoload, :LZ, "/nonexistent/w30auto.rb")
p W30Auto.const_source_location(:LZ)`, "[]\n"},
		// The name is coerced through #to_str.
		{`module W30Str; SK = 1; end
n = Object.new
def n.to_str = "SK"
p W30Str.const_source_location(n)`, "[]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A leading segment that names a non-module is a TypeError, as in
	// rb_mod_const_source_location's "does not refer to class/module".
	if err := runErr(t, `W30NOTMOD = 1
Object.const_source_location("W30NOTMOD::X")`); err == nil ||
		!strings.Contains(err.Error(), "does not refer to class/module") {
		t.Errorf("non-module segment: got %v, want TypeError", err)
	}
	// Every malformed name MRI rejects.
	for _, src := range []string{
		`Module.const_source_location "name"`,
		`Module.const_source_location "__CONSTX__"`,
		`Module.const_source_location "@K"`,
		`Module.const_source_location "!K"`,
		`Module.const_source_location "K="`,
		`Module.const_source_location "K?"`,
		`Module.const_source_location :"::K"`,
		`Module.const_source_location :"A::B"`,
		`Module.const_source_location "::"`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "NameError") {
			t.Errorf("%s: got %v, want NameError", src, err)
		}
	}
	// A non-name argument is a TypeError …
	if err := runErr(t, `Module.const_source_location(123)`); err == nil ||
		!strings.Contains(err.Error(), "TypeError") {
		t.Errorf("Integer name: got %v, want TypeError", err)
	}
	// … and the arity is 1..2 (rb_check_arity).
	for _, src := range []string{`Module.const_source_location`, `Module.const_source_location(:K, true, 3)`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
			t.Errorf("%s: got %v, want ArgumentError", src, err)
		}
	}
}

// TestModuleWave30ConstantNameShape covers the constant-name test every
// constant entry point shares — const_set / const_get / const_defined? /
// remove_const / autoload / const_source_location. It is MRI's
// rb_enc_symname_type (ruby/ruby v3_4_0 symbol.c): rb_sym_constant_char_p for
// the leading character, is_identchar for every byte after it. Asserted against
// MRI 4.0.
func TestModuleWave30ConstantNameShape(t *testing.T) {
	// Names MRI accepts. Each is const_set on a fresh module and read back.
	for _, name := range []string{
		`CS_A`, // the ordinary shape
		`A`,    // one character
		`A1_b`, // digits, underscore and lowercase after the lead
		"Aλ",   // a non-ASCII LETTER after the lead
		"A€",   // a non-ASCII non-letter: is_identchar takes any non-ASCII byte
		"A B",  // including a non-breaking space
		"ΛX",   // an uppercase multi-byte lead
		"Ǆx",   // U+01C4 DŽ, uppercase
		"ǅx",   // U+01C5 Dž, TITLECASE — rb_sym_constant_char_p's titlecase arm
	} {
		src := "m = Module.new\nm.const_set(" + rubyStringLit(name) + ", 7)\np m.const_get(" + rubyStringLit(name) + ")\n"
		if got := eval(t, src); got != "7\n" {
			t.Errorf("const_set(%q): got %q, want 7", name, got)
		}
	}
	// A name in a non-UTF-8 ASCII-compatible encoding is a name too: its bytes
	// are not valid UTF-8, and MRI never decodes them.
	if got := eval(t, `m = Module.new
s = "CS_CONSTλ".encode("euc-jp")
m.const_set(s, 7)
p [m.const_get(s), m.const_defined?(s)]`); got != "[7, true]\n" {
		t.Errorf("EUC-JP constant name: got %q", got)
	}
	// Names MRI refuses.
	for _, name := range []string{
		`name`, // lowercase lead
		`_A`,   // underscore lead
		`A b`,  // a space after the lead
		`A-`,   // punctuation after the lead
		`A=`,   // the attrset shape is not a constant
		`A?`,
		"λX", // a LOWERCASE multi-byte lead
		"ǆx", // U+01C6 dž, lowercase
	} {
		src := "Module.new.const_set(" + rubyStringLit(name) + ", 7)"
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "NameError") {
			t.Errorf("const_set(%q): got %v, want NameError", name, err)
		}
	}
	// A multi-byte lead in a non-UTF-8 encoding is not a constant character
	// either — nothing MRI calls uppercase lives in those tables.
	if err := runErr(t, `Module.new.const_set("λX".encode("euc-jp"), 7)`); err == nil ||
		!strings.Contains(err.Error(), "NameError") {
		t.Errorf("EUC-JP lowercase lead: got %v, want NameError", err)
	}
	// The empty name.
	if err := runErr(t, `Module.new.const_set("", 7)`); err == nil ||
		!strings.Contains(err.Error(), "NameError") {
		t.Errorf("empty name: got %v, want NameError", err)
	}
}

// rubyStringLit renders s as a Ruby double-quoted string literal with every
// non-ASCII byte escaped, so a test source stays plain ASCII whatever the name
// under test contains.
func rubyStringLit(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < utf8.RuneSelf && c != '"' && c != '\\' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, `\x%02X`, c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TestModuleWave30Ruby2Keywords covers Module#ruby2_keywords' MRI-visible
// contract — rb_mod_ruby2_keywords, ruby/ruby v3_4_0 vm_method.c:2568, with
// 4.0's wording for the parameter-shape warning. The flag itself is not carried
// yet (bytecode.ISeq has no param.flags.ruby2_keywords), so what is asserted
// here is everything that does not depend on it. Asserted against MRI 4.0.
func TestModuleWave30Ruby2Keywords(t *testing.T) {
	cases := []struct{ src, want string }{
		// It returns nil, and is silent for a method it could mark.
		{`o = Object.new
o.singleton_class.class_exec do
  def foo(*a) end
  p ruby2_keywords(:foo)
end`, "nil\n"},
		// It is a PRIVATE instance method of Module.
		{`p Module.private_instance_methods(false).include?(:ruby2_keywords)`, "true\n"},
		{`p (Module.new.ruby2_keywords(:x) rescue $!.class)`, "NoMethodError\n"},
		// A String name is accepted as well as a Symbol.
		{`o = Object.new
o.singleton_class.class_exec do
  def foo(*a) end
  p ruby2_keywords("foo")
end`, "nil\n"},
		// Several names in one call.
		{`c = Class.new do
  def a(*x) end
  def b(*x) end
  p ruby2_keywords(:a, :b)
end`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// Each shape MRI warns about, with its own wording. $VERBOSE is assigned so
	// rb_warn's gate is open, and $stderr is captured.
	for _, w := range []struct{ def, want string }{
		// has_rest missing …
		{`def foo(a, b, c) end`, "method accepts keywords or post arguments or method does not accept argument splat"},
		// … a required keyword …
		{`def foo(*a, b:) end`, "method accepts keywords or post arguments or method does not accept argument splat"},
		// … a keyword splat …
		{`def foo(*a, **b) end`, "method accepts keywords or post arguments or method does not accept argument splat"},
		// … and, since 4.0, a post-splat positional.
		{`def foo(*a, b) end`, "method accepts keywords or post arguments or method does not accept argument splat"},
	} {
		src := `$VERBOSE = false
require "stringio"
$stderr = StringIO.new
c = Class.new do
  ` + w.def + `
  ruby2_keywords :foo
end
out = $stderr.string
$stderr = STDERR
p out.include?("Skipping set of ruby2_keywords flag for foo (` + w.want + `)")`
		if got := eval(t, src); got != "true\n" {
			t.Errorf("warning for %q: got %q", w.def, got)
		}
	}
	// A method whose body is not Ruby (an attr reader) …
	if got := eval(t, `$VERBOSE = false
require "stringio"
$stderr = StringIO.new
c = Class.new do
  attr_reader :a
  ruby2_keywords :a
end
out = $stderr.string
$stderr = STDERR
p out.include?("Skipping set of ruby2_keywords flag for a (method not defined in Ruby)")`); got != "true\n" {
		t.Errorf("non-Ruby method warning: got %q", got)
	}
	// … and one the receiver does not define itself, reached through the
	// superclass or (for a module) through Object.
	for _, src := range []string{
		`class W30R2KBase; def sp(*a) end; end
class W30R2KSub < W30R2KBase; end
W30R2KSub.class_exec { ruby2_keywords :sp }`,
		`Module.new.class_exec { ruby2_keywords :puts }`,
	} {
		full := `$VERBOSE = false
require "stringio"
$stderr = StringIO.new
` + src + `
out = $stderr.string
$stderr = STDERR
p out.include?("can only set in method defining module")`
		if got := eval(t, full); got != "true\n" {
			t.Errorf("inherited-name warning (%s): got %q", src, got)
		}
	}
	// No argument, a bad name, an unknown name and a frozen receiver all raise.
	for _, tc := range []struct{ src, want string }{
		{`Module.new.send(:ruby2_keywords)`, "ArgumentError"},
		{`Class.new { ruby2_keywords Object.new }`, "is not a symbol nor a string"},
		{`Class.new { ruby2_keywords :not_existing_w30 }`, "undefined method 'not_existing_w30'"},
		{`Module.new.freeze.send(:ruby2_keywords, :x)`, "FrozenError"},
	} {
		if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %s", tc.src, err, tc.want)
		}
	}
}

// TestModuleWave30AutoloadRetiredByDirectRequire covers an autoload entry that
// is settled by a DIRECT require of its file rather than by the autoload
// itself. MRI's const_tbl_update replaces the autoload entry the moment the
// constant gets a value, so Module#autoload? answers nil for good; rbgo retires
// the entry at the first read that finds the constant defined. Asserted against
// MRI 4.0.
func TestModuleWave30AutoloadRetiredByDirectRequire(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "w30_autoload_direct.rb")
	if err := os.WriteFile(path, []byte("W30Direct::K = :loaded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lit := strconv.Quote(path)
	// Before the require the entry is pending; after it, and after $" is
	// restored and the constant removed, a FRESH autoload for the same file is
	// live again — the settled entry did not linger to mask it.
	src := `module W30Direct; end
saved = $".dup
W30Direct.autoload(:K, ` + lit + `)
a = W30Direct.autoload?(:K)
require ` + lit + `
b = W30Direct.autoload?(:K)
$".replace saved
W30Direct.send(:remove_const, :K)
W30Direct.autoload(:K, ` + lit + `)
p [a == ` + lit + `, b, W30Direct.autoload?(:K) == ` + lit + `]`
	if got := eval(t, src); got != "[true, nil, true]\n" {
		t.Errorf("direct require of an autoload's file: got %q", got)
	}
	// Reading it back through the retired entry does not re-run the file, and
	// Module#constants keeps listing a pending name.
	src2 := `module W30Direct2; end
W30Direct2.autoload(:K, ` + lit + `)
p [W30Direct2.constants(false), W30Direct2.const_defined?(:K, false)]`
	if got := eval(t, src2); got != "[[:K], true]\n" {
		t.Errorf("pending autoload listing: got %q", got)
	}
}
