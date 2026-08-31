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

func TestStringSubGsubResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"backref_plus_last_paren", `p "hello".gsub(/(l)o/, '<\+>')`, "\"hel<l>\"\n"},
		{"backref_plus_no_capture", `p "hello".sub(/l/, '<\+>')`, "\"he<>lo\"\n"},
		{"sub_ignores_block", `p "food".sub(/f/, "g"){"w"}`, "\"good\"\n"},
		{"gsub_ignores_block", `p "food".gsub(/o/, "0"){"w"}`, "\"f00d\"\n"},
		{"sub_to_str_replacement", `class R; def to_str; "X"; end; end; p "abc".sub(/b/, R.new)`, "\"aXc\"\n"},
		{"sub_to_str_pattern", `class P; def to_str; "b"; end; end; p "abc".sub(P.new, "X")`, "\"aXc\"\n"},
		{"gsub_hash", `p "hello".gsub(/[el]/, "e" => "3", "l" => "1")`, "\"h311o\"\n"},
		{"gsub_enum_size_nil", `p "hello".gsub(/l/).size`, "nil\n"},
	})
}

func TestStringScanResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"basic", `p "cruel world".scan(/\w+/)`, "[\"cruel\", \"world\"]\n"},
		{"sets_match", `"hello.".scan(/.(.)/); p $~[0]`, "\"o.\"\n"},
		{"match_nil_after_none", `"hello.".scan(/xyz/); p $~`, "nil\n"},
		{"block_sees_match", `m=[]; "hello".scan(/([aeiou])/){ m << $~.offset(0) }; p m`, "[[1, 2], [4, 5]]\n"},
		{"restores_after_block", `"hello".scan(/./){ "ok".match(/./) }; p $~[0]`, "\"o\"\n"},
		{"to_str_pattern", `class SP; def to_str; "o"; end; end; p "o_o".scan(SP.new)`, "[\"o\", \"o\"]\n"},
	})
}

func TestStringCasecmpResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"ascii", `p "A".casecmp("a")`, "0\n"},
		{"nonascii_no_fold", `p "Ã".casecmp("ã")`, "-1\n"},
		{"nonascii_umlaut", `p "Ã".casecmp("Ä")`, "-1\n"},
		{"no_fold_sharp_s", `p "ß".casecmp("ss")`, "1\n"},
		{"to_str", `class CO; def to_str; "abc"; end; end; p "abc".casecmp(CO.new)`, "0\n"},
		{"nil_when_unconvertible", `p "abc".casecmp(123)`, "nil\n"},
		{"subclass", `class MS < String; end; p "a".casecmp(MS.new("b"))`, "-1\n"},
		{"q_equal", `p "abc".casecmp?("ABC")`, "true\n"},
		{"q_unicode_fold", `p "äöü".casecmp?("ÄÖÜ")`, "true\n"},
		{"q_sharp_s", `p "ß".casecmp?("ss")`, "true\n"},
		{"q_unrelated", `p "Ã".casecmp?("Ä")`, "false\n"},
		{"q_nil_unconvertible", `p "abc".casecmp?(123)`, "nil\n"},
		{"incompatible_encoding_nil", `p "あれ".casecmp("れ".encode("EUC-JP"))`, "nil\n"},
		{"q_incompatible_encoding_nil", `p "あれ".casecmp?("れ".encode("EUC-JP"))`, "nil\n"},
	})
}

func TestStringCoerceArgNonStringToStr(t *testing.T) {
	// #to_str returning a non-String raises TypeError with MRI's message.
	err := runErr(t, `class B; def to_str; 5; end; end; "x".chomp(B.new)`)
	if err == nil || !strings.Contains(err.Error(), "can't convert B to String (B#to_str gives Integer)") {
		t.Fatalf("chomp(bad to_str): %v", err)
	}
	// casecmp with a #to_str that returns a non-String also raises TypeError.
	err = runErr(t, `class B2; def to_str; 42; end; end; "abc".casecmp(B2.new)`)
	if err == nil || !strings.Contains(err.Error(), "can't convert B2 to String") {
		t.Fatalf("casecmp(bad to_str): %v", err)
	}
}

func TestStringSearchIncompatibleEncoding(t *testing.T) {
	// index/rindex negotiate the needle's encoding and raise on incompatibility.
	for _, m := range []string{"index", "rindex"} {
		err := runErr(t, `"あ".`+m+`("れ".encode("EUC-JP"))`)
		if err == nil || !strings.Contains(err.Error(), "CompatibilityError") {
			t.Fatalf("%s incompatible encoding: %v", m, err)
		}
	}
}

func TestStringPartitionSubclassSeparator(t *testing.T) {
	// A String-subclass separator is unwrapped (regexpSep) and matched literally.
	if got := eval(t, `class SubSep < String; end; p "hello".partition(SubSep.new("l"))`); got != "[\"he\", \"l\", \"lo\"]\n" {
		t.Fatalf("partition subclass sep: %q", got)
	}
	// split with no positional argument falling back to $; exercises the empty-args
	// substitution path.
	if got := eval(t, `$; = ","; r = "a,b".split; $; = nil; p r`); got != "[\"a\", \"b\"]\n" {
		t.Fatalf("split no-arg $;: %q", got)
	}
}

func TestStringChompResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"newline_sep_removes_cr", `p "abc\r\r".chomp("\n")`, "\"abc\\r\"\n"},
		{"newline_sep_crlf", `p "abc\r\n\r\n".chomp("\n")`, "\"abc\\r\\n\"\n"},
		{"nil_no_chomp", `p "abc\r\n".chomp(nil)`, "\"abc\\r\\n\"\n"},
		{"custom_dollar_slash", `$/ = "cdef"; p "abcdef".chomp`, "\"ab\"\n"},
		{"default_smart", `$/ = "\n"; p "abc\r\r".chomp`, "\"abc\\r\"\n"},
		{"to_str_arg", `class ChA; def to_str; "bc"; end; end; p "abc".chomp(ChA.new)`, "\"a\"\n"},
		{"paragraph_mode", `p "abc\n\n\n".chomp("")`, "\"abc\"\n"},
		{"bang_nil_returns_nil", `p "abc".chomp!(nil)`, "nil\n"},
	})
}

func TestStringPartitionResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"partition_regexp", `p "hello!".partition(/l./)`, "[\"he\", \"ll\", \"o!\"]\n"},
		{"rpartition_regexp", `p "hello!".rpartition(/l./)`, "[\"hel\", \"lo\", \"!\"]\n"},
		{"partition_sets_globals", `"hello!".partition(/(.l)(.o)/); p [$1, $2]`, "[\"el\", \"lo\"]\n"},
		{"partition_string", `p "hello".partition("l")`, "[\"he\", \"l\", \"lo\"]\n"},
		{"rpartition_string", `p "hello".rpartition("l")`, "[\"hel\", \"l\", \"o\"]\n"},
		{"partition_no_match", `p "hello".partition("x")`, "[\"hello\", \"\", \"\"]\n"},
		{"rpartition_no_match", `p "hello".rpartition("x")`, "[\"\", \"\", \"hello\"]\n"},
		{"partition_to_str", `class LP; def to_str; "l"; end; end; p "hello".partition(LP.new)`, "[\"he\", \"l\", \"lo\"]\n"},
		{"partition_typeerror", `begin; "hello".partition(5); rescue TypeError; puts "te"; end`, "te\n"},
	})
}

func TestStringCharsetResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"tr_multibyte_to_single", `p "über".tr("ü", "u")`, "\"uber\"\n"},
		{"tr_single_to_multibyte", `p "uber".tr("u", "ü")`, "\"über\"\n"},
		{"tr_negate", `p "hello".tr("^aeiou", "*")`, "\"*e**o\"\n"},
		{"tr_range", `p "hello".tr("a-y", "b-z")`, "\"ifmmp\"\n"},
		{"tr_s_squeeze", `p "mississippi".tr_s("sp", "*")`, "\"mi*i*i*i\"\n"},
		{"count_multibyte", `p "abcが".count("が")`, "1\n"},
		{"delete_multibyte", `p "我倒是".delete("倒")`, "\"我是\"\n"},
		{"delete_to_str", `class DF; def to_str; "l"; end; end; p "hello".delete(DF.new)`, "\"heo\"\n"},
		{"squeeze_range", `p "aaabbbccc".squeeze("a-b")`, "\"abccc\"\n"},
		{"tr_to_str_args", `class TA; def to_str; "e"; end; end; class TB; def to_str; "i"; end; end; p "hello".tr(TA.new, TB.new)`, "\"hillo\"\n"},
		{"tr_reversed_range_raises", `begin; "x".tr("z-a", "y"); rescue ArgumentError; puts "ae"; end`, "ae\n"},
		{"count_to_str_arg", `class CA; def to_str; "l"; end; end; p "hello".count(CA.new)`, "2\n"},
	})
}

func TestStringBytesCodepointsBlock(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"bytes_block_returns_self", `p "ab".bytes { |b| }`, "\"ab\"\n"},
		{"bytes_block_yields", `a=[]; "ab".bytes { |b| a << b }; p a`, "[97, 98]\n"},
		{"codepoints_block_returns_self", `p "ab".codepoints { |c| }`, "\"ab\"\n"},
		{"codepoints_block_yields", `a=[]; "abcd".codepoints { |c| a << c }; p a`, "[97, 98, 99, 100]\n"},
		{"bytes_no_block", `p "ab".bytes`, "[97, 98]\n"},
	})
}

func TestStringSplitResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"awk_trailing_positive_limit", `p " 1 2 ".split(" ", 3)`, "[\"1\", \"2\", \"\"]\n"},
		{"awk_trailing_neg_limit", `p " a  b  ".split(" ", -1)`, "[\"a\", \"b\", \"\"]\n"},
		{"awk_no_trailing_default", `p " a  b  ".split(" ")`, "[\"a\", \"b\"]\n"},
		{"awk_limit_remainder", `p " 1 2 ".split(" ", 2)`, "[\"1\", \"2 \"]\n"},
		{"dollar_semi_string", `$; = ","; r = "x,y,z".split(nil); $; = nil; p r`, "[\"x\", \"y\", \"z\"]\n"},
		{"dollar_semi_regexp", `$; = /:/; r = "1:2:".split(nil, -1); $; = nil; p r`, "[\"1\", \"2\", \"\"]\n"},
		{"dollar_semi_nil_whitespace", `$; = nil; p "a b c".split`, "[\"a\", \"b\", \"c\"]\n"},
	})
}

func TestStringMiscResiduals(t *testing.T) {
	evalCases(t, []struct{ name, src, want string }{
		{"replace_to_str", `class RP; def to_str; "hi"; end; end; p "x".replace(RP.new)`, "\"hi\"\n"},
		{"next_is_succ", `p "az".next`, "\"ba\"\n"},
		{"next_bang", `s = "az"; s.next!; p s`, "\"ba\"\n"},
		{"each_line_size_nil", `p "a\nb\nc".each_line.size`, "nil\n"},
		{"each_line_custom_dollar_slash", `$/ = ":"; r = []; "a:b:c".each_line { |l| r << l }; $/ = nil; p r`, "[\"a:\", \"b:\", \"c\"]\n"},
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
