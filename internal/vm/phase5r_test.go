package vm_test

import "testing"

func TestEnumerableEachWithObjectFilterMap(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"filter_map_array", `p [1, 2, 3, 4].filter_map { |x| x * 2 if x.even? }`, "[4, 8]\n"},
		{"filter_map_range", `p (1..5).filter_map { |x| x if x.odd? }`, "[1, 3, 5]\n"},
		{"filter_map_hash", "p({a: 1, b: 2, c: 3}.filter_map { |k, v| k if v > 1 })", "[:b, :c]\n"},
		{"filter_map_all", `p [1, 2, 3].filter_map { |x| x }`, "[1, 2, 3]\n"},
		{"each_with_object_array", `p (1..5).each_with_object([]) { |x, a| a << x * x }`, "[1, 4, 9, 16, 25]\n"},
		{"each_with_object_hash", `p [1, 2, 3].each_with_object({}) { |x, h| h[x] = x * x }`, "{1 => 1, 2 => 4, 3 => 9}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
