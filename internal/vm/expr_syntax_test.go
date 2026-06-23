package vm_test

import "testing"

// TestExpressionSyntax covers if/unless and while/until used as expressions,
// leading-dot method chains across newlines, and a &block parameter in a block
// list. Asserted against MRI Ruby 4.0.5.
func TestExpressionSyntax(t *testing.T) {
	cases := []struct{ src, want string }{
		// if / unless as an expression (RHS, argument, return, method body).
		{`x = if true then 1 else 2 end; p x`, "1\n"},
		{`p(if 5 > 3 then :big else :small end)`, ":big\n"},
		{`y = unless false then :a else :b end; p y`, ":a\n"},
		{`class C; def go(c); v = if c then :y else :n end; v; end; end; p C.new.go(true)`, ":y\n"},
		// while / until as an expression (value is nil).
		{`x = (while false; end); p x`, "nil\n"},
		{`p(until true; end)`, "nil\n"},
		// Leading-dot method chains across newlines.
		{"r = \"abc\"\n  .upcase\n  .reverse\np r", "\"CBA\"\n"},
		// &block parameter in a block list: unbound (nil) under our calling convention.
		{`r = []; [1, 2].each { |x, &b| r << [x, b] }; p r`, "[[1, nil], [2, nil]]\n"},
		{`p proc { |a, &b| [a, b.nil?] }.call(5)`, "[5, true]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
