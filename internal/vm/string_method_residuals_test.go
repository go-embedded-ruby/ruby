package vm_test

import (
	"strings"
	"testing"
)

// evalCases runs a table of Ruby snippets, each expected to print want.
func evalCases(t *testing.T, cases []struct{ name, src, want string }) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringIndexResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"regexp_basic", `p "hello".index(/l/)`, "2\n"},
		{"regexp_none", `p "hello".index(/z/)`, "nil\n"},
		{"regexp_offset", `p "blablabla".index(/bla/, 1)`, "3\n"},
		{"regexp_sets_match", `"hello".index(/l/); p $~[0]`, "\"l\"\n"},
		{"regexp_multibyte", `p "ちがう".index(/が/)`, "1\n"},
		{"to_str_arg", `class C; def to_str; "b"; end; end; p "abc".index(C.new)`, "1\n"},
		{"to_int_offset", `class O; def to_int; 1; end; end; p "abc".index("c", O.new)`, "2\n"},
		{"subclass_arg", `class S < String; end; p "blablabla".index(S.new("bla"))`, "0\n"},
		{"empty_at_len", `p "abc".index("", 3)`, "3\n"},
		{"neg_offset", `p "blablabla".index("bl", -3)`, "6\n"},
		{"nil_typeerror", `begin; "abc".index(nil); rescue TypeError; puts "te"; end`, "te\n"},
		{"symbol_typeerror", `begin; "abc".index(:a); rescue TypeError; puts "te"; end`, "te\n"},
		{"regexp_clears_match_out_of_range", `"a".index(/a/); "bla".index(/bla/, 99); p $~`, "nil\n"},
		{"string_form_keeps_match", `$~ = nil; "hello".index("ll"); p $~`, "nil\n"},
	})
}

func TestStringRindexResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"string_last", `p "blablabla".rindex("bla")`, "6\n"},
		{"string_offset", `p "blablabla".rindex("bl", 2)`, "0\n"},
		{"string_none", `p "blablabla".rindex("z")`, "nil\n"},
		{"empty_clamped", `p "blablabla".rindex("", 10)`, "9\n"},
		{"empty_neg", `p "hello".rindex("", -6)`, "nil\n"},
		{"regexp_last", `p "blablabla".rindex(/bla/)`, "6\n"},
		{"regexp_dotstar", `p "blablabla".rindex(/.*/)`, "9\n"},
		{"regexp_dotplus", `p "blablabla".rindex(/.+/)`, "8\n"},
		{"regexp_caret", `p "\nblablabla".rindex(/^/)`, "1\n"},
		{"regexp_sets_match", `"blablabla".rindex(/bla/); p $~.begin(0)`, "6\n"},
		{"to_str_arg", `class C2; def to_str; "lo"; end; end; p "hello".rindex(C2.new)`, "3\n"},
		{"subclass_arg", `class S2 < String; end; p "blablabla".rindex(S2.new("bla"))`, "6\n"},
		{"nil_offset_typeerror", `begin; "str".rindex("st", nil); rescue TypeError; puts "te"; end`, "te\n"},
		{"integer_typeerror", `begin; "hello".rindex(42); rescue TypeError; puts "te"; end`, "te\n"},
		{"string_form_keeps_match", `$~ = nil; "hello".rindex("ll"); p $~`, "nil\n"},
	})
}

func TestStringIndexRindexErrorMessages(t *testing.T) {
	if err := runErr(t, `"abc".index(nil)`); err == nil || !strings.Contains(err.Error(), "no implicit conversion") {
		t.Fatalf("index(nil): %v", err)
	}
	if err := runErr(t, `"abc".rindex(:x)`); err == nil || !strings.Contains(err.Error(), "no implicit conversion") {
		t.Fatalf("rindex(:x): %v", err)
	}
}
