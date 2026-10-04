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

// TestWrapperTypesRenderUnchanged is the control for making #puts dispatch
// #to_s. Each built-in wrapper type has TWO entry points to one renderer: a
// Ruby `to_s` method and the Go ToS() that object.Value requires. Dispatching
// means the display path now takes the Ruby one, which the coverage gate
// reported as nineteen ToS() methods falling to 0%.
//
// Nothing about the OUTPUT may change, and this is what says so: every row is
// what ruby 4.0.5 prints and what rbgo printed before the dispatch.
//
// Two rows differ from ruby and are pinned as they are, because they are
// pre-existing and tracked elsewhere: a plain object's address (#756) and
// Enumerator#to_s being over-defined to #inspect (#756 step 4). Pinning the
// current answer rather than ruby's keeps this test about THIS change.
func TestWrapperTypesRenderUnchanged(t *testing.T) {
	src := `require "date"; require "set"; require "uri"; require "ipaddr"; require "matrix"
require "rexml/document"
def show(label, v)
  puts "#{label}=#{(v.to_s rescue "ERR:#{$!.class}").gsub(/0x[0-9a-f]+/, "0xA")}"
end
show "Object",    Object.new
show "Encoding",  Encoding::UTF_8
show "Regexp",    /ab/i
show "Time",      Time.at(0).utc
show "Date",      Date.new(2026, 1, 2)
show "Set",       Set.new([1, 2])
show "URI",       URI.parse("http://a/b")
show "IPAddr",    IPAddr.new("10.0.0.1")
show "Matrix",    Matrix[[1, 2], [3, 4]]
show "Vector",    Vector[1, 2]
show "Enum",      [1, 2].each
show "REXMLDoc",  REXML::Document.new("<a><b/></a>")
show "REXMLElem", REXML::Document.new("<a><b/></a>").root
show "ARGFclass", ARGF.class
`
	const want = `Object=#<Object>
Encoding=UTF-8
Regexp=(?i-mx:ab)
Time=1970-01-01 00:00:00 UTC
Date=2026-01-02
Set=Set[1, 2]
URI=http://a/b
IPAddr=10.0.0.1
Matrix=Matrix[[1, 2], [3, 4]]
Vector=Vector[1, 2]
Enum=#<Enumerator: [1, 2]:each>
REXMLDoc=<a><b/></a>
REXMLElem=<a><b/></a>
ARGFclass=ARGF.class
`
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
