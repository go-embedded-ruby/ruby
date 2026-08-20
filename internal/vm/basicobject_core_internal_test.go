// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestBasicObjectCoreMethods covers the BasicObject instance-method surface:
// the core methods (==, !, !=, __id__, __send__, equal?, instance_eval,
// instance_exec) live on BasicObject itself (so a bare BasicObject.new can use
// them and Object inherits them), initialize/method_missing are private, and
// instance_methods reports the public set exactly as MRI 4.0.6 does. ! and != are
// exercised through #send so the real methods run, not the bytecode operators.
func TestBasicObjectCoreMethods(t *testing.T) {
	cases := []struct{ src, want string }{
		// The public instance-method set matches MRI exactly (no initialize /
		// method_missing, which are private).
		{`p BasicObject.instance_methods(false).sort`,
			"[:!, :!=, :==, :__id__, :__send__, :equal?, :instance_eval, :instance_exec]"},
		{`p BasicObject.private_instance_methods(false).sort`, "[:initialize, :method_missing]"},
		// A bare BasicObject has the core methods (no Kernel).
		{`b = BasicObject.new; p b.__send__(:equal?, b)`, "true"},
		{`b = BasicObject.new; p b.__send__(:==, b)`, "true"},
		{`b = BasicObject.new; p b.__send__(:!)`, "false"},     // b is truthy
		{`b = BasicObject.new; p b.__send__(:!=, b)`, "false"}, // == self
		{`b = BasicObject.new; p b.__send__(:__id__).class`, "Integer"},
		// ! and != as dispatched methods (Object inherits them from BasicObject).
		{`p 1.send(:!)`, "false"},
		{`p nil.send(:!)`, "true"},
		{`p 1.send(:!=, 2)`, "true"},
		{`p 1.send(:!=, 1)`, "false"},
		// instance_methods filters private on an ordinary class too (the general
		// reflection fix): a private method appears only in private_instance_methods.
		{`class C1; def pub; end; def pri; end; private :pri; end
p C1.instance_methods(false).sort`, "[:pub]"},
		{`class C2; def pub; end; def pri; end; private :pri; end
p C2.private_instance_methods(false).sort`, "[:pri]"},
		// An object's own-only method list (methods(false) = its singleton methods)
		// walks a single class, exercising the non-ancestor reflection path.
		{`o = Object.new; def o.sing; end; p o.methods(false)`, "[:sing]"},
		// An undef'd method is hidden from the ancestor-walked method list even
		// though a superclass still defines it (the tombstone path).
		{`class BOBase; def foo; end; end
class BOSub < BOBase; undef_method :foo; end
p BOSub.new.methods.include?(:foo)`, "false"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
