package vm_test

import "testing"

// TestParserFeatures covers grammar added in go-ruby-parser and exercised end to
// end here: %q/%Q/%() string literals, {x:} hash shorthand, and adjacent string
// concatenation. Values asserted against MRI Ruby 4.0.5.
func TestParserFeatures(t *testing.T) {
	cases := []struct{ src, want string }{
		// %q — non-interpolating; only \<delim> and \\ are escapes.
		{`x = %q(hi there); p x`, "\"hi there\"\n"},
		{`x = %q(a\)b); puts x`, "a)b\n"},
		{`x = %q(back\\slash); puts x`, "back\\slash\n"},
		{`x = %q(no #{interp}); puts x`, "no #{interp}\n"},
		{`x = %q[a (nested) ok]; puts x`, "a (nested) ok\n"},
		// %Q / %() — interpolating, double-quote semantics.
		{`n = 5; x = %Q(val=#{n}); puts x`, "val=5\n"},
		{`x = %(plain #{1 + 2}); puts x`, "plain 3\n"},
		{`x = %Q{nest #{[1, 2].map { |y| y }} ok}; puts x`, "nest [1, 2] ok\n"},
		// {x:} hash shorthand.
		{`x = 1; y = 2; p({x:, y:})`, "{x: 1, y: 2}\n"},
		{"def m(a, b); {a:, b:}; end\np m(3, 4)", "{a: 3, b: 4}\n"},
		// adjacent string-literal concatenation.
		{`p "a" "b" "c"`, "\"abc\"\n"},
		{`puts "hello " "world"`, "hello world\n"},
		// explicit operator-method calls.
		{`p 1.+(2)`, "3\n"},
		{`p 10.-(3)`, "7\n"},
		{`p (3.<=>(5))`, "-1\n"},
		// multi-value return.
		{"def m; return 1, 2; end\np m", "[1, 2]\n"},
		{"def f(x); return x, x * 2; end\na, b = f(5)\np [a, b]", "[5, 10]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
