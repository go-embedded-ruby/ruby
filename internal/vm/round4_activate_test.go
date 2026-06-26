package vm_test

import "testing"

// TestRound4Activate exercises the Round-4 parser shapes end to end through the
// compiler and VM. Every want value is what MRI Ruby prints for the snippet.
func TestRound4Activate(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- Case 1: nested (destructuring) MultiAssign targets ---
		{"(a, b) = [1, 2]; p [a, b]", "[1, 2]\n"},
		{"(a, (b, c)) = [1, [2, 3]]; p [a, b, c]", "[1, 2, 3]\n"},
		{"((a, b), c) = [[1, 2], 3]; p [a, b, c]", "[1, 2, 3]\n"},
		{"(a, *b) = [1, 2, 3]; p [a, b]", "[1, [2, 3]]\n"},
		{"(a, (b, *c)) = [1, [2, 3, 4]]; p [a, b, c]", "[1, 2, [3, 4]]\n"},
		{"((a, b), (c, d)) = [[1, 2], [3, 4]]; p [a, b, c, d]", "[1, 2, 3, 4]\n"},
		{"a, (b, c), d = 1, [2, 3], 4; p [a, b, c, d]", "[1, 2, 3, 4]\n"},
		{"x = ((a, b) = [1, 2]); p x", "[1, 2]\n"}, // assignment is the RHS array
		{"[[1, 2]].each { |(a, b)| p [a, b] }", "[1, 2]\n"},
		// A nested group whose value is not an array still binds (splat-of-scalar).
		{"(a, (b, c)) = [1, 9]; p [a, b, c]", "[1, 9, nil]\n"},

		// --- Case 2: class / module / singleton-class definition as a value ---
		{"x = class C; def m; 9; end; 7; end; p x", "7\n"},
		{"m = module M; 5; end; p m", "5\n"},
		{"sc = class << Object.new; self; end; p sc.class", "Class\n"},

		// --- Case 3: anonymous-argument forwarding (*, **, &) ---
		{"def f(*); g(*); end; def g(*a); a; end; p f(1, 2, 3)", "[1, 2, 3]\n"},
		{"def f(**); g(**); end; def g(**k); k; end; p f(a: 1, b: 2)", "{a: 1, b: 2}\n"},
		{"def f(&); g(&); end; def g; yield 5; end; p f { |x| x * 10 }", "50\n"},
		{"def f(*, **, &); g(*, **, &); end; def g(*a, **k); yield a, k; end; p f(1, 2, x: 3) { |a, k| [a, k] }", "[[1, 2], {x: 3}]\n"},
		// Bare markers also forward a def(...) capture.
		{"def f(...); g(*); end; def g(*a); a; end; p f(1, 2)", "[1, 2]\n"},
		{"def f(...); g(**); end; def g(**k); k; end; p f(a: 1)", "{a: 1}\n"},
		{"def f(...); g(&); end; def g; yield 7; end; p f { |x| x + 1 }", "8\n"},

		// --- Case 4: rational / imaginary literals ---
		{"p 2r", "(2/1)\n"},
		{"p 2r + 1r", "(3/1)\n"},
		{"p 2.5r", "(5/2)\n"},
		{"p 0.1r", "(1/10)\n"},
		{"p 3i", "(0+3i)\n"},
		{"p 2.5ri", "(0+(5/2)*i)\n"},
		{"p 100000000000000000000r", "(100000000000000000000/1)\n"},  // bignum rational
		{"p 100000000000000000000i", "(0+100000000000000000000i)\n"}, // bignum imaginary
		{"p 1.5i", "(0+1.5i)\n"},
		{"p Complex(1, Rational(-1, 2))", "(1-(1/2)*i)\n"},
		{"p Complex(Rational(1, 3), 2)", "((1/3)+2i)\n"},
		// to_s keeps the bare (un-parenthesised) rational components.
		{"puts Complex(0, Rational(5, 2)).to_s", "0+5/2i\n"},

		// --- Case 5: rescue capture to a non-local target ---
		{`begin; raise "x"; rescue => @e; end; p @e.message`, "\"x\"\n"},
		{`begin; raise "y"; rescue => $g; end; p $g.message`, "\"y\"\n"},
		{`class K; @@e = nil; def self.go; begin; raise "z"; rescue => @@e; end; @@e.message; end; end; p K.go`, "\"z\"\n"},

		// --- Case 6: explicit block passed to super ---
		{"class A; def m; yield; end; end; class B < A; def m; super { 42 }; end; end; p B.new.m { 99 }", "42\n"},
		{"class A; def m; yield 1; end; end; class B < A; def m; super { |x| x + 100 }; end; end; p B.new.m", "101\n"},
		{"class A; def m(*a); yield a; end; end; class B < A; def m(*a); super(*a) { |x| x.sum }; end; end; p B.new.m(1, 2, 3)", "6\n"},
		// super with a splat AND an explicit &block-pass.
		{"class A; def m(*a); yield a; end; end; class B < A; def m(*a); pr = proc { |x| x.sum * 10 }; super(*a, &pr); end; end; p B.new.m(1, 2)", "30\n"},

		// --- Case 7: !~ (truthy-negated match) ---
		{`p("ab" !~ /b/)`, "false\n"},
		{`p("ab" !~ /z/)`, "true\n"},
		{`s = "hello"; p(s !~ /xyz/)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRound4ActivateErrors covers the runtime error a `!~` against a receiver
// without `=~` raises (mirroring MRI's Kernel#!~ → NoMethodError on `=~`).
func TestRound4ActivateErrors(t *testing.T) {
	if err := runErr(t, "p(1 !~ /x/)"); err == nil {
		t.Fatal("expected NoMethodError for `1 !~ /x/`")
	}
}
