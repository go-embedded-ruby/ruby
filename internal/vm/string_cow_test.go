// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestStringCoWAliasing proves that the copy-on-write string views produced by
// split/slice/chars/lines never let an in-place mutation of one element corrupt
// its siblings or the source string. Each case mutates a view and asserts the
// neighbours are untouched, exercising the view->owned transition on the first
// mutation for every mutating method.
func TestStringCoWAliasing(t *testing.T) {
	cases := []struct{ src, want string }{
		// The canonical case from the task: mutating a split field must not
		// corrupt siblings or the source.
		{`a = "hello world".split(" "); a[0] << "X"; p a; p "hello world"`,
			`["helloX", "world"]` + "\n" + `"hello world"` + "\n"},
		// Two views carved from the same source, mutated independently.
		{`s = "abc abc"; a = s.split(" "); a[0].upcase!; p a; p s`,
			`["ABC", "abc"]` + "\n" + `"abc abc"` + "\n"},
		// Regexp split fields.
		{`a = "a1b1c".split(/1/); a[1].replace("Z"); p a`,
			`["a", "Z", "c"]` + "\n"},
		// String#[] / #slice substring is a view; mutating it leaves the source.
		{`s = "hello"; t = s[1,3]; t << "!"; p t; p s`,
			`"ell!"` + "\n" + `"hello"` + "\n"},
		// chars elements are views.
		{`cs = "abc".chars; cs[0] << "z"; p cs`,
			`["az", "b", "c"]` + "\n"},
		// lines elements are views.
		{`ls = "a\nb\n".lines; ls[0].chomp!; p ls`,
			`["a", "b\n"]` + "\n"},
		// A whole battery of bang methods, each on a fresh split view, asserting
		// the view materializes correctly and in isolation.
		{`a = "Foo bar".split(" "); a[0].downcase!; p a[0]; p a[1]`, `"foo"` + "\n" + `"bar"` + "\n"},
		{`a = "abc def".split(" "); a[0].reverse!; p a`, `["cba", "def"]` + "\n"},
		{`a = "  x   y".split(" "); a[0].prepend("<"); p a`, `["<x", "y"]` + "\n"},
		{`a = "aab bbc".split(" "); a[0].squeeze!; p a`, `["ab", "bbc"]` + "\n"},
		{`a = "abc def".split(" "); a[0].insert(1, "-"); p a`, `["a-bc", "def"]` + "\n"},
		{`a = "abc def".split(" "); a[0].gsub!("b", "B"); p a`, `["aBc", "def"]` + "\n"},
		{`a = "abc def".split(" "); a[0].sub!("a", "A"); p a`, `["Abc", "def"]` + "\n"},
		{`a = "abc def".split(" "); a[0][0] = "Q"; p a`, `["Qbc", "def"]` + "\n"},
		{`a = "abc def".split(" "); a[0].slice!(0); p a`, `["bc", "def"]` + "\n"},
		{`a = "abc def".split(" "); a[0].concat("!"); p a`, `["abc!", "def"]` + "\n"},
		{`a = "abc def".split(" "); a[0].clear; p a`, `["", "def"]` + "\n"},
		{`a = "ABC def".split(" "); a[0].capitalize!; p a`, `["Abc", "def"]` + "\n"},
		{`a = "aB cD".split(" "); a[0].swapcase!; p a`, `["Ab", "cD"]` + "\n"},
		{`a = "  x  y".split(" "); a[0].replace(" z "); a[0].strip!; p a`, `["z", "y"]` + "\n"},
		// A binary (ASCII-8BIT) slice! on a materialized string still works.
		{`s = "hello".b; r = s.slice!(0,2); p r; p s`, `"he"` + "\n" + `"llo"` + "\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
		}
	}
}

// TestStringCoWFrozenViewRaises proves a frozen view still raises on mutation:
// ensureOwned must not mask a FrozenError. Frozen string literals are emitted as
// frozen views, so mutating one must raise rather than silently copy.
func TestStringCoWFrozenViewRaises(t *testing.T) {
	src := `
begin
  s = "frozen".freeze
  s << "x"
  puts "no error"
rescue => e
  puts e.class
end`
	if got := eval(t, src); got != "FrozenError\n" {
		t.Errorf("frozen view mutation: got %q, want FrozenError", got)
	}
}

// TestStringCoWViewAsHashKey exercises a split view used as a Hash key (the
// strKey view path) and then mutated afterwards, proving the stored key is not
// disturbed by the later mutation.
func TestStringCoWViewAsHashKey(t *testing.T) {
	src := `
h = {}
words = "one two one".split(" ")
words.each { |w| h[w] = (h[w] || 0) + 1 }
p h
words[0] << "!"
p h`
	want := `{"one" => 2, "two" => 1}` + "\n" + `{"one" => 2, "two" => 1}` + "\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
