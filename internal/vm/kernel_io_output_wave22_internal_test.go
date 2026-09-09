// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// TestKernelPReturnValues covers nativeP's three return-value arms: nil for no
// argument, the argument itself for a single one, and a NEW Array of the
// arguments for several. Reference: ruby/ruby v3_4_0 io.c (rb_f_p). Verified
// byte-for-byte against MRI 4.0.5.
func TestKernelPReturnValues(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// No argument: #p writes nothing and returns nil.
		{"no_args", `x = p(); p x`, "nil"},
		// One argument: #p returns that same object (identity preserved).
		{"one_arg", `s = "s"; r = p(s); p r.equal?(s)`, "\"s\"\ntrue"},
		// Several arguments: #p returns a new Array of them.
		{"multi_args", `a = p(1, 2); p a.class; p a`, "1\n2\nArray\n[1, 2]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestKernelPInspectDispatch covers pInspect: #p renders each argument through
// the object's own #inspect (so an overridden #inspect, and the per-element
// #inspect used inside Array#inspect, are honoured), and coerces a non-String
// #inspect result with #to_s exactly as MRI's rb_inspect/rb_obj_as_string do.
func TestKernelPInspectDispatch(t *testing.T) {
	// A non-String #inspect result is coerced with #to_s (here 42 -> "42").
	if got := runSrc(t, `class X; def inspect; 42; end; end; p X.new`); got != "42" {
		t.Fatalf("non-String inspect: got %q want %q", got, "42")
	}
	// An element's overridden #inspect is used inside Array#inspect.
	if got := runSrc(t, `class Y; def inspect; "Y!"; end; end; p [Y.new]`); got != "[Y!]" {
		t.Fatalf("array element inspect: got %q want %q", got, "[Y!]")
	}
}

// TestKernelPrint covers nativePrint: it writes through $stdout.write, joins
// several arguments with the output field separator $, terminates with the
// output record separator $\, and with no arguments writes $_ (the last line
// read). Reference: ruby/ruby v3_4_0 io.c (rb_f_print -> rb_io_print).
func TestKernelPrint(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"plain", `print "ab"`, "ab"},
		{"dollar_underscore", `$_ = "foo"; print`, "foo"},
		{"no_arg_no_lastline", `print`, ""},
		{"multi_no_separator", `print "a", "b"`, "ab"},
		{"separators", `$, = "-"; $\ = "!"; print "a", "b"`, "a-b!"},
		// A $stdout that answers no #write falls back to the underlying stream (MRI
		// rejects such a $stdout at assignment; rbgo tolerates it).
		{"fallback_no_write", `$stdout = Object.new; print "x"`, "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestKernelPutc covers the Kernel#putc closure: a normal call forwards a single
// character to $stdout (an Integer's low byte, or a String's first character) and
// returns its argument; a wrong argument count raises ArgumentError before any
// output. Reference: ruby/ruby v3_4_0 io.c (rb_f_putc).
func TestKernelPutc(t *testing.T) {
	if got := runSrc(t, `r = putc(65); print " "; print r`); got != "A 65" {
		t.Fatalf("putc(65): got %q want %q", got, "A 65")
	}
	if got := runSrc(t, `r = putc("XY"); print " "; print r`); got != "X XY" {
		t.Fatalf(`putc("XY"): got %q want %q`, got, "X XY")
	}
	// An empty String writes nothing (the len==0 arm) and returns "".
	if got := runSrc(t, `r = putc ""; print r.inspect`); got != `""` {
		t.Fatalf(`putc(""): got %q want %q`, got, `""`)
	}
	// A $stdout that answers no #write falls back to the underlying stream.
	if got := runSrc(t, `$stdout = Object.new; putc 65`); got != "A" {
		t.Fatalf("putc fallback: got %q want %q", got, "A")
	}
	// Wrong argument count raises ArgumentError with MRI's message, and nothing is
	// written.
	for _, tc := range []struct{ src, want string }{
		{`putc`, "wrong number of arguments (given 0, expected 1)"},
		{`putc(1, 2)`, "wrong number of arguments (given 2, expected 1)"},
	} {
		err := runStragglerErr(t, tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
		}
	}
}

// TestKernelOutputToReassignedStdout covers output through a custom $stdout that
// answers #write: #puts forwards to $stdout.puts and, when that resolves back to
// Kernel#puts (recv==$stdout), writes straight to the object's #write instead of
// recursing (MRI's rb_f_puts guard); #putc/#print deliver the bytes to
// $stdout.write directly. Verified byte-for-byte against MRI 4.0.5.
func TestKernelOutputToReassignedStdout(t *testing.T) {
	// $stdout is restored before the captured value is printed via #inspect (so a
	// trailing newline the harness would trim stays visible inside the quotes).
	const foo = `class Foo; def write(*a); @b = (@b || "") + a.join; end; def b; @b || ""; end; end; f = Foo.new; o = $stdout; $stdout = f; `
	cases := []struct{ name, src, want string }{
		{"puts", foo + `puts "z"; $stdout = o; print f.b.inspect`, `"z\n"`},
		{"print", foo + `print "y"; $stdout = o; print f.b.inspect`, `"y"`},
		{"putc_int", foo + `putc 66; $stdout = o; print f.b.inspect`, `"B"`},
		{"putc_str", foo + `putc "QZ"; $stdout = o; print f.b.inspect`, `"Q"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestKernelPutsFallbackNoWrite covers nativePuts' branch for a $stdout that
// answers no #write: MRI rejects such a $stdout at assignment, but rbgo tolerates
// it and writes to the underlying stream rather than raising.
func TestKernelPutsFallbackNoWrite(t *testing.T) {
	if got := runSrc(t, `$stdout = Object.new; puts "x"`); got != "x" {
		t.Fatalf("puts fallback: got %q want %q", got, "x")
	}
}

// TestKernelPutcVisibility covers the module_function split registered for putc:
// it is a private instance method of Kernel (so a bare `putc c` works but
// `obj.putc c` does not) and a public method on the Kernel module. Verified
// against MRI 4.0.5.
func TestKernelPutcVisibility(t *testing.T) {
	if got := runSrc(t, `p Kernel.private_instance_methods(false).include?(:putc)`); got != "true" {
		t.Fatalf("putc private_instance_method: got %q want true", got)
	}
	if got := runSrc(t, `p Kernel.public_methods(false).include?(:putc)`); got != "true" {
		t.Fatalf("putc Kernel public method: got %q want true", got)
	}
}
