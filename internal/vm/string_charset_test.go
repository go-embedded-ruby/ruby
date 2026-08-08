package vm_test

import (
	"strings"
	"testing"
)

// String tr-style char-set methods: tr/tr_s/tr!/tr_s!, count, delete/delete!,
// squeeze/squeeze!, and the standalone chr/setbyte/succ!/upto/sum. Every case is
// checked byte-exact against MRI ruby 4.x semantics.
func TestStringCharset(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// tr: plain mapping, ranges, negation, shorter/empty to, dup last-wins.
		{"tr_basic", `p "hello".tr("el", "ip")`, "\"hippo\"\n"},
		{"tr_range", `p "hello".tr("a-y", "b-z")`, "\"ifmmp\"\n"},
		{"tr_neg", `p "hello".tr("^aeiou", "*")`, "\"*e**o\"\n"},
		{"tr_neg_empty_to", `p "hello".tr("^l", "")`, "\"ll\"\n"},
		{"tr_to_shorter", `p "hello".tr("el", "*")`, "\"h***o\"\n"},
		{"tr_empty_to_deletes", `p "hello".tr("l", "")`, "\"heo\"\n"},
		{"tr_dup_last_wins", `p "hello".tr("ll", "12")`, "\"he22o\"\n"},
		{"tr_no_match", `p "hello".tr("z", "0")`, "\"hello\"\n"},
		{"tr_escaped_dash", `p "a-c".tr("a\\-c", "x")`, "\"xxx\"\n"},
		{"tr_lone_caret", `p "x^y".tr("^", "z")`, "\"xzy\"\n"},
		{"tr_escaped_range_end", `p "X".tr("A-\\\\", "*")`, "\"*\"\n"},
		// tr_s: translate then squeeze the affected runs.
		{"trs_squeeze", `p "hello".tr_s("l", "r")`, "\"hero\"\n"},
		{"trs_collapse", `p "hello".tr_s("el", "*")`, "\"h*o\"\n"},
		{"trs_all", `p "aabbcc".tr_s("a-z", "*")`, "\"*\"\n"},
		{"trs_delete", `p "hello".tr_s("l", "")`, "\"heo\"\n"},
		{"trs_mixed", `p "mississippi".tr_s("sp", "*")`, "\"mi*i*i*i\"\n"},
		{"trs_neg", `p "hello".tr_s("^a", "*")`, "\"*\"\n"},
		// tr!/tr_s! bangs return nil when unchanged.
		{"tr_bang", `s = "hello"; s.tr!("l", "L"); p s`, "\"heLLo\"\n"},
		{"tr_bang_nochange", `p "abc".tr!("x", "y")`, "nil\n"},
		{"trs_bang", `s = "aabbcc"; s.tr_s!("a-c", "*"); p s`, "\"*\"\n"},
		{"trs_bang_nochange", `p "abc".tr_s!("x", "y")`, "nil\n"},
		// count: single/multi-arg intersection, negation, ranges.
		{"count_basic", `p "hello world".count("lo")`, "5\n"},
		{"count_intersect", `p "hello world".count("lo", "o")`, "2\n"},
		{"count_neg", `p "hello world".count("^l")`, "8\n"},
		{"count_range", `p "hello world".count("ej-m")`, "4\n"},
		{"count_escaped_caret", `p "hello^world".count("\\^")`, "1\n"},
		{"count_intersect_neg", `p "hello world".count("l", "^o")`, "3\n"},
		// delete/delete!: intersection semantics, negation.
		{"delete_basic", `p "hello".delete("l")`, "\"heo\"\n"},
		{"delete_pair", `p "hello".delete("lo")`, "\"he\"\n"},
		{"delete_intersect", `p "hello".delete("l", "lo")`, "\"heo\"\n"},
		{"delete_neg", `p "hello world".delete("^aeiou")`, "\"eoo\"\n"},
		{"delete_bang", `s = "hello"; s.delete!("l"); p s`, "\"heo\"\n"},
		{"delete_bang_nochange", `p "abc".delete!("z")`, "nil\n"},
		// squeeze/squeeze!: all, selector, range, negation.
		{"squeeze_all", `p "aaabbbccc".squeeze`, "\"abc\"\n"},
		{"squeeze_sel", `p "aaabbbccc".squeeze("a")`, "\"abbbccc\"\n"},
		{"squeeze_range", `p "aaabbbccc".squeeze("a-b")`, "\"abccc\"\n"},
		{"squeeze_neg", `p "aaabbbccc".squeeze("^a")`, "\"aaabc\"\n"},
		{"squeeze_bang", `s = "aaabbb"; s.squeeze!; p s`, "\"ab\"\n"},
		{"squeeze_bang_nochange", `p "abc".squeeze!`, "nil\n"},
		// chr: empty, ASCII, leading multibyte.
		{"chr_ascii", `p "hello".chr`, "\"h\"\n"},
		{"chr_empty", `p "".chr`, "\"\"\n"},
		{"chr_multibyte", `p "あbc".chr`, "\"あ\"\n"},
		// setbyte: in place, negative index, returns the value argument.
		{"setbyte", `s = "hello"; s.setbyte(0, 72); p s`, "\"Hello\"\n"},
		{"setbyte_returns_val", `s = "hello"; p s.setbyte(1, 65)`, "65\n"},
		{"setbyte_neg", `s = "hello"; s.setbyte(-1, 88); p s`, "\"hellX\"\n"},
		// succ!/next!: mutate in place; empty stays empty and returns self.
		{"succ_bang", `s = "az"; s.succ!; p s`, "\"ba\"\n"},
		{"succ_bang_returns_self", `s = "a"; p s.succ!.equal?(s)`, "true\n"},
		{"next_bang", `s = "Az"; s.next!; p s`, "\"Ba\"\n"},
		{"succ_bang_empty", `s = ""; p s.succ!`, "\"\"\n"},
		// upto: numeric branch (width preserved), string branch, exclusive, edges.
		{"upto_str", `p "a8".upto("b1").to_a`, "[\"a8\", \"a9\", \"b0\", \"b1\"]\n"},
		{"upto_numeric", `p "9".upto("11").to_a`, "[\"9\", \"10\", \"11\"]\n"},
		{"upto_numeric_zeropad", `p "007".upto("011").to_a`, "[\"007\", \"008\", \"009\", \"010\", \"011\"]\n"},
		{"upto_numeric_desc_empty", `p "10".upto("8").to_a`, "[]\n"},
		{"upto_numeric_excl", `p "1".upto("3", true).to_a`, "[\"1\", \"2\"]\n"},
		{"upto_str_excl", `p "a".upto("e", true).to_a`, "[\"a\", \"b\", \"c\", \"d\"]\n"},
		{"upto_desc_empty", `p "z".upto("aa").to_a`, "[]\n"},
		{"upto_empty_start", `p "".upto("a").to_a`, "[\"\"]\n"},
		{"upto_len_overrun", `p "az".upto("b").to_a`, "[\"az\"]\n"},
		{"upto_inclusive_end", `p "a".upto("c").to_a`, "[\"a\", \"b\", \"c\"]\n"},
		{"upto_block_returns_self", `s = "a"; p s.upto("c") {}.equal?(s)`, "true\n"},
		// sum: default 16-bit, explicit bits, and 0 (full sum).
		{"sum_default", `p "hello".sum`, "532\n"},
		{"sum_bits", `p "hello".sum(8)`, "20\n"},
		{"sum_zero_full", `p "hello".sum(0)`, "532\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringCharsetErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"tr_reversed_range", `"abc".tr("c-a", "x")`, "invalid range"},
		{"count_no_args", `"abc".count`, "wrong number of arguments"},
		{"delete_no_args", `"abc".delete`, "wrong number of arguments"},
		{"setbyte_oob", `"a".setbyte(5, 1)`, "out of string"},
		{"setbyte_frozen", `"hi".freeze.setbyte(0, 1)`, "FrozenError"},
		{"succ_bang_frozen", `s = "a"; s.freeze; s.succ!`, "FrozenError"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}
