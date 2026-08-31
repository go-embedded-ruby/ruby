// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestSprintfCharCoercion covers formatValue.CoerceChar: MRI's %c operand
// coercion for a user object, dispatching #to_str before #to_int through the
// method table (never #respond_to?, so a BasicObject that defines only one of
// them is coerced without raising NoMethodError). Every expectation is
// byte-for-byte against MRI 4.0.
func TestSprintfCharCoercion(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// #to_str returning a String uses its first character.
		{"to_str_object", `o=Object.new; def o.to_str; "abc"; end; p("%c" % o)`, "\"a\"\n"},
		// #to_str on a BasicObject (no #respond_to?) is still found and used.
		{"to_str_basicobject",
			`o=BasicObject.new; def o.to_str; "Q"; end; ::Kernel.p("%c" % o)`, "\"Q\"\n"},
		// With no #to_str, #to_int supplies a code point.
		{"to_int_object", `o=Object.new; def o.to_int; 90; end; p("%c" % o)`, "\"Z\"\n"},
		// #to_str is preferred over #to_int when both are present.
		{"to_str_over_to_int",
			`o=Object.new; def o.to_str; "x"; end; def o.to_int; 90; end; p("%c" % o)`, "\"x\"\n"},
		// A plain String / Integer argument bypasses the hook (regression guard).
		{"string_native", `p("%c" % "hi")`, "\"h\"\n"},
		{"integer_native", `p("%c" % 97)`, "\"a\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestSprintfCharCoercionErrors covers CoerceChar's raising branches: a #to_str
// or #to_int result of the wrong type, and an object answering neither, each
// raising the MRI TypeError message verbatim.
func TestSprintfCharCoercionErrors(t *testing.T) {
	tests := []struct{ name, src, wantClass, wantMsg string }{
		{"to_str_non_string",
			`o=Object.new; def o.to_str; :foo; end; "%c" % o`,
			"TypeError", "can't convert Object into String"},
		{"to_int_non_integer",
			`o=Object.new; def o.to_int; :foo; end; "%c" % o`,
			"TypeError", "can't convert Object into Integer"},
		{"neither",
			`o=Object.new; "%c" % o`,
			"TypeError", "no implicit conversion of Object into Integer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil {
				t.Fatalf("src=%q: expected %s, got nil", tc.src, tc.wantClass)
			}
			if !strings.Contains(err.Error(), tc.wantClass) || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("src=%q: want %s %q, got %v", tc.src, tc.wantClass, tc.wantMsg, err)
			}
		})
	}
}

// TestSprintfNamedHashDefault covers formatNamedArgs' NamedArgs default
// resolver: a %{name}/%<name>s reference to a key absent from the hash consults
// the hash's own #[] (Hash#default / default_proc). A non-nil default is used; a
// nil one raises MRI's "key{NAME} not found" KeyError; a present key (even a nil
// value) never reaches the resolver.
func TestSprintfNamedHashDefault(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Static default value.
		{"default_value", `p("%{foo}" % Hash.new(123))`, "\"123\"\n"},
		// default_proc.
		{"default_proc", `p("%{foo}" % Hash.new { 123 })`, "\"123\"\n"},
		// %<name>s form also consults the default.
		{"angle_form", `p("%<foo>s" % Hash.new(123))`, "\"123\"\n"},
		// A present key with a nil value renders "" (never hits the default).
		{"present_nil", `p("%{foo}" % {foo: nil})`, "\"\"\n"},
		// A present key with a value wins over any default.
		{"present_value", `p("%{foo}" % Hash.new(9).merge(foo: 1))`, "\"1\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestSprintfNamedHashDefaultKeyError covers the resolver's nil-default branch:
// an absent key whose default is nil (a bare hash, Hash.new(nil), or a proc
// returning nil) raises MRI's KeyError with #key and #receiver set.
func TestSprintfNamedHashDefaultKeyError(t *testing.T) {
	tests := []struct{ name, src string }{
		{"empty_hash", `"%{foo}" % {}`},
		{"default_nil", `"%{foo}" % Hash.new(nil)`},
		{"proc_nil", `"%{foo}" % Hash.new { nil }`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil {
				t.Fatalf("src=%q: expected KeyError, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "KeyError") || !strings.Contains(err.Error(), "key{foo} not found") {
				t.Errorf("src=%q: want KeyError key{foo} not found, got %v", tc.src, err)
			}
		})
	}
	// #key and #receiver are set on the raised KeyError.
	got := eval(t, `h={}; begin; "%{foo}" % h; rescue KeyError => e; p [e.key, e.receiver.equal?(h)]; end`)
	if want := "[:foo, true]\n"; got != want {
		t.Errorf("key/receiver: got=%q want=%q", got, want)
	}
}
