// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestKernelBindingIsMirroredOntoKernel pins the module_function split for
// Kernel#binding. MRI installs it with rb_define_global_function("binding",
// rb_f_binding, 0) (proc.c ruby_4_0:4726), so it is a PRIVATE instance method of
// Kernel and a PUBLIC method on the Kernel module. The method already worked here
// — self.send(:binding) returned a Binding — but the name was missing from
// registerKernelModuleFunctions' list, so none of the reflection below saw it.
// Every expectation was measured on ruby 4.0.5.
func TestKernelBindingIsMirroredOntoKernel(t *testing.T) {
	cases := []struct{ src, want string }{
		// The omission itself, in the shape core/kernel/binding_spec.rb reads it.
		{`p Kernel.private_instance_methods(false).include?(:binding)`, "true"},
		{`p Kernel.private_method_defined?(:binding)`, "true"},
		{`p Kernel.instance_method(:binding).owner`, "Kernel"},
		{`p Kernel.instance_method(:binding).name`, ":binding"},
		// -1, not MRI's 0: EVERY native method reports -1 here (Object
		// #frozen?/#object_id do too, both 0 on ruby 4.0.5), so this is the
		// engine-wide native-arity gap and not something the mirror introduced.
		// It is asserted rather than omitted so the mirrored record is seen to
		// carry the ORIGINAL's arity instead of inventing one.
		{`p Kernel.instance_method(:binding).arity`, "-1"},
		// Public on the Kernel module, like every other module_function.
		{`p Kernel.public_methods.include?(:binding)`, "true"},
		{`p Kernel.binding.class`, "Binding"},
		// And still private on the instance side, still reachable through send —
		// the half that already worked, kept under test so a later mirror change
		// cannot trade one for the other.
		{`p respond_to?(:binding)`, "false"},
		{`p respond_to?(:binding, true)`, "true"},
		{`p self.send(:binding).class`, "Binding"},
		{`p binding.class`, "Binding"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}

	// An explicit receiver is refused, as rb_define_global_function's private
	// Object-side copy demands. Measured on ruby 4.0.5.
	class, msg := evalErr(t, `Object.new.binding`)
	if want := "private method 'binding' called for an instance of Object"; class != "NoMethodError" || msg != want {
		t.Errorf("Object.new.binding: got %s:%q want NoMethodError:%q", class, msg, want)
	}
}

// TestSpecificEvalStringSeesCallerLocals covers the STRING form of
// instance_eval / module_eval / class_eval. MRI's specific_eval routes it to
// eval_under -> eval_string_with_cref (vm_eval.c ruby_4_0:2152-2187,
// 1882-1911), which compiles the source as a child scope of
// rb_vm_get_ruby_level_next_cfp — the CALLER'S frame — while overwriting self
// with the receiver and PUSHING a cref (the singleton class for instance_eval,
// the module itself for class_eval).
//
// rbgo ran the source in a fresh, EMPTY scope, so a caller local raised
// NoMethodError on read and was silently ignored on write. Both directions are
// asserted: the write one is where a half-fix shows, because it fails without
// raising anything.
func TestSpecificEvalStringSeesCallerLocals(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// Read, all three entry points.
		{"instance_eval read", `x = 41; p Object.new.instance_eval("x + 1")`, "42"},
		{"class_eval read", `x = 41; p String.class_eval("x + 1")`, "42"},
		{"module_eval read", `x = 41; p Comparable.module_eval("x + 1")`, "42"},
		// Write back into the caller's local — the direction a half-fix misses.
		{"instance_eval write", `x = 1; Object.new.instance_eval("x = 99"); p x`, "99"},
		{"class_eval write", `x = 1; String.class_eval("x = 99"); p x`, "99"},
		{"write inside a method", `def w; z = 1; Object.new.instance_eval("z = 42"); z; end; p w`, "42"},
		// A caller local SHADOWS a method of the same name, as it does in ordinary
		// Ruby: this answered :meth before, silently.
		{"local shadows method", `def shadow; :meth; end; shadow = :local; p Object.new.instance_eval("shadow")`, ":local"},
		// The receiver is self even while the locals come from the caller — the two
		// halves come from different places (eval_string_with_cref overwrites
		// block.as.captured.self).
		{"self is the receiver", `o = Object.new; p o.instance_eval("self").equal?(o)`, "true"},
		{"self over an immediate", `n = 3; p 5.instance_eval("self + n")`, "8"},
		{"class_eval self is the module", `p String.class_eval("self")`, "String"},
		// The cref is PUSHED, not the caller's: singleton for instance_eval so a
		// `def` becomes a singleton method, the module for class_eval so it becomes
		// an instance method. This half already worked; it must survive the change.
		{"instance_eval def is a singleton method",
			`o = Object.new; o.instance_eval("def zzz; :sing; end"); p [o.zzz, o.singleton_methods]`, "[:sing, [:zzz]]"},
		{"class_eval def is an instance method",
			`c = Class.new; c.class_eval("def yyy; :inst; end"); p [c.new.yyy, c.instance_methods(false)]`, "[:inst, [:yyy]]"},
		{"class_eval def self. is a class method",
			`c = Class.new; c.class_eval("def self.cm; :cm; end"); p c.cm`, ":cm"},
		{"constant resolves through the pushed cref",
			`class O13; K13 = :found; end; p O13.class_eval("K13")`, ":found"},
		{"ivars resolve against the receiver",
			`o = Object.new; o.instance_variable_set(:@a, 7); p o.instance_eval("@a")`, "7"},
		// A local the eval string CREATES does not leak back to the caller: the
		// caller's ISeq has no slot for it, so `defined?` there is nil (MRI agrees).
		{"a new local does not leak", `Object.new.instance_eval("qq = 5"); p defined?(qq)`, "nil"},
		// eval is transparent to __method__ through this path too.
		{"__method__ is the caller's", `def w12; Object.new.instance_eval("__method__"); end; p w12`, ":w12"},
		// The (file, line) pair is still specific_eval's argv[1]/argv[2], not the
		// caller's: routing through the caller's scope must not borrow its path.
		{"explicit filename wins", `p Object.new.instance_eval("__FILE__", "given.rb")`, `"given.rb"`},
		{"explicit first line wins", `p Object.new.instance_eval("__LINE__", "given.rb", 40)`, "40"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("%s: src=%q got=%q want=%q", c.name, c.src, got, c.want+"\n")
		}
	}
}

