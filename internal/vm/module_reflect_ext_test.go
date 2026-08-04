// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestModuleReflect covers Module#public/private/protected_instance_methods and
// the attr_* family (return value, visibility, #to_str coercion, frozen
// receivers). Asserted against MRI 3.4.
func TestModuleReflect(t *testing.T) {
	cases := []struct{ src, want string }{
		// Visibility-filtered instance-method listings (own only, false argument).
		{`class C; def a; end; protected def b; end; private def c; end; end
p [C.public_instance_methods(false), C.protected_instance_methods(false), C.private_instance_methods(false)]`,
			"[[:a], [:b], [:c]]\n"},
		// include-super default gathers inherited public methods; a nearer private
		// override shadows an inherited public method.
		{`class P; def pub; end; end
class Q < P; private :pub; end
p [Q.public_instance_methods(false).include?(:pub), Q.private_instance_methods.include?(:pub)]`,
			"[false, true]\n"},
		// undef hides an inherited name from the listing.
		{`class P2; def gone; end; end
class Q2 < P2; undef_method :gone; end
p Q2.public_instance_methods.include?(:gone)`, "false\n"},
		// attr_* return the created names as Symbols.
		{`class R; end; p R.send(:attr_reader, :foo, :bar)`, "[:foo, :bar]\n"},
		{`class R2; end; p R2.send(:attr_writer, :foo)`, "[:foo=]\n"},
		{`class R3; end; p R3.send(:attr_accessor, :foo)`, "[:foo, :foo=]\n"},
		{`class R4; end; p R4.send(:attr, :foo)`, "[:foo]\n"},
		// attr boolean form: attr(:name, true) adds a writer.
		{`class R5; end; p R5.send(:attr, :foo, true)`, "[:foo, :foo=]\n"},
		{`class R6; end; p R6.send(:attr, :foo, false)`, "[:foo]\n"},
		// attr with two non-boolean names defines a reader for each.
		{`class R7; end; p R7.send(:attr, :foo, :bar)`, "[:foo, :bar]\n"},
		// accessors round-trip.
		{`class A1; attr_accessor :x; end; o=A1.new; o.x = 9; p o.x`, "9\n"},
		// attr_* coerce a #to_str name.
		{`class Nm; def to_str; "hi"; end; end; class A2; end; A2.send(:attr_reader, Nm.new); o=A2.new; o.instance_variable_set(:@hi, 3); p o.hi`, "3\n"},
		// current visibility applies to generated accessors.
		{`class A3; private; attr_accessor :sec; end; p A3.private_instance_methods(false).sort`, "[:sec, :sec=]\n"},
		// class frozen? reflects Object#freeze.
		{`c = Class.new; c.freeze; p c.frozen?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// attr_* on a frozen class raise FrozenError.
		{`c = Class.new; c.freeze; c.send(:attr_reader, :x)`, "FrozenError"},
		// a generated writer on a frozen instance raises FrozenError.
		{`class F1; attr_writer :x; end; o=F1.new; o.freeze; o.x = 1`, "FrozenError"},
		// a private accessor is not callable with an explicit receiver.
		{`class F2; private; attr_reader :x; end; F2.new.x`, "NoMethodError"},
		// a non-#to_str attribute name raises TypeError.
		{`class F3; end; F3.send(:attr_reader, 5)`, "TypeError"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
