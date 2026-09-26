// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestSearchEncodingCompatibility pins the Encoding::CompatibilityError that
// string.c v3_4_0 raises out of rb_reg_search's rb_reg_prepare_re when the
// pattern cannot match the subject at all. String#=~ and String#match already
// checked; #index, #rindex, #byteindex and #byterindex went straight to the
// engine and quietly answered nil.
func TestSearchEncodingCompatibility(t *testing.T) {
	const eucRe = `re = Regexp.new("れ".encode(Encoding::EUC_JP))` + "\n"
	cases := []struct{ name, src, class, msg string }{
		{"index_regexp", eucRe + `"あれ".index(re)`, "Encoding::CompatibilityError",
			"incompatible encoding regexp match (EUC-JP regexp with UTF-8 string)"},
		{"rindex_regexp", eucRe + `"あれ".rindex(re)`, "Encoding::CompatibilityError",
			"incompatible encoding regexp match (EUC-JP regexp with UTF-8 string)"},
		{"byteindex_regexp", eucRe + `"あれ".byteindex(re)`, "Encoding::CompatibilityError",
			"incompatible encoding regexp match (EUC-JP regexp with UTF-8 string)"},
		{"byterindex_regexp", eucRe + `"あれ".byterindex(re)`, "Encoding::CompatibilityError",
			"incompatible encoding regexp match (EUC-JP regexp with UTF-8 string)"},
		// rb_str_upto_each's opening rb_enc_check.
		{"upto_incompatible", `'a'.dup.force_encoding("EUC-JP").upto('b'.dup.force_encoding("ISO-2022-JP")) {}`,
			"Encoding::CompatibilityError", "incompatible character encodings: EUC-JP and ISO-2022-JP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, msg := evalErr(t, tc.src)
			if class != tc.class || !strings.Contains(msg, tc.msg) {
				t.Fatalf("src=%q got %s: %q want %s: %q", tc.src, class, msg, tc.class, tc.msg)
			}
		})
	}
	// The compatible cases still work.
	ok := []struct{ name, src, want string }{
		{"index_compatible", `p "あれ".index(/れ/)`, "1\n"},
		{"rindex_compatible", `p "あれ".rindex(/れ/)`, "1\n"},
		{"byteindex_compatible", `p "あれ".byteindex(/れ/)`, "3\n"},
		{"byterindex_compatible", `p "あれ".byterindex(/れ/)`, "3\n"},
		{"upto_compatible", `
out = []
'a'.upto('c') { |c| out << c }
p out`, "[\"a\", \"b\", \"c\"]\n"},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestHashSubclassOperand pins hash.c v3_4_0's `to_hash` helper: an operand
// that is already T_HASH — a Hash SUBCLASS instance included — is used as is,
// so #to_hash is never called on one. This only became observable once Hash.[]
// started answering subclass instances.
func TestHashSubclassOperand(t *testing.T) {
	const prelude = `
class ToHashHash < Hash
  def to_hash = { "to_hash" => "was", "called!" => "duh." }
end
`
	tests := []struct{ name, src, want string }{
		{"merge", `p({ 3 => 4 }.merge(ToHashHash[1 => 2]))`, "{3 => 4, 1 => 2}\n"},
		{"merge_bang", `p({ 3 => 4 }.merge!(ToHashHash[1 => 2]))`, "{3 => 4, 1 => 2}\n"},
		{"replace", `
h = {}
h.replace(ToHashHash[1 => 2])
p h`, "{1 => 2}\n"},
		// A non-Hash operand still goes through #to_hash.
		{"non_hash_uses_to_hash", `
class Convertible
  def to_hash = { 1 => 2 }
end
p({ 3 => 4 }.merge(Convertible.new))`, "{3 => 4, 1 => 2}\n"},
		// rb_hash_s_create allocates through the receiver.
		{"subclass_constructor", `
class MyHash < Hash; end
p [MyHash[[[1, 2]]].class, MyHash[1, 2].class, MyHash[{ 1 => 2 }].class, Hash[1, 2].class]`,
			"[MyHash, MyHash, MyHash, Hash]\n"},
		{"subclass_constructor_skips_initialize", `
class MyInitializerHash < Hash
  def initialize = raise("Constructor called")
end
p MyInitializerHash[1 => 2].class`, "MyInitializerHash\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, prelude+tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
