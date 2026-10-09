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
		// nil, true and false report frozen? == true -- they are immutable -- and
		// MRI nonetheless ALLOWS a singleton method on them: it lands on
		// NilClass/TrueClass/FalseClass. A guard built on Ruby's `frozen?` refuses
		// these, and the first version of this one did: it cost
		// core/{nil,true,false}/singleton_method_spec.rb, three files the
		// conformance ratchet caught at 0 passing against a baseline of 1. What a
		// definition guard needs is "frozen by SOMEONE", which is a flag, not
		// "frozen by nature", which is a type.
		{`nil.define_singleton_method(:zz) { 42 }
p nil.zz`, "42\n"},
		{`true.define_singleton_method(:zz) { 42 }
p true.zz`, "42\n"},
		{`def (false).zz; 42; end
p false.zz`, "42\n"},
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

// TestAFrozenRegexpRefusesDefToo covers the one branch of explicitlyFrozen the
// first version of this change left untested -- the coverage gate named it, at
// 90.0%, and the question a named function asks is whether the branch is DEAD
// or merely unreached. It was unreached: a literal /a/ is born frozen in both
// engines, and defining on it already raised.
//
// Measuring it turned up a second thing. `Regexp.new("a").freeze.frozen?`
// answered FALSE: Object#freeze had no *Regexp arm while isFrozen had read
// Regexp.frozen all along, so the two disagreed. The literal case hid it,
// because a literal needs no freeze call to be frozen.
func TestAFrozenRegexpRefusesDefToo(t *testing.T) {
	checkCases(t, []runCase{
		// freeze must actually mark it. This is the half that was broken.
		{`r = Regexp.new("a"); p r.frozen?; r.freeze; p r.frozen?`, "false\ntrue\n"},
		// ... and then refuse a definition, naming the Regexp as MRI does.
		{`r = Regexp.new("a").freeze
begin; r.define_singleton_method(:z) {}; rescue FrozenError => e; puts e.message.split(":").first; end`,
			"can't modify frozen Regexp\n"},
		// A literal is born frozen in both engines, so it refuses without a
		// freeze call -- this is the case that was already passing and hid the
		// one above.
		{`begin; /a/.define_singleton_method(:z) {}; rescue FrozenError => e; puts e.message.split(":").first; end`,
			"can't modify frozen Regexp\n"},
		// The negative control: an unfrozen Regexp still takes a singleton method.
		{`r = Regexp.new("a"); r.define_singleton_method(:z) { 7 }; p r.z`, "7\n"},
	})
}
