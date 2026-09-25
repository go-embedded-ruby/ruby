// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
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
