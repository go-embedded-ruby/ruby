// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestLazyEachValuePacking pins the multi-value packing of Enumerator::Lazy#each
// (rb_enum_values_pack). A bare lazy forwards a source's multiple yielded values
// to the block by ordinary arity; once any op has run the values are packed into
// one Array element. Every expectation was checked against MRI 4.0.5.
func TestLazyEachValuePacking(t *testing.T) {
	multi := `Enumerator.new { |y| y.yield 1, 2 }`
	cases := []struct{ src, want string }{
		// Bare lazy, multi-value source: spread by arity (spreadMulti, multi != nil).
		{`a=[]; ` + multi + `.lazy.each { |x| a << x }; p a`, "[1]\n"},
		{`a=[]; ` + multi + `.lazy.each { |*x| a << x }; p a`, "[[1, 2]]\n"},
		{`a=[]; ` + multi + `.lazy.each { |x,y| a << [x,y] }; p a`, "[[1, 2]]\n"},
		// Bare lazy, single-value source: multi is nil (spreadMulti, multi == nil).
		{`a=[]; [10,20].lazy.each { |x| a << x }; p a`, "[10, 20]\n"},
		// After an op, a multi-value yield is packed into one element (not spread).
		{`a=[]; ` + multi + `.lazy.select { true }.each { |*z| a << z }; p a`, "[[[1, 2]]]\n"},
		{`a=[]; ` + multi + `.lazy.take(1).each { |x| a << x }; p a`, "[[1, 2]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
