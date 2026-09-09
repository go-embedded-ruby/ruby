package vm_test

import "testing"

// TestSingletonClassVisibilityReset covers the visibility/module_function reset
// that MRI performs on every `class << obj` body entry, mirroring the reset an
// ordinary `class`/`module` (re)open does. A singleton class is persistent (a
// class's cached metaclass, or an object's cached singleton), so without the
// reset a bare `private`/`protected` in one `class << obj` block would leak into
// a later reopened one. Every expected output is the MRI Ruby 4.0.5 result.
func TestSingletonClassVisibilityReset(t *testing.T) {
	checkCases(t, []runCase{
		// A reopened `class << self` starts public again: `b`, defined after a
		// `private` in a PRIOR block, is public; the prior `a` stays private.
		{`class C; class << self; private; def a; end; end; class << self; def b; end; end; end; p C.respond_to?(:b)`, "true\n"},
		{`class C; class << self; private; def a; end; end; class << self; def b; end; end; end; p C.respond_to?(:a)`, "false\n"},

		// protected likewise does not leak into the reopened block.
		{`class C; class << self; protected; def a; end; end; class << self; def b; end; end; end; p C.singleton_class.public_instance_methods(false).include?(:b)`, "true\n"},

		// A per-object singleton (not a class metaclass) resets on reopen too.
		{`o=Object.new; class << o; private; def a; end; end; class << o; def b; end; end; p o.respond_to?(:b)`, "true\n"},
		{`o=Object.new; class << o; private; def a; end; end; class << o; def b; end; end; p o.respond_to?(:a)`, "false\n"},

		// A module's singleton (its class-method table) resets on reopen.
		{`module M; class << self; private; def z; end; end; class << self; def y; end; end; end; p M.respond_to?(:y)`, "true\n"},

		// private_class_method + a fresh `class << self` block: `bar` is public.
		{`class E; def self.foo; end; private_class_method :foo; class << self; def bar; end; end; end; p E.respond_to?(:foo); p E.respond_to?(:bar)`, "false\ntrue\n"},

		// The reset is on the singleton body only: a `private` inside `class << self`
		// does not touch the enclosing (ordinary) body's own default visibility.
		{`class G; class << self; private; end; def self.h; end; end; p G.respond_to?(:h)`, "true\n"},

		// The reset is body-ENTRY only, not per-def: an in-block `private` after a
		// public def still applies to defs that follow it in the SAME block.
		{`class C; class << self; def a; end; private; def b; end; end; end; p C.respond_to?(:a); p C.respond_to?(:b)`, "true\nfalse\n"},

		// A no-arg `public` switches back mid-block after the entry reset.
		{`class C; class << self; private; def a; end; end; class << self; def b; end; public; def c; end; end; end; p [C.respond_to?(:a),C.respond_to?(:b),C.respond_to?(:c)]`, "[false, true, true]\n"},
	})
}
