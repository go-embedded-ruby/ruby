// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestKernelModuleFunctionPrivacy covers registerKernelModuleFunctions: the
// Kernel conversion/print/control methods MRI carries as module_function are
// PRIVATE instance methods (callable only without an explicit receiver) yet
// PUBLIC on the Kernel module (Kernel.X and Kernel.public_methods). Verified
// against ruby 4.0.6.
func TestKernelModuleFunctionPrivacy(t *testing.T) {
	values := []struct{ src, want string }{
		// private as an instance method: absent from a plain respond_to?, present
		// once private methods are included.
		{`p respond_to?(:Integer)`, "false"},
		{`p respond_to?(:Integer, true)`, "true"},
		{`p Kernel.private_instance_methods(false).include?(:puts)`, "true"},
		// public on the Kernel module itself.
		{`p Kernel.public_methods.include?(:Integer)`, "true"},
		{`p Kernel.public_methods.include?(:raise)`, "true"},
		// still callable without a receiver, and as a Kernel-module method.
		{`p Integer("5")`, "5"},
		{`p Kernel.Integer("5")`, "5"},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}

	// An explicit receiver is refused, exactly as MRI phrases it.
	errs := []struct{ src, class, msg string }{
		{`Object.new.puts("x")`, "NoMethodError", "private method 'puts' called for an instance of Object"},
		{`Object.new.Integer("5")`, "NoMethodError", "private method 'Integer' called for an instance of Object"},
	}
	for _, c := range errs {
		class, msg := evalErr(t, c.src)
		if class != c.class || msg != c.msg {
			t.Errorf("src=%q got %s:%q want %s:%q", c.src, class, msg, c.class, c.msg)
		}
	}
}
