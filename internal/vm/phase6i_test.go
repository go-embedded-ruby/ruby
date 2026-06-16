package vm_test

import (
	"strings"
	"testing"
)

func TestBignum(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"pow", `p 2 ** 100`, "1267650600228229401496703205376\n"},
		{"add_overflow", `p 9223372036854775807 + 1`, "9223372036854775808\n"},
		{"sub_overflow", `p((-(2 ** 63)) - 1)`, "-9223372036854775809\n"},
		{"mul_overflow", `p 1000000000000 * 1000000000000`, "1000000000000000000000000\n"},
		{"class_is_integer", `p (2 ** 100).class`, "Integer\n"},
		{"demote_on_shrink", `p (2 ** 64) - (2 ** 64)`, "0\n"},
		{"demote_fits", `p((-2) ** 63)`, "-9223372036854775808\n"},
		{"big_add", `p (2 ** 100) + (2 ** 100)`, "2535301200456458802993406410752\n"},
		{"big_div", `p (10 ** 30) / (10 ** 10)`, "100000000000000000000\n"},
		{"big_mod", `p (10 ** 30) % 7`, "1\n"},
		{"neg_div_floor", `p((-(10 ** 20)) / 3)`, "-33333333333333333334\n"},
		{"big_lt", `p((2 ** 100) < (2 ** 101))`, "true\n"},
		{"big_gt", `p (2 ** 100) > (2 ** 99)`, "true\n"},
		{"big_le", `p((2 ** 100) <= (2 ** 100))`, "true\n"},
		{"big_ge", `p((2 ** 101) >= (2 ** 100))`, "true\n"},
		{"big_eq", `p (2 ** 100) == (2 ** 100)`, "true\n"},
		{"big_neq", `p (2 ** 100) == (2 ** 99)`, "false\n"},
		{"big_ne_int", `p (2 ** 100) == 5`, "false\n"},
		{"neg_pow", `p 2 ** -2`, "0.25\n"},
		{"sort_bignums", `p [2**100, 5, 2**99, 100].sort`, "[5, 100, 633825300114114700748351602688, 1267650600228229401496703205376]\n"},
		{"to_s", `p (2 ** 100).to_s`, "\"1267650600228229401496703205376\"\n"},
		{"to_s_base", `p (255 ** 5).to_s(16)`, "\"fb09f604ff\"\n"},
		{"interpolation", `p "#{2 ** 100}!"`, "\"1267650600228229401496703205376!\"\n"},
		{"puts", `puts (2 ** 100)`, "1267650600228229401496703205376\n"},
		{"truthy", `if (2 ** 100) then p "t" end`, "\"t\"\n"},
		{"abs", `p (-(2 ** 100)).abs`, "1267650600228229401496703205376\n"},
		{"abs_min_int", `p (-(2 ** 63)).abs`, "9223372036854775808\n"},
		{"even", `p (2 ** 100).even?`, "true\n"},
		{"odd", `p (2 ** 100 + 1).odd?`, "true\n"},
		{"zero_false", `p (2 ** 100).zero?`, "false\n"},
		{"positive", `p (2 ** 100).positive?`, "true\n"},
		{"negative", `p (-(2 ** 100)).negative?`, "true\n"},
		{"succ", `p (2 ** 64).succ - 2 ** 64`, "1\n"},
		{"pred", `p (2 ** 64) - (2 ** 64).pred`, "1\n"},
		{"big_times_neg", `p (2 ** 70) * -1`, "-1180591620717411303424\n"},
		{"negate", `p(-(2 ** 100))`, "-1267650600228229401496703205376\n"},
		{"negate_min_int", `p(-(-(2 ** 63)))`, "9223372036854775808\n"},
		{"to_f", `p (2 ** 70).to_f`, "1.1805916207174113e+21\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestBignumErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"out_of_range_method", `(2 ** 100).gcd(2)`, "RangeError"},
		{"div_zero", `(2 ** 100) / 0`, "ZeroDivisionError"},
		{"mod_zero", `(2 ** 100) % 0`, "ZeroDivisionError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %q", tc.src, err, tc.want)
			}
		})
	}
}
