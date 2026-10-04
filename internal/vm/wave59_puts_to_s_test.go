// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestPutsAndPrintDispatchToS: #puts and #print render their arguments with
// MRI's rb_obj_as_string, which DISPATCHES #to_s. rbgo reserved that dispatch
// for a user object (*RObject) and took the Go-level ToS() for every built-in
// value type -- a fast path with no guard, observable on seven of the nine core
// classes at once:
//
//	class Integer; def to_s; "HIJACKED"; end; end
//	puts 1     ruby 4.0.5  HIJACKED    before  1
//	print 1    ruby 4.0.5  HIJACKED    before  1
//
// Each row is a separate eval, because a redefinition is global.
func TestPutsAndPrintDispatchToS(t *testing.T) {
	for _, tc := range []struct{ name, cls, val, want string }{
		{"Integer", "Integer", "1", "HIJACKED\nHIJACKED"},
		{"Float", "Float", "1.5", "HIJACKED\nHIJACKED"},
		{"Symbol", "Symbol", ":sym", "HIJACKED\nHIJACKED"},
		{"Hash", "Hash", "{a: 1}", "HIJACKED\nHIJACKED"},
		{"NilClass", "NilClass", "nil", "HIJACKED\nHIJACKED"},
		{"TrueClass", "TrueClass", "true", "HIJACKED\nHIJACKED"},
		{"Range", "Range", "(1..2)", "HIJACKED\nHIJACKED"},
		// puts recurses into an Array element-wise and never asks it for #to_s,
		// so only print shows the dispatch here. That asymmetry is MRI's
		// (io_puts_ary vs rb_io_write) and is the control for the row above it.
		{"Array", "Array", "[1]", "1\nHIJACKED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "class " + tc.cls + `; def to_s; "HIJACKED"; end; end
v = ` + tc.val + `
puts v
print v`
			if got := eval(t, src); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestPutsLeavesAStringAlone is the other half of rb_obj_as_string: a String is
// used AS IS, subclasses included, so a redefined String#to_s does not change
// what #puts or #print writes. Without that arm the dispatch added above would
// have been wrong in the opposite direction.
func TestPutsLeavesAStringAlone(t *testing.T) {
	src := `class String; def to_s; "HIJACKED"; end; end
puts "s"
print "s"
print "\n"
p "s".to_s`
	const want = "s\ns\n\"HIJACKED\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestPutsLeavesAStringSubclassAlone: RB_TYPE_P(v, T_STRING) covers a subclass
// instance, which in rbgo is an *RObject carrying the String in its builtin
// slot -- a bare type assertion misses it and the dispatch answers the
// subclass's #to_s where ruby answers the string.
func TestPutsLeavesAStringSubclassAlone(t *testing.T) {
	src := `class S < String; def to_s; "SUB"; end; end
s = S.new("x")
puts s
print s
print "\n"
p s.to_s
class T < String; end
puts T.new("y")`
	const want = "x\nx\n\"SUB\"\ny\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestPutsShapesAreUnchanged is the control for everything the fast path was
// already getting right: nested arrays flattened one element per line, a
// self-referential array as "[...]", #to_ary expansion, an empty string writing
// only the separator, a line that already ends in one getting no second, a
// no-argument puts writing a lone newline, and nil writing an empty line. Every
// row is both ruby 4.0.5's answer and what rbgo produced before the dispatch
// was added.
func TestPutsShapesAreUnchanged(t *testing.T) {
	src := `puts [1, [2, 3], 4]
a = [1]; a << a
puts a
class ToAry; def to_ary; [7, 8]; end; end
puts ToAry.new
puts ""
puts "has newline\n"
puts
puts nil
puts [nil]
print "a", "b"
print "\n"`
	const want = "1\n2\n3\n4\n1\n[...]\n7\n8\n\nhas newline\n\n\n\nab\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
