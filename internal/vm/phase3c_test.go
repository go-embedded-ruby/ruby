package vm_test

import (
	"strings"
	"testing"
)

func TestStringInterpolation(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"local", "x = 5\nputs \"x is #{x}\"", "x is 5\n"},
		{"expr", `puts "sum: #{1 + 2}"`, "sum: 3\n"},
		{"prefix_suffix", "n = \"Bob\"\nputs \"hi #{n}!\"", "hi Bob!\n"},
		{"only_interp", "x = 5\nputs \"#{x}\"", "5\n"},
		{"multiple", `puts "a#{1}b#{2}c"`, "a1b2c\n"},
		{"plain_string", `puts "no interp"`, "no interp\n"},
		{"nil_to_s", "y = nil\nputs \"v=#{y}.\"", "v=.\n"},
		{"array_to_s", `puts "arr #{[1, 2, 3]}"`, "arr [1, 2, 3]\n"},
		{"method_call", "a = [1, 2]\nputs \"len #{a.length}\"", "len 2\n"},
		{"hash_brace", `puts "h #{ {a: 1, b: 2}.size }"`, "h 2\n"},
		{"block_brace", "x = 0\n[1, 2, 3].each { |n| x += n }\nputs \"total #{x}\"", "total 6\n"},
		{"nested", "x = 5\nputs \"out #{\"in #{x}\"}\"", "out in 5\n"},
		{"escaped", `puts "lit \#{x}"`, "lit #{x}\n"},
		{"value", "x = 2\ns = \"n#{x}\"\np s", "\"n2\"\n"},
		{"empty_parts", "x = 1\np \"#{x}\"", "\"1\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// An interpolation whose embedded code isn't a single expression is rejected as
// a parse error (the diagnostic wording is the parser's; we only require that the
// malformed interpolation fails to parse).
func TestStringInterpolationMalformed(t *testing.T) {
	if err := runErr(t, `"#{1 2}"`); err == nil || !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("got %v, want a parse error for malformed interpolation", err)
	}
}
