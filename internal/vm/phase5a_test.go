package vm_test

import (
	"strings"
	"testing"
)

func TestFormatString(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"decimal", `p("%d apples" % 5)`, "\"5 apples\"\n"},
		{"float_pad", `p("%05.2f" % 3.14159)`, "\"03.14\"\n"},
		{"two_strings", `p("%s and %s" % ["a", "b"])`, "\"a and b\"\n"},
		{"radixes", `p("%x %X %o %b" % [255, 255, 8, 5])`, "\"ff FF 10 101\"\n"},
		{"flags", `p("%+d % d %-5d|" % [3, 4, 7])`, "\"+3  4 7    |\"\n"},
		{"format_fn", `p(format("%3d", 5))`, "\"  5\"\n"},
		{"sprintf_fn", `p(sprintf("%.3f", 1.0 / 3))`, "\"0.333\"\n"},
		{"chars", `p("%c%c" % [65, 66])`, "\"AB\"\n"},
		{"alt_form", `p("%#x %#o" % [255, 8])`, "\"0xff 010\"\n"},
		{"sci", `p("%e" % 12345.678)`, "\"1.234568e+04\"\n"},
		{"general", `p("%g" % 0.0001)`, "\"0.0001\"\n"},
		{"float_to_int", `p("%d" % 3.9)`, "\"3\"\n"},
		{"i_verb", `p("%i" % 42)`, "\"42\"\n"},
		{"literal_pct", `p("%d%%" % 7)`, "\"7%\"\n"},
		{"char_from_string", `p("%c" % "XYZ")`, "\"X\"\n"},
		{"neg_zeropad", `p("%08.3f" % -2.5)`, "\"-002.500\"\n"},
		{"float_from_int", `p("%f" % 5)`, "\"5.000000\"\n"},
		{"char_empty", `p("%c" % "")`, "\"\"\n"},
		{"char_width", `p("%5c|" % "A")`, "\"    A|\"\n"},
		// Numeric String coercion (MRI: "%02d" % "7" => "07").
		{"int_from_string", `p("%02d" % "7")`, "\"07\"\n"},
		{"int_from_string_ws", `p("%d" % "  12  ")`, "\"12\"\n"},
		{"int_from_string_uscore", `p("%d" % "1_000")`, "\"1000\"\n"},
		{"int_from_string_hex", `p("%d" % "0x1A")`, "\"26\"\n"},
		{"float_from_string", `p("%.1f" % "1.5")`, "\"1.5\"\n"},
		// Bignum (and a numeric String too large for int64) format at full width.
		{"int_bignum", `p("%d" % (10 ** 30))`, "\"1000000000000000000000000000000\"\n"},
		{"hex_bignum", `p("%x" % (10 ** 30))`, "\"c9f2c9cd04674edea40000000\"\n"},
		{"int_bignum_string", `p("%d" % ("1" + "0" * 30))`, "\"1000000000000000000000000000000\"\n"},
		{"float_bignum", `p("%.0f" % (10 ** 20))`, "\"100000000000000000000\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestFormatErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"int_symbol", `"%d" % :s`, "Symbol into Integer"},
		{"float_symbol", `"%f" % :s`, "Symbol into Float"},
		{"char_float", `"%c" % [1.5]`, "Float into Integer"},
		{"int_nil", `"%d" % [nil]`, "nil into Integer"},
		{"int_array", `"%d" % [[1]]`, "Array into Integer"},
		{"int_hash", `"%d" % [{}]`, "Hash into Integer"},
		{"int_string", `"%d" % ["x"]`, `invalid value for Integer(): "x"`},
		{"float_string", `"%f" % ["x"]`, `invalid value for Float(): "x"`},
		{"int_object", "class Foo\nend\n\"%d\" % [Foo.new]", "Object into Integer"},
		{"format_nonstring", `format(5)`, "Integer into String"},
		{"too_few", `"%d %d" % [1]`, "too few arguments"},
		{"unknown_verb", `"%q" % 5`, "malformed format"},
		{"trailing_pct", `"abc%" % []`, "malformed format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %q", tc.src, err, tc.want)
			}
		})
	}
}
