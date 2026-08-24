// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestSymbolIndex covers Symbol#[] / Symbol#slice running the full String#[]
// protocol against the symbol's name: an Integer index, an index and length, a
// Range, a substring, and a Regexp (with and without a capture index, setting
// $~), each yielding a String or nil. Verified against ruby 4.0.6.
func TestSymbolIndex(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p :hello[1]`, `"e"`},
		{`p :hello[-1]`, `"o"`},
		{`p :hello[1, 3]`, `"ell"`},
		{`p :hello[1..3]`, `"ell"`},
		{`p :hello["ell"]`, `"ell"`},
		{`p :hello["xyz"]`, `nil`},
		{`p :hello[10]`, `nil`},
		// Regexp forms.
		{`p :hello[/l+/]`, `"ll"`},
		{`p :hello[/l(l)/, 1]`, `"l"`},
		{`p :hello[/xyz/]`, `nil`},
		// A Regexp match sets $~.
		{`:hello[/l+/]; p $~[0]`, `"ll"`},
		// #slice is the same.
		{`p :hello.slice(1, 3)`, `"ell"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