// TestSpecificEvalStringInheritsTheBlockScopeGap records a divergence this change
// does NOT close, so that nobody reads the tests above as a claim that it did.
//
// rbgo's frameBinding (internal/vm/vm.go) builds a Binding from the frame's own
// iseq.Locals and does not walk the enclosing Env chain, so a binding captured
// inside a BLOCK cannot see the locals of the method or top level around it. MRI
// does: `y = 5; [1].map { eval("y * 2") }` is [10] on ruby 4.0.5.
//
// That gap is Kernel#eval's already — it predates this change and behaves
// identically there — and instance_eval now shares it precisely BECAUSE the two
// were joined onto one mechanism. Pinning it keeps the two in step: when
// frameBinding learns to walk the parent env, this test fails and both want
// is [10].
func TestSpecificEvalStringInheritsTheBlockScopeGap(t *testing.T) {
	const evalSrc = `y = 5; p([1].map { eval("y * 2") })`
	const ievalSrc = `y = 5; p([1].map { Object.new.instance_eval("y * 2") })`
	const want = "undefined method 'y' for "
	for _, src := range []string{evalSrc, ievalSrc} {
		class, msg := evalErr(t, src)
		if class != "NoMethodError" || len(msg) < len(want) || msg[:len(want)] != want {
			t.Errorf("src=%q: got %s:%q; expected the shared block-scope gap (%s%s…). "+
				"If frameBinding now walks the enclosing env, both of these answer [10] on "+
				"ruby 4.0.5 and this test should become that assertion.", src, class, msg, "NoMethodError:", want)
		}
	}
}
