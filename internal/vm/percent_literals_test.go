package vm_test

import "testing"

// TestPercentLiterals covers the percent-literal forms in both statement and
// command-argument position, the interpolating variants, and that modulo still
// parses. Asserted against MRI Ruby 4.0.5.
func TestPercentLiterals(t *testing.T) {
	cases := []struct{ src, want string }{
		// %w / %i word and symbol arrays, including command-argument position.
		{`p %w[a b c]`, "[\"a\", \"b\", \"c\"]\n"},
		{`p %i[x y z]`, "[:x, :y, :z]\n"},
		{`p %w[a b].map(&:upcase)`, "[\"A\", \"B\"]\n"},
		{`def f(*a); a; end; p f(%w[x y], %i[z])`, "[[\"x\", \"y\"], [:z]]\n"},
		// %q (no interpolation) vs %Q / %( ) (interpolation).
		{`p %q(a#{1}b)`, "\"a\\#{1}b\"\n"},
		{`p %Q(a#{1 + 1}b)`, "\"a2b\"\n"},
		{`p %(hi#{3})`, "\"hi3\"\n"},
		// %W / %I interpolating arrays.
		{`p %W[a#{2} b]`, "[\"a2\", \"b\"]\n"},
		{`p %I[s#{1} t]`, "[:s1, :t]\n"},
		// Modulo is unaffected.
		{`p [5 % 2, 7 % 4]`, "[1, 3]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
