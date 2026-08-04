// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestRespondSendReflect covers send / public_send name coercion, respond_to?'s
// fall-through to respond_to_missing? for inaccessible methods, and that a class
// descending directly from BasicObject allocates and dispatches without crashing.
// Asserted against MRI 3.4.
func TestRespondSendReflect(t *testing.T) {
	cases := []struct{ src, want string }{
		// send / __send__ / public_send name forms.
		{`p "hello".send(:upcase)`, "\"HELLO\"\n"},
		{`p "hello".send("upcase")`, "\"HELLO\"\n"},
		{`p "hello".__send__(:upcase)`, "\"HELLO\"\n"},
		{`p "hello".public_send(:upcase)`, "\"HELLO\"\n"},
		{`class N; def to_str; "upcase"; end; end; p "hi".send(N.new)`, "\"HI\"\n"},
		// respond_to? calls respond_to_missing? for a private method (returns false by default).
		{`class C; private def sec; end; end; p C.new.respond_to?(:sec)`, "false\n"},
		{`class C2; private def sec; end; end; p C2.new.respond_to?(:sec, true)`, "true\n"},
		// respond_to_missing? override answers respond_to? for an inaccessible/missing name.
		{`class D; def respond_to_missing?(n, p); n == :ghost; end; end; p [D.new.respond_to?(:ghost), D.new.respond_to?(:nope)]`, "[true, false]\n"},
		// public method answers immediately without consulting respond_to_missing?.
		{`class E; def pub; end; def respond_to_missing?(n, p); raise "should not be called"; end; end; p E.new.respond_to?(:pub)`, "true\n"},
		// A class directly under BasicObject allocates and initializes without crashing.
		{`class B < BasicObject; def val; 7; end; end; p B.new.val`, "7\n"},
		{`class B2 < BasicObject; def initialize(x); @x = x; end; def x; @x; end; end; p B2.new(5).x`, "5\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// send with a non-Symbol/String/to_str name raises TypeError.
		{`Object.new.send(42)`, "TypeError"},
		{`Object.new.send(nil)`, "TypeError"},
		{`Object.new.public_send(3.14)`, "TypeError"},
		{`class W; def to_str; 42; end; end; Object.new.send(W.new)`, "TypeError"},
		// A BasicObject subclass reports an unknown method through method_missing
		// (NoMethodError) rather than dereferencing a nil method record.
		{`class B3 < BasicObject; end; B3.new.no_such_method`, "NoMethodError"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
