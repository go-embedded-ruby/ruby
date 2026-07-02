package vm_test

import (
	"strings"
	"testing"
)

// In-place String mutation (Stage 2): <<, concat, replace, prepend, insert,
// clear, the bang transforms, sub!/gsub!, []=, and slice!.
func TestStringMutation(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"append_str", `s = "abc"; s << "def"; p s`, "\"abcdef\"\n"},
		{"append_int", `s = "ab"; s << 33; p s`, "\"ab!\"\n"},
		{"append_aliases", "a = \"x\"\nb = a\na << \"y\"\np b", "\"xy\"\n"},
		{"append_returns_self", `s = "a"; p((s << "b").equal?(s))`, "true\n"},
		{"concat_many", `p "foo".dup.concat("bar", "baz")`, "\"foobarbaz\"\n"},
		{"concat_int", `s = "a"; s.concat(98, 99); p s`, "\"abc\"\n"},
		{"replace", `s = "hello"; s.replace("world"); p s`, "\"world\"\n"},
		{"replace_aliases", "a = \"x\"\nb = a\na.replace(\"yy\")\np b", "\"yy\"\n"},
		{"prepend", `s = "bc"; s.prepend("a"); p s`, "\"abc\"\n"},
		{"prepend_many", `s = "c"; s.prepend("a", "b"); p s`, "\"abc\"\n"},
		{"insert_mid", `s = "abc"; s.insert(1, "X"); p s`, "\"aXbc\"\n"},
		{"insert_end_neg", `s = "abc"; s.insert(-1, "Z"); p s`, "\"abcZ\"\n"},
		{"insert_neg2", `s = "abc"; s.insert(-2, "Y"); p s`, "\"abYc\"\n"},
		{"insert_zero", `s = "abc"; s.insert(0, "_"); p s`, "\"_abc\"\n"},
		{"clear", `s = "xyz"; s.clear; p s`, "\"\"\n"},
		{"clear_empty?", `s = "xyz"; s.clear; p s.empty?`, "true\n"},
		{"upcase_bang", `s = "Hello"; s.upcase!; p s`, "\"HELLO\"\n"},
		{"upcase_bang_nochange", `p "ABC".upcase!`, "nil\n"},
		{"downcase_bang", `s = "AbC"; s.downcase!; p s`, "\"abc\"\n"},
		{"capitalize_bang", `s = "hello"; s.capitalize!; p s`, "\"Hello\"\n"},
		{"capitalize_bang_nochange", `p "Hello".capitalize!`, "nil\n"},
		{"swapcase_bang", `s = "AbC"; s.swapcase!; p s`, "\"aBc\"\n"},
		{"reverse_bang", `s = "abc"; s.reverse!; p s`, "\"cba\"\n"},
		{"strip_bang", `s = "  hi  "; s.strip!; p s`, "\"hi\"\n"},
		{"strip_bang_nochange", `p "hi".strip!`, "nil\n"},
		{"lstrip_bang", `s = "  x"; s.lstrip!; p s`, "\"x\"\n"},
		{"rstrip_bang", `s = "x  "; s.rstrip!; p s`, "\"x\"\n"},
		{"chomp_bang", "s = \"abc\\n\"; s.chomp!; p s", "\"abc\"\n"},
		{"chomp_bang_nochange", `p "abc".chomp!`, "nil\n"},
		{"chop_bang", `s = "abc"; s.chop!; p s`, "\"ab\"\n"},
		{"squeeze_bang", `s = "aabbcc"; s.squeeze!; p s`, "\"abc\"\n"},
		{"squeeze_bang_nochange", `p "abc".squeeze!`, "nil\n"},
		{"sub_bang", `s = "hello"; s.sub!("l", "L"); p s`, "\"heLlo\"\n"},
		{"sub_bang_nochange", `p "hello".sub!("z", "L")`, "nil\n"},
		{"gsub_bang", `s = "hello"; s.gsub!("l", "L"); p s`, "\"heLLo\"\n"},
		{"gsub_bang_nochange", `p "hello".gsub!("z", "q")`, "nil\n"},
		{"index_assign", `s = "hello"; s[0] = "H"; p s`, "\"Hello\"\n"},
		{"index_assign_neg", `s = "hello"; s[-1] = "O"; p s`, "\"hellO\"\n"},
		{"index_assign_returns_rhs", `s = "hello"; p(s[0] = "H")`, "\"H\"\n"},
		{"startlen_assign", `s = "hello"; s[1, 3] = "X"; p s`, "\"hXo\"\n"},
		{"startlen_assign_grow", `s = "hello"; s[0, 2] = "ABCD"; p s`, "\"ABCDllo\"\n"},
		{"range_assign", `s = "hello"; s[1..2] = "EL"; p s`, "\"hELlo\"\n"},
		{"range_assign_excl", `s = "hello"; s[1...3] = "Y"; p s`, "\"hYlo\"\n"},
		{"startlen_assign_clamp", `s = "hello"; s[1, 99] = "X"; p s`, "\"hX\"\n"},
		{"slice_bang_startlen_clamp", `s = "hello"; p s.slice!(3, 99)`, "\"lo\"\n"},
		{"slice_bang_index", `s = "hello"; p s.slice!(0)`, "\"h\"\n"},
		{"slice_bang_mutates", `s = "hello"; s.slice!(0); p s`, "\"ello\"\n"},
		{"slice_bang_startlen", `s = "hello"; p s.slice!(1, 2)`, "\"el\"\n"},
		{"slice_bang_range", `s = "hello"; s.slice!(1..2); p s`, "\"hlo\"\n"},
		{"slice_bang_oob", `p "hello".slice!(9)`, "nil\n"},
		{"slice_bang_oob_startlen", `p "hello".slice!(9, 1)`, "nil\n"},
		{"slice_bang_oob_range", `p "hello".slice!(9..10)`, "nil\n"},
		// Binary (ASCII-8BIT) slice! is byte-oriented and keeps the result binary
		// (MRI): "é" is 2 bytes, so a 5-char "héllo".b is 6 bytes; slice!(0, 6)
		// removes all 6 bytes and the removed span reports 6 bytes, not 5 chars.
		{"slice_bang_binary_bytes", `s = "héllo".b; p s.slice!(0, 6).bytesize`, "6\n"},
		{"slice_bang_binary_enc", `s = "héllo".b; p s.slice!(0, 6).encoding.to_s`, "\"ASCII-8BIT\"\n"},
		{"slice_bang_binary_empties", `s = "héllo".b; s.slice!(0, 6); p s.bytesize`, "0\n"},
		{"slice_bang_binary_byteoffset", `s = "é".b; p s.slice!(1).bytesize`, "1\n"},
		{"slice_bang_binary_partial", `s = "héllo".b; s.slice!(0, 2); p s.bytesize`, "4\n"},
		{"slice_bang_binary_forced", `s = "abc".force_encoding("ASCII-8BIT"); p s.slice!(0, 2)`, "\"ab\"\n"},
		{"slice_bang_binary_range", `s = "héllo".b; p s.slice!(0..1).bytesize`, "2\n"},
		{"slice_bang_binary_oob", `s = "é".b; p s.slice!(9)`, "nil\n"},
		{"freeze_frozen?", `s = "x".freeze; p s.frozen?`, "true\n"},
		// <<= reassigns the (mutated) same object.
		{"shovel_assign", `s = "a"; s <<= "b"; p s`, "\"ab\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringMutationErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"frozen_shovel", `"x".freeze << "y"`, "FrozenError"},
		{"frozen_upcase", `s = "x"; s.freeze; s.upcase!`, "FrozenError"},
		{"frozen_replace", `s = "x"; s.freeze; s.replace("y")`, "FrozenError"},
		{"frozen_index_assign", `s = "x"; s.freeze; s[0] = "Y"`, "FrozenError"},
		{"frozen_slice_bang", `s = "x"; s.freeze; s.slice!(0)`, "FrozenError"},
		{"append_bad_type", `"x" << []`, "no implicit conversion"},
		{"insert_oob", `"abc".insert(9, "x")`, "out of string"},
		{"index_assign_oob", `s = "abc"; s[9] = "x"`, "out of string"},
		{"index_assign_startlen_oob", `s = "abc"; s[9, 1] = "x"`, "out of string"},
		{"index_assign_neg_len", `s = "abc"; s[0, -1] = "x"`, "negative length"},
		{"index_assign_range_oob", `s = "abc"; s[9..10] = "x"`, "out of range"},
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
