// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestShellwordsBinding exercises the Shellwords module backed by the
// go-ruby-shellwords library (internal/vm/shellwords.go): the module functions
// (shellsplit / shellwords / split, shellescape / escape, shelljoin / join) and
// the String#shellsplit / #shellescape and Array#shelljoin core extensions
// installed on require, the Array#shelljoin element #to_s coercion, and the
// require true/false return. Each expectation is pinned against MRI 4.0.5.
func TestShellwordsBinding(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// --- require returns true first call, false after, then a no-op -----
		{"require_true", `p require "shellwords"`, "true\n"},
		{"require_false", `require "shellwords"; p require "shellwords"`, "false\n"},

		// --- lazy gating: nothing exists before require, MRI-style ----------
		{"gate_const_before", `p defined?(Shellwords)`, "nil\n"},
		{"gate_string_before", `p "x".respond_to?(:shellsplit)`, "false\n"},
		{"gate_array_before", `p [].respond_to?(:shelljoin)`, "false\n"},
		{"gate_const_after", `require "shellwords"; p defined?(Shellwords)`, "\"constant\"\n"},
		{"gate_string_after", `require "shellwords"; p "x".respond_to?(:shellescape)`, "true\n"},
		{"gate_array_after", `require "shellwords"; p [].respond_to?(:shelljoin)`, "true\n"},

		// --- Shellwords.shellsplit / shellwords / split (the splitter) ------
		{"shellsplit_basic", `require "shellwords"; p Shellwords.shellsplit('a b "c d"')`, "[\"a\", \"b\", \"c d\"]\n"},
		{"shellsplit_empty", `require "shellwords"; p Shellwords.shellsplit("")`, "[]\n"},
		{"shellsplit_single_quote", `require "shellwords"; p Shellwords.shellsplit("'a b'")`, "[\"a b\"]\n"},
		{"shellsplit_backslash", `require "shellwords"; p Shellwords.shellsplit('a\ b')`, "[\"a b\"]\n"},
		{"shellwords_alias", `require "shellwords"; p Shellwords.shellwords('a b')`, "[\"a\", \"b\"]\n"},
		{"split_alias", `require "shellwords"; p Shellwords.split('x y')`, "[\"x\", \"y\"]\n"},

		// --- Shellwords.shellescape / escape (the escaper) ------------------
		{"shellescape_quote", `require "shellwords"; p Shellwords.shellescape("it's")`, "\"it\\\\'s\"\n"},
		{"shellescape_empty", `require "shellwords"; p Shellwords.shellescape("")`, "\"''\"\n"},
		{"escape_alias", `require "shellwords"; p Shellwords.escape("z z")`, "\"z\\\\ z\"\n"},

		// --- Shellwords.shelljoin / join (the joiner) -----------------------
		{"shelljoin_basic", `require "shellwords"; p Shellwords.shelljoin(["a","b c"])`, "\"a b\\\\ c\"\n"},
		{"shelljoin_empty", `require "shellwords"; p Shellwords.shelljoin([])`, "\"\"\n"},
		{"shelljoin_single", `require "shellwords"; p Shellwords.shelljoin(["a b"])`, "\"a\\\\ b\"\n"},
		{"join_alias", `require "shellwords"; p Shellwords.join(["p","q r"])`, "\"p q\\\\ r\"\n"},
		// Element #to_s coercion: Integer/Symbol/nil stringify like MRI.
		{"shelljoin_coerce_int", `require "shellwords"; p Shellwords.shelljoin([1, "a b"])`, "\"1 a\\\\ b\"\n"},
		{"shelljoin_coerce_nil", `require "shellwords"; p [nil].shelljoin`, "\"''\"\n"},
		{"shelljoin_coerce_sym", `require "shellwords"; p Shellwords.shelljoin([:sym])`, "\"sym\"\n"},

		// --- String#shellsplit / #shellescape -------------------------------
		{"string_shellsplit", `require "shellwords"; p 'a b "c d"'.shellsplit`, "[\"a\", \"b\", \"c d\"]\n"},
		{"string_shellescape", `require "shellwords"; p "it's".shellescape`, "\"it\\\\'s\"\n"},

		// --- Array#shelljoin ------------------------------------------------
		{"array_shelljoin", `require "shellwords"; p ["a","b c"].shelljoin`, "\"a b\\\\ c\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("src=%q: got %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestShellwordsErrors covers the binding's error re-raises: the library's
// *ArgumentError (an unmatched quote or a NUL character) becomes Ruby's built-in
// ArgumentError with the identical message; a non-String to shellsplit raises
// TypeError; a non-Array to shelljoin raises TypeError. Messages are pinned
// against MRI 4.0.5 (the ArgumentError text is byte-for-byte).
func TestShellwordsErrors(t *testing.T) {
	tests := []struct{ name, src, class, msgPart string }{
		{"unmatched_quote", `require "shellwords"; Shellwords.shellsplit('a "b')`, "ArgumentError", "Unmatched quote at 2: ...\""},
		{"unmatched_quote_string", `require "shellwords"; 'a "b'.shellsplit`, "ArgumentError", "Unmatched quote at 2: ...\""},
		{"nul_character", `require "shellwords"; Shellwords.shellsplit("a" + 0.chr)`, "ArgumentError", "Nul character at 1: ..."},
		{"shellsplit_non_string", `require "shellwords"; Shellwords.shellsplit(5)`, "TypeError", "no implicit conversion"},
		{"shellescape_non_string", `require "shellwords"; Shellwords.shellescape(5)`, "TypeError", "no implicit conversion"},
		{"shelljoin_non_array", `require "shellwords"; Shellwords.shelljoin("x")`, "TypeError", "no implicit conversion of String into Array"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, msg := evalErr(t, tt.src)
			if class != tt.class {
				t.Fatalf("src=%q: got class %q, want %q", tt.src, class, tt.class)
			}
			if !strings.Contains(msg, tt.msgPart) {
				t.Fatalf("src=%q: msg %q missing %q", tt.src, msg, tt.msgPart)
			}
		})
	}
}
