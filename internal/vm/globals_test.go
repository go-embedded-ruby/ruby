package vm_test

import "testing"

// TestGlobalVariables covers user-assigned $globals: plain and compound
// assignment, cross-scope visibility, and that an unset global reads as nil —
// while the regexp match-data specials still take priority. MRI Ruby 4.0.5.
func TestGlobalVariables(t *testing.T) {
	cases := []struct{ src, want string }{
		{`$g = 5; p $g`, "5\n"},
		{`p $undefined`, "nil\n"},
		{`$count = 0; $count += 3; $count += 2; p $count`, "5\n"},
		{`$flag ||= 10; p $flag`, "10\n"},
		{`$a = 1; $a ||= 99; p $a`, "1\n"},
		{`$a = $b = 7; p [$a, $b]`, "[7, 7]\n"},
		// A global set inside a method is visible at the top level (globals are
		// not scoped, unlike locals).
		{`def setit; $shared = 42; end; setit; p $shared`, "42\n"},
		{`class C; def set; $obj = self; end; end; c = C.new; c.set; p $obj.class`, "C\n"},
		// Match-data specials keep priority over the user-global table.
		{`"abc" =~ /(b)/; p $1`, "\"b\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
