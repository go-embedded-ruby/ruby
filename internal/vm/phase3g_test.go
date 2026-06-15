package vm_test

import (
	"strings"
	"testing"
)

func TestHashMethods(t *testing.T) {
	const h = "h = {a: 1, b: 2}\n"
	tests := []struct{ name, src, want string }{
		{"merge", h + `p h.merge({b: 3, c: 4})`, "{a: 1, b: 3, c: 4}\n"},
		{"merge_nonmutating", h + "h.merge({c: 9})\np h", "{a: 1, b: 2}\n"},
		{"fetch", h + `p h.fetch(:a)`, "1\n"},
		{"fetch_default", h + `p h.fetch(:z, 99)`, "99\n"},
		{"dig_one", h + `p h.dig(:a)`, "1\n"},
		{"dig_nested", "n = {x: {y: {z: 5}}}\np n.dig(:x, :y, :z)", "5\n"},
		{"dig_missing", "n = {x: {y: 1}}\np n.dig(:x, :q)", "nil\n"},
		{"dig_missing_deep", "n = {x: {y: 1}}\np n.dig(:x, :q, :r)", "nil\n"},
		{"dig_array", "n = {a: [10, 20, 30]}\np n.dig(:a, 1)", "20\n"},
		{"dig_past_nil", "n = {a: nil}\np n.dig(:a, :b)", "nil\n"},
		{"dig_array_oob", "n = {a: [1, 2]}\np n.dig(:a, 9)", "nil\n"},
		{"values_at", h + `p h.values_at(:a, :b)`, "[1, 2]\n"},
		{"transform_values", h + `p h.transform_values { |v| v * 10 }`, "{a: 10, b: 20}\n"},
		{"transform_keys", h + `p h.transform_keys { |k| k.to_s }`, "{\"a\" => 1, \"b\" => 2}\n"},
		{"invert", h + `p h.invert`, "{1 => :a, 2 => :b}\n"},
		{"to_h", h + `p h.to_h`, "{a: 1, b: 2}\n"},
		{"has_value_true", h + `p h.has_value?(2)`, "true\n"},
		{"has_value_false", h + `p h.has_value?(9)`, "false\n"},
		{"value_alias", h + `p h.value?(1)`, "true\n"},
		{"store", h + "h.store(:c, 3)\np h", "{a: 1, b: 2, c: 3}\n"},
		{"delete", h + "r = h.delete(:a)\np r\np h", "1\n{b: 2}\n"},
		{"delete_missing", h + `p h.delete(:zzz)`, "nil\n"},
		{"each_pair", "t = 0\n{x: 1, y: 2}.each_pair { |k, v| t += v }\np t", "3\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestHashMethodErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"fetch_missing", `({a: 1}).fetch(:z)`, "KeyError"},
		{"merge_non_hash", `({a: 1}).merge(5)`, "TypeError"},
		{"dig_non_digable", `({a: 1}).dig(:a, :b)`, "TypeError"},
		{"transform_values_no_block", `({a: 1}).transform_values`, "LocalJumpError"},
		{"transform_keys_no_block", `({a: 1}).transform_keys`, "LocalJumpError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %s", tc.src, err, tc.want)
			}
		})
	}
}
