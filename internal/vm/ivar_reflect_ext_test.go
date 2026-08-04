// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestIvarReflection exercises Object#instance_variable_get / _set / _defined? /
// instance_variables / remove_instance_variable, covering the MRI name
// coercion (#to_str), validation and frozen semantics. Asserted against MRI 3.4.
func TestIvarReflection(t *testing.T) {
	cases := []struct{ src, want string }{
		// get: Symbol / String / #to_str forms.
		{`o = Object.new; o.instance_variable_set(:@a, 7); p o.instance_variable_get(:@a)`, "7\n"},
		{`o = Object.new; o.instance_variable_set("@a", 7); p o.instance_variable_get("@a")`, "7\n"},
		{`class S; def to_str; "@a"; end; end
o = Object.new; o.instance_variable_set(:@a, 42); p o.instance_variable_get(S.new)`, "42\n"},
		// get: missing variable is nil, even on a frozen immediate.
		{`p Object.new.instance_variable_get(:@nope)`, "nil\n"},
		{`p nil.instance_variable_get(:@foo)`, "nil\n"},
		{`p :sym.instance_variable_get(:@foo)`, "nil\n"},
		// set returns the assigned value.
		{`p Object.new.instance_variable_set(:@a, "x")`, "\"x\"\n"},
		// defined?
		{`o = Object.new; o.instance_variable_set(:@a, 1); p [o.instance_variable_defined?(:@a), o.instance_variable_defined?(:@b)]`, "[true, false]\n"},
		{`p 5.instance_variable_defined?(:@a)`, "false\n"},
		// instance_variables: declaration order, empty, immediate, class (non-RObject).
		{`c = Class.new { def initialize; @c=1; @a=2; @b=3; end }; p c.new.instance_variables`, "[:@c, :@a, :@b]\n"},
		{`p Object.new.instance_variables`, "[]\n"},
		{`p [0, 0.5, true, false, nil].map { |v| v.instance_variables }`, "[[], [], [], [], []]\n"},
		{`c = Class.new; c.instance_variable_set(:@z, 9); p c.instance_variables`, "[:@z]\n"},
		{`@top = 1; p self.instance_variables`, "[:@top]\n"},
		// unicode variable name.
		{`o = Object.new; o.instance_variable_set(:@💙, 42); p o.instance_variable_get(:@💙)`, "42\n"},
		// remove returns the value and clears the variable (also drops from order).
		{`o = Object.new; o.instance_variable_set(:@a, "hi"); v = o.remove_instance_variable(:@a); p [v, o.instance_variable_defined?(:@a), o.instance_variables]`, "[\"hi\", false, []]\n"},
		{`o = Object.new; o.instance_variable_set(:@a, 1); o.instance_variable_set(:@b, 2); o.remove_instance_variable(:@a); p o.instance_variables`, "[:@b]\n"},
		// freeze reports frozen and is idempotent for get.
		{`o = Object.new; o.freeze; p o.frozen?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// get: invalid names -> NameError.
		{`Object.new.instance_variable_get(:@)`, "NameError"},
		{`Object.new.instance_variable_get(:@0)`, "NameError"},
		{`Object.new.instance_variable_get(:test)`, "NameError"},
		{`Object.new.instance_variable_get("test")`, "NameError"},
		{`Object.new.instance_variable_get(:"@@x")`, "NameError"},
		// get: bad argument types -> TypeError.
		{`Object.new.instance_variable_get(10)`, "TypeError"},
		{`Object.new.instance_variable_get(Object.new)`, "TypeError"},
		{`class T; def to_str; 123; end; end
Object.new.instance_variable_get(T.new)`, "TypeError"},
		// get: #to_str result that is not an @name -> NameError.
		{`class U; def to_str; "test"; end; end
Object.new.instance_variable_get(U.new)`, "NameError"},
		// set: frozen object / invalid name / bad type.
		{`o = Object.new; o.freeze; o.instance_variable_set(:@a, 1)`, "FrozenError"},
		{`nil.instance_variable_set(:@foo, 1)`, "FrozenError"},
		{`nil.instance_variable_set(:foo, 1)`, "NameError"},
		{`Object.new.instance_variable_set(:c, 1)`, "NameError"},
		{`"".instance_variable_set(1, 2)`, "TypeError"},
		// defined?: invalid name still validated.
		{`Object.new.instance_variable_defined?(:bad)`, "NameError"},
		// remove: undefined / invalid / frozen.
		{`Object.new.remove_instance_variable(:@missing)`, "NameError"},
		{`Object.new.remove_instance_variable(:@0)`, "NameError"},
		{`o = Object.new; o.freeze; o.remove_instance_variable(:@a)`, "FrozenError"},
		{`nil.remove_instance_variable(:@foo)`, "FrozenError"},
		{`nil.remove_instance_variable(:foo)`, "NameError"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
