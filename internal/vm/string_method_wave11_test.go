package vm_test

import (
	"strings"
	"testing"
)

// TestStringWave11 covers the non-encoding String conformance work of wave 11:
// the <=> / == coercion protocol, the built-in aliases, to_f underscores and
// terminals, to_i base handling, getbyte arity, upto codepoint iteration and
// coercion, and start_with? coercion / $~ setting. Each case is byte-exact
// against MRI ruby 4.x.
func TestStringWave11(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// --- built-in aliases share one Method record (== on UnboundMethod) ---
		{"alias_next_succ", `p String.instance_method(:next) == String.instance_method(:succ)`, "true\n"},
		{"alias_next_bang", `p String.instance_method(:next!) == String.instance_method(:succ!)`, "true\n"},
		{"alias_intern", `p String.instance_method(:intern) == String.instance_method(:to_sym)`, "true\n"},
		{"alias_to_str", `p String.instance_method(:to_str) == String.instance_method(:to_s)`, "true\n"},

		// --- String#<=> ---
		{"cmp_plain_eq", `p("yep" <=> "yep")`, "0\n"},
		{"cmp_plain_lt", `p("a" <=> "b")`, "-1\n"},
		{"cmp_subclass", `class S < String; end; p("hello" <=> S.new("hello"))`, "0\n"},
		{"cmp_to_str", `o = Object.new; def o.to_str; "aaa"; end; p("abc" <=> o)`, "1\n"},
		{"cmp_invcmp_int", `o = Object.new; def o.<=>(x); -1; end; p("abc" <=> o)`, "1\n"},
		{"cmp_invcmp_nil", `p("a" <=> 5)`, "nil\n"},
		{"cmp_inverse_guard", `o = Object.new; def o.<=>(x); x <=> self; end; p("abc" <=> o)`, "nil\n"},

		// --- String#== / operator fast path ---
		{"eq_plain_true", `p("a" == "a")`, "true\n"},
		{"eq_plain_false", `p("a" == "b")`, "false\n"},
		{"eq_subclass", `class S2 < String; end; p("hello" == S2.new("hello"))`, "true\n"},
		{"eq_int_false", `p("hello" == 5)`, "false\n"},
		{"eq_sym_false", `p("hello" == :hello)`, "false\n"},
		{"eq_to_str_defer", `o = Object.new; def o.to_str; "x"; end; def o.==(x); true; end; p("hello" == o)`, "true\n"},
		{"eq_to_str_send", `o = Object.new; def o.to_str; "x"; end; def o.==(x); true; end; p("hello".send(:==, o))`, "true\n"},
		{"neq_to_str_defer", `o = Object.new; def o.to_str; "x"; end; def o.==(x); true; end; p("hello" != o)`, "false\n"},

		// --- String#to_f: underscores, sign, exponent rollback, terminals ---
		{"tof_underscore", `p("1_234_567.890_1".to_f == 1_234_567.890_1)`, "true\n"},
		{"tof_sign_exp_underscore", `p "-5_5e-5_0".to_f`, "-5.5e-49\n"},
		{"tof_nul_terminal", `p "\x001.2".to_f`, "0.0\n"},
		{"tof_e_rollback", `p "5e".to_f`, "5.0\n"},
		{"tof_dot_five_e", `p ".5e1".to_f`, "5.0\n"},
		{"tof_bad", `p "bad".to_f`, "0.0\n"},
		{"tof_double_us", `p "1__2".to_f`, "1.0\n"},
		{"tof_frac_us_terminal", `p "1.2e_x3".to_f`, "1.2\n"},
		{"tof_leading_us", `p "_9".to_f`, "0.0\n"},

		// --- String#to_i: base 0 octal, base coercion ---
		{"toi_base0_octal", `p "017".to_i(0)`, "15\n"},
		{"toi_base0_hex", `p "0x1f".to_i(0)`, "31\n"},
		{"toi_base0_decimal", `p "42".to_i(0)`, "42\n"},
		{"toi_base_float", `p "17".to_i(8.0)`, "15\n"},
		{"toi_base_to_int", `class B; def to_int; 8; end; end; p "17".to_i(B.new)`, "15\n"},

		// --- String#getbyte arity ---
		{"getbyte_ok", `p "abc".getbyte(1)`, "98\n"},

		// --- String#upto: codepoint iteration + coercion ---
		{"upto_ascii_span", `p "9".upto("A").to_a`, "[\"9\", \":\", \";\", \"<\", \"=\", \">\", \"?\", \"@\", \"A\"]\n"},
		{"upto_excl", `a = []; "a".upto("d", true) { |s| a << s }; p a`, "[\"a\", \"b\", \"c\"]\n"},
		{"upto_letters", `a = []; "a".upto("c") { |s| a << s }; p a`, "[\"a\", \"b\", \"c\"]\n"},
		{"upto_to_str", `o = Object.new; def o.to_str; "abd"; end; a = []; "abc".upto(o) { |s| a << s }; p a`, "[\"abc\", \"abd\"]\n"},
		{"upto_digits", `a = []; "8".upto("11") { |s| a << s }; p a`, "[\"8\", \"9\", \"10\", \"11\"]\n"},
		{"upto_size_nil", `p "a".upto("b").size`, "nil\n"},

		// --- String#start_with?: coercion + $~ ---
		{"sw_plain", `p "hello".start_with?("he")`, "true\n"},
		{"sw_to_str", `o = Object.new; def o.to_str; "h"; end; p "hello".start_with?(o)`, "true\n"},
		{"sw_regexp_match", `r = "test-1337".start_with?(/test-(\d+)/); p r; p $1`, "true\n\"1337\"\n"},
		{"sw_regexp_nomatch", `r = "test-asdf".start_with?(/test-(\d+)/); p r; p $1`, "false\nnil\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestStringWave11Errors covers the TypeError / ArgumentError paths added in
// wave 11: getbyte arity, upto's non-convertible end, and start_with?'s
// non-convertible prefix.
func TestStringWave11Errors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"getbyte_zero_args", `"glark".getbyte`, "ArgumentError"},
		{"getbyte_two_args", `"food".getbyte(0, 0)`, "ArgumentError"},
		{"upto_int", `"abc".upto(123) { }`, "TypeError"},
		{"upto_symbol", `"a".upto(:c).to_a`, "TypeError"},
		{"start_with_int", `"hello".start_with?(1)`, "TypeError"},
		{"start_with_array", `"hello".start_with?(["h"])`, "TypeError"},
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
