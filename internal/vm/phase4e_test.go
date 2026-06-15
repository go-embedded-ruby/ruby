package vm_test

import "testing"

func TestEnumerableExtras(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"flat_map", `p [1, 2, 3].flat_map { |x| [x, x * 10] }`, "[1, 10, 2, 20, 3, 30]\n"},
		{"flat_map_scalar", `p [1, 2, 3].flat_map { |x| x }`, "[1, 2, 3]\n"},
		{"partition", `p [1, 2, 3, 4].partition { |x| x.even? }`, "[[2, 4], [1, 3]]\n"},
		{"group_by", `p [1, 2, 3, 4, 5].group_by { |x| x % 2 }`, "{1 => [1, 3, 5], 0 => [2, 4]}\n"},
		{"tally", `p ["a", "b", "a", "c", "a"].tally`, "{\"a\" => 3, \"b\" => 1, \"c\" => 1}\n"},
		{"tally_ints", `p [1, 1, 2, 3, 3, 3].tally`, "{1 => 2, 2 => 1, 3 => 3}\n"},
		{"zip", `p [1, 2, 3].zip([4, 5, 6])`, "[[1, 4], [2, 5], [3, 6]]\n"},
		{"range_flat_map", `p((1..3).flat_map { |x| [x, -x] })`, "[1, -1, 2, -2, 3, -3]\n"},
		{"range_partition", `p((1..4).partition { |x| x > 2 })`, "[[3, 4], [1, 2]]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
