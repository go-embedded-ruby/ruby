package vm_test

import (
	"strings"
	"testing"
)

func TestStruct(t *testing.T) {
	pre := "P = Struct.new(:x, :y)\n"
	tests := []struct{ name, src, want string }{
		{"accessors", pre + "p1 = P.new(1, 2)\np p1.x\np p1.y", "1\n2\n"},
		{"setter", pre + "p1 = P.new(1, 2)\np1.x = 9\np p1.x", "9\n"},
		{"to_a", pre + "p P.new(1, 2).to_a", "[1, 2]\n"},
		{"to_h", pre + "p P.new(1, 2).to_h", "{x: 1, y: 2}\n"},
		{"members", pre + "p P.new(1, 2).members", "[:x, :y]\n"},
		{"size", pre + "p P.new(1, 2).size", "2\n"},
		{"length", pre + "p P.new(1, 2).length", "2\n"},
		{"index_int", pre + "p P.new(1, 2)[0]", "1\n"},
		{"index_neg", pre + "p P.new(1, 2)[-1]", "2\n"},
		{"index_sym", pre + "p P.new(1, 2)[:y]", "2\n"},
		{"index_str", pre + "p P.new(1, 2)[\"x\"]", "1\n"},
		{"eq_true", pre + "p(P.new(1, 2) == P.new(1, 2))", "true\n"},
		{"eq_false", pre + "p(P.new(1, 2) == P.new(1, 9))", "false\n"},
		{"eq_diff_struct", pre + "Q = Struct.new(:a)\np(P.new(1, 2) == Q.new(1))", "false\n"},
		{"missing_args", pre + "p P.new(1).to_a", "[1, nil]\n"},
		{"no_args", pre + "p P.new.to_a", "[nil, nil]\n"},
		{"values", pre + "p P.new(3, 4).values", "[3, 4]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStructErrors(t *testing.T) {
	pre := "P = Struct.new(:x, :y)\n"
	cases := []struct{ name, src, want string }{
		{"too_many", pre + "P.new(1, 2, 3)", "struct size differs"},
		{"bad_member", pre + "P.new(1, 2)[:nope]", "no member 'nope'"},
		{"index_oob", pre + "P.new(1, 2)[5]", "too large"},
		{"bad_index_type", pre + "P.new(1, 2)[1.5]", "into Integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %q", tc.src, err, tc.want)
			}
		})
	}
}
