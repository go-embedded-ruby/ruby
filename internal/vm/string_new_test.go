package vm_test

import (
	"strings"
	"testing"
)

// TestStringNew covers String.new, which was falling through to the
// instance-allocating Class#new and producing a bogus object instead of a real
// String. Asserted against MRI Ruby 4.0.5.
func TestStringNew(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [String.new, String.new("abc"), String.new("x").frozen?]`, "[\"\", \"abc\", false]\n"},
		// A copy is independent of its source and a fresh object each call.
		{`s = String.new("hi"); s << " there"; p s`, "\"hi there\"\n"},
		{`p String.new("dup").equal?(String.new("dup"))`, "false\n"},
		{`p String.new("x").upcase`, "\"X\"\n"},
		// Keyword-only arguments (capacity:/encoding:) arrive as a Hash and are ignored.
		{`p [String.new(capacity: 16), String.new("x", capacity: 8)]`, "[\"\", \"x\"]\n"},
		// A subclass of String now yields a working String rather than crashing
		// (its class identity is not yet preserved — a separate limitation).
		{`c = Class.new(String); p c.new("hi").upcase`, "\"HI\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	if err := runErr(t, `String.new(123)`); err == nil || !strings.Contains(err.Error(), "no implicit conversion of Integer into String") {
		t.Errorf("String.new(123) err=%v, want \"no implicit conversion of Integer into String\"", err)
	}
}
