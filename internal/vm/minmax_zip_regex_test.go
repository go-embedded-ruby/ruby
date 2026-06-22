package vm_test

import "testing"

// TestMinMaxZipStringRegex covers min(n)/max(n) (Array, Range, String range),
// the variadic zip, and String#[] / #slice with a Regexp. Asserted against MRI
// Ruby 4.0.5.
func TestMinMaxZipStringRegex(t *testing.T) {
	cases := []struct{ src, want string }{
		// min(n) / max(n): n smallest (ascending) / n largest (descending).
		{`p [(1..5).min(2), (1..5).max(2)]`, "[[1, 2], [5, 4]]\n"},
		{`p [[3, 1, 2].min(2), [3, 1, 2].max(2)]`, "[[1, 2], [3, 2]]\n"},
		{`p (1..3).max(5)`, "[3, 2, 1]\n"}, // n larger than the range
		// No-arg min/max still scalar; String ranges work for both forms.
		{`p [(1..5).min, (1..5).max]`, "[1, 5]\n"},
		{`p [("a".."e").min, ("a".."e").max]`, "[\"a\", \"e\"]\n"},
		{`p [("a".."e").min(2), ("a".."e").max(2)]`, "[[\"a\", \"b\"], [\"e\", \"d\"]]\n"},
		// Empty ranges -> nil (integer and string).
		{`p [(5..1).min, (5..1).max]`, "[nil, nil]\n"},
		{`p [("z".."a").min, ("z".."a").max]`, "[nil, nil]\n"},
		// Variadic zip pads short operands with nil.
		{`p [1, 2, 3].zip([4, 5, 6], [7, 8])`, "[[1, 4, 7], [2, 5, 8], [3, 6, nil]]\n"},
		{`p (1..3).zip([4, 5, 6])`, "[[1, 4], [2, 5], [3, 6]]\n"},
		// String#[] / #slice with a Regexp: whole match, capture group, named group, no match.
		{`p ["hello"[/l+/], "hello"[/x/], "hello"[/l(l)o/, 1], "hi"[/(?<a>h)/, :a]]`, "[\"ll\", nil, \"l\", \"h\"]\n"},
		{`p "phone: 555-1234"[/(\d+)-(\d+)/, 2]`, "\"1234\"\n"},
		{`p "hello".slice(/l+/)`, "\"ll\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
