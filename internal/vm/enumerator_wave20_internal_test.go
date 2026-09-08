// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestEnumeratorProductClass covers the Enumerator::Product class surface added
// beyond Enumerator.product: .new / .allocate / #initialize, the private
// #initialize_copy (replace, self-copy, frozen, uninitialized-source,
// different-class and arity guards), #rewind, #size across every branch, and
// #inspect (normal, uninitialized, self-referential). Verified against ruby 4.0.6.
func TestEnumeratorProductClass(t *testing.T) {
	cases := []struct{ src, want string }{
		// .new / #each store and walk the enumerables.
		{`p Enumerator::Product.new([1, 2], [:a, :b]).each.to_a`, `[[1, :a], [1, :b], [2, :a], [2, :b]]`},
		{`p Enumerator::Product.new.each.to_a`, `[[]]`},
		{`p Enumerator::Product.new([1, 2], [:a, :b]).class`, `Enumerator::Product`},
		// #each with a block returns self; yields each element to multi-arg blocks.
		{`e = Enumerator::Product.new([1, 2], [:a, :b]); p e.each {}.equal?(e)`, `true`},
		{`acc = []; Enumerator::Product.new([1, 2], [:a, :b]).each { |x, y| acc << y }; p acc`, `[:a, :b, :a, :b]`},
		{`acc = []; Enumerator::Product.new([], [1]).each { |x| acc << x }; p acc`, `[]`},
		// #inspect: normal, uninitialized (from .allocate), and self-referential.
		{`p Enumerator::Product.new([1, 2], [:a, :b]).inspect`, `"#<Enumerator::Product: [[1, 2], [:a, :b]]>"`},
		{`p Enumerator::Product.allocate.inspect`, `"#<Enumerator::Product: uninitialized>"`},
		{`a = [1, 2]; r = Enumerator::Product.new(a); a << r; p r.inspect`, `"#<Enumerator::Product: [[1, 2, #<Enumerator::Product: ...>]]>"`},
		// #initialize is private; .allocate + send(:initialize, ...) returns self.
		{`p Enumerator::Product.private_instance_methods(false).include?(:initialize)`, `true`},
		{`u = Enumerator::Product.allocate; p u.send(:initialize, 0..1, 2..3).equal?(u)`, `true`},
		{`u = Enumerator::Product.allocate.freeze; begin; u.send(:initialize, 0..1); rescue => e; p e.class; end`, `FrozenError`},
		// #initialize_copy: replace, return self, self-copy no-op (even frozen),
		// frozen receiver, uninitialized source, different class, non-Enumerator, arity.
		{`a = Enumerator::Product.new([true]); b = Enumerator::Product.new([1, 2], [:a, :b]); a.send(:initialize_copy, b); p a.each.to_a`, `[[1, :a], [1, :b], [2, :a], [2, :b]]`},
		{`a = Enumerator::Product.new([true]); b = Enumerator::Product.new([1]); p a.send(:initialize_copy, b).equal?(a)`, `true`},
		{`p Enumerator::Product.private_instance_methods(false).include?(:initialize_copy)`, `true`},
		{`a = Enumerator::Product.new(1..2); p a.send(:initialize_copy, a).equal?(a)`, `true`},
		{`a = Enumerator::Product.new(1..2).freeze; p a.send(:initialize_copy, a).equal?(a)`, `true`},
		{`a = Enumerator::Product.new(1..2).freeze; b = Enumerator::Product.new(3..4); begin; a.send(:initialize_copy, b); rescue => e; p e.class; end`, `FrozenError`},
		{`a = Enumerator::Product.new(1..2); u = Enumerator::Product.allocate; begin; a.send(:initialize_copy, u); rescue => e; p e.message; end`, `"uninitialized product"`},
		{`a = Enumerator::Product.new(1..2); begin; a.send(:initialize_copy, "x"); rescue => e; p e.class; end`, `TypeError`},
		{`a = Enumerator::Product.new(1..2); begin; a.send(:initialize_copy, [1].chain([2])); rescue => e; p e.class; end`, `TypeError`},
		{`a = Enumerator::Product.new(1..2); begin; a.send(:initialize_copy); rescue => e; p e.class; end`, `ArgumentError`},
		// #rewind rewinds each source that responds and returns self.
		{`o = Object.new; def o.each_entry; end; e = Enumerator::Product.new([1, 2].each, o); e.rewind; p e.rewind.equal?(e)`, `true`},
		// #size across every branch: Integer product, empty (0), no #size (nil),
		// non-Integer finite Float (nil), Symbol (nil), Bignum, and infinite (INFINITY).
		{`p Enumerator::Product.new(1..2, 1..3).size`, `6`},
		{`p Enumerator::Product.new(1...1, [1, 2]).size`, `0`},
		{`o = Object.new; def o.each_entry; end; p Enumerator::Product.new(1..2, o).size`, `nil`},
		{`o = Object.new; def o.size; 1.0; end; p Enumerator::Product.new(1..2, o).size`, `nil`},
		{`o = Object.new; def o.size; :sym; end; p Enumerator::Product.new(1..2, o).size`, `nil`},
		{`o = Object.new; def o.size; 10 ** 20; end; p Enumerator::Product.new(1..2, o).size`, `200000000000000000000`},
		{`p Enumerator::Product.new(1.., [:a]).size`, `Infinity`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestEnumeratorChainAndArithSeqWave20 covers the Chain constructors added
// (.allocate + private #initialize and an "uninitialized" #inspect) and the
// ArithmeticSequence surface (.new raises NoMethodError, .allocate raises
// TypeError, #== and #hash key on the begin/end/step/exclude_end? tuple). It also
// covers the plain-Enumerator "uninitialized" #inspect. Verified against ruby 4.0.6.
func TestEnumeratorChainAndArithSeqWave20(t *testing.T) {
	cases := []struct{ src, want string }{
		// Chain: uninitialized and initialized #inspect, private #initialize.
		{`p Enumerator::Chain.allocate.inspect`, `"#<Enumerator::Chain: uninitialized>"`},
		{`p Enumerator::Chain.new(1..2, 3..4).inspect`, `"#<Enumerator::Chain: [1..2, 3..4]>"`},
		{`p Enumerator::Chain.new.inspect`, `"#<Enumerator::Chain: []>"`},
		{`p Enumerator::Chain.private_instance_methods(false).include?(:initialize)`, `true`},
		{`u = Enumerator::Chain.allocate; p u.send(:initialize, 1..2).equal?(u)`, `true`},
		{`u = Enumerator::Chain.allocate; u.send(:initialize, 1..2, 3..4); p u.to_a`, `[1, 2, 3, 4]`},
		{`u = Enumerator::Chain.allocate.freeze; begin; u.send(:initialize); rescue => e; p e.class; end`, `FrozenError`},
		// A plain Enumerator from .allocate renders "uninitialized".
		{`p Enumerator.allocate.inspect`, `"#<Enumerator: uninitialized>"`},
		// ArithmeticSequence: no .new, no .allocate.
		{`begin; Enumerator::ArithmeticSequence.new; rescue => e; p e.class; end`, `NoMethodError`},
		{`begin; Enumerator::ArithmeticSequence.allocate; rescue => e; p e.message; end`, `"allocator undefined for Enumerator::ArithmeticSequence"`},
		// #== keys on begin/end/step/exclude_end? (dispatched via #send to bypass the
		// == operator's identity fast path), and #hash is a stable Integer.
		{`p 1.step(10).send(:==, 1.step(10))`, `true`},
		{`p 1.step(10, 100).send(:==, (1..10).step(100))`, `true`},
		{`p 1.step(10).send(:==, 1.step(11))`, `false`},
		{`p 1.step(10).send(:==, 1.step(10, 2))`, `false`},
		{`p (1..10).step.send(:==, (1...10).step)`, `false`},
		{`p 1.step(10).send(:==, 5)`, `false`},
		{`p 1.step(10).hash.is_a?(Integer)`, `true`},
		{`p 1.step(10).hash == 1.step(10).hash`, `true`},
		{`p((1...10).step.hash.is_a?(Integer))`, `true`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestEnumeratorTakeWhileBreaksEarly covers Enumerator#take_while: it terminates
// on an unbounded source (produce / cycle) by breaking at the first falsy element,
// returns an Enumerator without a block, collects while truthy for a finite
// source, and propagates a non-break exception from the block. Verified against
// ruby 4.0.6.
func TestEnumeratorTakeWhileBreaksEarly(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Enumerator.produce(0) { |x| x + 1 }.take_while { |x| x < 3 }`, `[0, 1, 2]`},
		{`p [1, 2, 3].cycle.take_while { |x| x < 3 }`, `[1, 2]`},
		{`p [1, 2, 3, 4].each.take_while { |x| x < 3 }`, `[1, 2]`},
		{`p [1, 2, 3].each.take_while.class`, `Enumerator`},
		{`begin; [1, 2, 3].each.take_while { raise "boom" }; rescue => e; p e.message; end`, `"boom"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
