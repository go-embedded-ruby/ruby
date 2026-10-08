package vm_test

import "testing"

// TestFreezeStopsDef: freeze is a hardening boundary -- a library freezes a
// module to say "do not monkey-patch me after boot" -- and rbgo refused every
// REFLECTIVE definer (define_method, attr_accessor, remove_method, const_set)
// while letting the `def` keyword straight through (#798). A test of the
// protection written with define_method therefore passed while the protection
// was absent, which is why these cases are written with `def`.
//
// Every expectation here was measured against MRI
// `ruby 4.0.7 (2026-09-15 revision 229531a6cf) +PRISM [arm64-darwin25]`.
func TestFreezeStopsDef(t *testing.T) {
	checkCases(t, []runCase{
		// def on a frozen object's singleton: MRI names the OBJECT, not the
		// anonymous singleton class.
		{`o = Object.new.freeze
begin; o.instance_eval("def foo; end"); rescue FrozenError => e; puts e.message.split(":").first; end`,
			"can't modify frozen Object\n"},
		{`o = Object.new.freeze
begin; o.define_singleton_method(:foo) {}; rescue FrozenError => e; puts e.message.split(":").first; end`,
			"can't modify frozen Object\n"},
		// def into a frozen class, through the class body.
		{`c = Class.new.freeze
begin; c.class_eval { def z; end }; rescue FrozenError => e; puts e.message.split(":").first; end`,
			"can't modify frozen Class\n"},
		// `class << c` reaches the metaclass, whose own frozen flag is false: the
		// flag that counts is the class's, through metaOf. A first version of the
		// fix handled only the per-object `attached` field and this went through.
		{`c = Class.new.freeze
begin; class << c; def bar; end; end; rescue FrozenError => e; puts e.message.split(":").first; end`,
			"can't modify frozen Class\n"},
		// def self.foo on a frozen class.
		{`c = Class.new.freeze
begin; c.instance_eval("def self.foo; end"); rescue FrozenError => e; puts e.message.split(":").first; end`,
			"can't modify frozen Class\n"},
		// A frozen module reports "Module", which is MRI 4.0's capitalised
		// wording (3.x said "module"; ruby/spec carries the distinction as
		// `ruby_version_is("4.0") ? "Module" : "module"`).
		{`m = Module.new.freeze
begin; m.module_eval { def z; end }; rescue FrozenError => e; puts e.message.split(":").first; end`,
			"can't modify frozen Module\n"},
		// The precedence that makes the check's PLACEMENT matter: every immediate
		// reports frozen, and MRI answers those with TypeError, so the frozen test
		// has to run after the can-this-have-a-singleton test, not before.
		{`begin; 1.define_singleton_method(:f) {}; rescue => e; p e.class; end`, "TypeError\n"},
		{`begin; :s.define_singleton_method(:f) {}; rescue => e; p e.class; end`, "TypeError\n"},
		// Unfrozen definition still works -- a check that refused everything would
		// also pass every case above.
		{`c = Class.new
c.class_eval { def z; 7; end }
p c.new.z`, "7\n"},
		{`o = Object.new
def o.foo; 8; end
p o.foo`, "8\n"},
	})
}
