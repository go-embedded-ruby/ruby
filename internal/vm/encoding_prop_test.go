package vm_test

import "testing"

// TestEncodingPropagation covers the binary (ASCII-8BIT) encoding flowing through
// concatenation, repetition and slicing, so a derived binary string keeps counting
// bytes. Asserted against MRI Ruby 4.0.5.
func TestEncodingPropagation(t *testing.T) {
	cases := []struct{ src, want string }{
		// + / * / [] keep the receiver's encoding.
		{`p [("x".b + "yz").encoding.name, ("x".b + "yz").length]`, "[\"ASCII-8BIT\", 3]\n"},
		{`p [("a".b * 3).encoding.name, ("a".b * 3).length]`, "[\"ASCII-8BIT\", 3]\n"},
		{`p ["café".b[0, 2].encoding.name, "café".b[0, 2].length, "hello".b[1..3]]`, "[\"ASCII-8BIT\", 2, \"ell\"]\n"},
		// A slice that does not match returns nil (no encoding to carry).
		{`p "hello".b["xyz"]`, "nil\n"},
		// Normal UTF-8 strings are unaffected.
		{`p [("a" + "b").encoding.name, "café"[0, 2], "café"[0, 2].encoding.name, "café"[0, 2].length]`, "[\"UTF-8\", \"ca\", \"UTF-8\", 2]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
