package vm_test

import "testing"

// Array and Range inherit the Enumerable surface (built on their native `each`)
// while their own native methods still take precedence.
func TestEnumerableMixin(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Array
		{"arr_select", `p [1, 2, 3, 4].select { |x| x.even? }`, "[2, 4]\n"},
		{"arr_reject", `p [1, 2, 3, 4].reject { |x| x.even? }`, "[1, 3]\n"},
		{"arr_reduce", `p [1, 2, 3].reduce(0) { |a, x| a + x }`, "6\n"},
		{"arr_sum", `p [1, 2, 3].sum`, "6\n"},
		{"arr_min", `p [3, 1, 2].min`, "1\n"},
		{"arr_max", `p [3, 1, 2].max`, "3\n"},
		{"arr_find", `p [1, 2, 3].find { |x| x > 1 }`, "2\n"},
		{"arr_any", `p [1, 2, 3].any? { |x| x > 2 }`, "true\n"},
		{"arr_all", `p [1, 2, 3].all? { |x| x > 0 }`, "true\n"},
		{"arr_none", `p [1, 2, 3].none? { |x| x > 9 }`, "true\n"},
		{"arr_count", `p [1, 2, 3].count`, "3\n"},
		{"arr_each_with_index", "r = []\n[7, 8].each_with_index { |x, i| r << [i, x] }\np r", "[[0, 7], [1, 8]]\n"},
		// native methods still win over the mixin
		{"arr_native_map", `p [1, 2, 3].map { |x| x * 2 }`, "[2, 4, 6]\n"},
		{"arr_native_include", `p [1, 2, 3].include?(2)`, "true\n"},
		// Range
		{"rng_select", `p((1..5).select { |x| x.odd? })`, "[1, 3, 5]\n"},
		{"rng_reduce", `p((1..5).reduce(0) { |a, x| a + x })`, "15\n"},
		{"rng_find", `p((1..5).find { |x| x > 3 })`, "4\n"},
		{"rng_any", `p((1..3).any? { |x| x > 2 })`, "true\n"},
		{"rng_reject", `p((1..5).reject { |x| x.odd? })`, "[2, 4]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
