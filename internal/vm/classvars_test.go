package vm_test

import (
	"strings"
	"testing"
)

// TestClassVariables covers @@class_variables: assignment and reads, sharing
// down the superclass hierarchy, compound and ||=/&&= forms, the uninitialized
// NameError, and the top-level RuntimeError. MRI Ruby 4.0.5.
func TestClassVariables(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class C; @@n = 0; def self.n; @@n; end; def inc; @@n += 1; end; end; c = C.new; c.inc; c.inc; p C.n`, "2\n"},
		// Shared down the hierarchy: a subclass sees the parent's class variable.
		{`class C; @@c = 1; end; class D < C; def self.c; @@c; end; end; p D.c`, "1\n"},
		// Counter idiom across instances.
		{`class K; @@count = 0; def initialize; @@count += 1; end; def self.count; @@count; end; end; K.new; K.new; K.new; p K.count`, "3\n"},
		// ||= memoization on an as-yet-undefined class variable (no NameError).
		{`class M; def data; @@d ||= "loaded"; end; end; m = M.new; p [m.data, m.data]`, "[\"loaded\", \"loaded\"]\n"},
		{`class C; @@v ||= 9; def self.v; @@v; end; end; p C.v`, "9\n"},
		{`class C; @@v = 1; @@v ||= 9; def self.v; @@v; end; end; p C.v`, "1\n"},
		{`class C; @@x = 5; def self.go; @@x &&= 10; end; end; p C.go`, "10\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// An uninitialized class variable read raises NameError.
	if err := runErr(t, `class C; def get; @@missing; end; end; C.new.get`); err == nil ||
		!strings.Contains(err.Error(), "uninitialized class variable @@missing") {
		t.Errorf("uninitialized: got %v want NameError", err)
	}
	// Class-variable access whose lexical class is Object raises RuntimeError:
	// at the top level (read and write) and in a method defined on Object.
	for _, src := range []string{
		`@@top = 5`,
		`p @@y`,
		`def foo; @@z = 1; end; foo`,
		`@@t ||= 1`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "class variable access from toplevel") {
			t.Errorf("toplevel src=%q: got %v want RuntimeError", src, err)
		}
	}
}
