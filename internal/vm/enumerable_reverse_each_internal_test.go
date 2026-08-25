// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestEnumerableReverseEach covers Enumerable#reverse_each: with a block it
// yields the elements in reverse and returns the receiver; a multi-value each
// gathers each yield into an array; with no block it returns an Enumerator whose
// size is the receiver's own size when it defines one and nil otherwise. Verified
// against ruby 4.0.6.
func TestEnumerableReverseEach(t *testing.T) {
	const numerous = `class E; include Enumerable; def each; yield 2; yield 5; yield 3; yield 6; yield 1; yield 4; end; end; `
	const withSize = `class S; include Enumerable; def each; yield 1; yield 2; yield 3; yield 4; end; def size; 4; end; end; `
	const multi = `class M; include Enumerable; def each; yield 1, 2; yield 3, 4, 5; end; end; `
	cases := []struct{ src, want string }{
		// Block form: reverse order, returns the receiver.
		{numerous + `a = []; E.new.reverse_each { |i| a << i }; p a`, `[4, 1, 6, 3, 5, 2]`},
		{numerous + `p E.new.reverse_each {}.class`, `E`},
		// No block: an Enumerator that reverses.
		{numerous + `p E.new.reverse_each.class`, `Enumerator`},
		{numerous + `p E.new.reverse_each.to_a`, `[4, 1, 6, 3, 5, 2]`},
		// A multi-value each gathers each yield into an array, then reverses.
		{multi + `y = []; M.new.reverse_each { |e| y << e }; p y`, `[[3, 4, 5], [1, 2]]`},
		// The enumerator's size follows the receiver: known when it has #size...
		{withSize + `p S.new.reverse_each.size`, `4`},
		// ...and nil otherwise.
		{numerous + `p E.new.reverse_each.size`, `nil`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
