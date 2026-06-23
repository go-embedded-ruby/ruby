package vm_test

import (
	"strings"
	"testing"
)

// TestClassReflectionAndInteger covers Module#name and the Module hierarchy
// operators (< <= > >=), Class.new (anonymous classes), and Integer() base/prefix
// parsing. Asserted against MRI Ruby 4.0.5.
func TestClassReflectionAndInteger(t *testing.T) {
	cases := []struct{ src, want string }{
		// Module#name (nil for an anonymous class).
		{`p [Integer.name, [].class.name, Comparable.name, Class.new.name]`, "[\"Integer\", \"Array\", \"Comparable\", nil]\n"},
		// Hierarchy operators: descendant < ancestor; equal for <=/>=; nil if unrelated.
		{`p [Integer < Numeric, Numeric < Integer, Integer <= Integer, Integer > Numeric, Numeric >= Integer]`, "[true, false, true, false, true]\n"},
		{`p [String < Integer, String < Comparable]`, "[nil, true]\n"},
		// Class.new: anonymous class, optional superclass, class-body block.
		{`p Class.new.is_a?(Class)`, "true\n"},
		{`k = Class.new { def greet; "hi"; end }; p k.new.greet`, "\"hi\"\n"},
		{`k = Class.new { def initialize(n); @n = n; end; def n; @n; end }; p k.new(7).n`, "7\n"},
		{`base = Class.new { def kind; "base"; end }; p Class.new(base).new.kind`, "\"base\"\n"},
		{`p Class.new(Object).superclass`, "Object\n"},
		// Integer(): auto-detect a prefix with no base; accept a matching prefix
		// with an explicit base; underscores; signs.
		{`p [Integer("0xff"), Integer("0b101"), Integer("0o17"), Integer("42"), Integer("1_000")]`, "[255, 5, 15, 42, 1000]\n"},
		{`p [Integer("0xff", 16), Integer("-0x10", 16), Integer("ff", 16), Integer("777", 8)]`, "[255, -16, 255, 511]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`Integer < 5`, "compared with non class/module"},
		{`Class.new(5)`, "superclass must be an instance of Class"},
		{`Integer("zz", 16)`, `invalid value for Integer(): "zz"`},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
