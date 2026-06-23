package vm_test

import "testing"

// TestBeginCaseAsCommandArg covers begin…end and case…end used as a paren-less
// command argument. Asserted against MRI Ruby 4.0.5.
func TestBeginCaseAsCommandArg(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p begin; 1 + 2; end`, "3\n"},
		{`p begin; raise "x"; rescue => e; e.message; end`, "\"x\"\n"},
		{`def f(v); v; end; p f begin; 42; end`, "42\n"},
		{`p case 5 when 1..3 then :lo when 4..6 then :hi end`, ":hi\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
