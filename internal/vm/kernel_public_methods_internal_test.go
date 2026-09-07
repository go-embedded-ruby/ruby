// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestKernelPublicMethodsMirror covers registerKernelPublicMethods: the plain
// (non-module-function) public Kernel instance methods are reflected onto the
// Kernel module as CRuby places them, while their bodies stay owned by Object so
// callNative does not unwrap a built-in value subclass's receiver. Verified
// against ruby 4.0.5.
func TestKernelPublicMethodsMirror(t *testing.T) {
	values := []struct{ src, want string }{
		// Listed as Kernel instance methods with the right visibility.
		{`p Kernel.public_instance_methods(false).include?(:respond_to?)`, "true"},
		{`p Kernel.public_instance_methods(false).include?(:eql?)`, "true"},
		{`p Kernel.public_instance_methods(false).include?(:remove_instance_variable)`, "true"},
		{`p Kernel.private_instance_methods(false).include?(:respond_to_missing?)`, "true"},
		// Kernel#then is an alias of Kernel#yield_self, and Kernel#kind_of? of
		// Kernel#is_a? — the mirror shares one record so the UnboundMethods are ==.
		{`p Kernel.instance_method(:then) == Kernel.instance_method(:yield_self)`, "true"},
		{`p Kernel.instance_method(:kind_of?) == Kernel.instance_method(:is_a?)`, "true"},
		// The two whose owner the specs read resolve to Kernel via the smethods copy.
		{`p Kernel.method(:respond_to?).owner`, "Kernel"},
		{`p Kernel.method(:respond_to_missing?).owner`, "Kernel"},
		// Ordinary dispatch is unchanged: is_a? still consults the receiver's real
		// class, not the unwrapped built-in value, for a subclass of String.
		{`class KSub < String; end; p KSub.new.is_a?(KSub)`, "true"},
		{`class KSub2 < String; end; p KSub2.new.is_a?(String)`, "true"},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestThenYieldSelfNoBlock covers the blockless arm of Kernel#then / #yield_self:
// each returns a size-1 Enumerator over self (its #inspect names the primary
// #then even through the #yield_self alias), while a block still yields self and
// returns the block's value. Verified against ruby 4.0.5.
func TestThenYieldSelfNoBlock(t *testing.T) {
	values := []struct{ src, want string }{
		{`p 5.then.class`, "Enumerator"},
		{`p 5.yield_self.class`, "Enumerator"},
		{`p 5.then.size`, "1"},
		{`o = Object.new; p o.then.first.equal?(o)`, "true"},
		{`o = Object.new; p o.then.peek.equal?(o)`, "true"},
		{`p 5.then.inspect`, `"#<Enumerator: 5:then>"`},
		{`p 5.yield_self.inspect`, `"#<Enumerator: 5:then>"`},
		// With a block: yields self, returns the block value.
		{`p 5.then { |n| n * 2 }`, "10"},
		{`p 5.yield_self { |n| n + 1 }`, "6"},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
