package vm_test

import "testing"

// Block auto-splat: a multi-parameter block called with a single Array
// destructures it element-wise.
func TestBlockAutoSplat(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"each_pairs", "r = []\n[[1, 2], [3, 4]].each { |a, b| r << a + b }\np r", "[3, 7]\n"},
		{"map_pairs", `p [[1, 2], [3, 4]].map { |a, b| a + b }`, "[3, 7]\n"},
		{"single_param_no_splat", `p [[1, 2]].map { |x| x }`, "[[1, 2]]\n"},
		{"three_params", "r = nil\n[[1, 2, 3]].each { |a, b, c| r = a + b + c }\np r", "6\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// Hash is Enumerable: each yields a [k, v] pair; map/find/count/any?/all?/none?
// /to_a come from the module, while select/reject are native (return a Hash).
func TestHashEnumerable(t *testing.T) {
	const h = `h = {:a => 1, :b => 2}` + "\n"
	tests := []struct{ name, src, want string }{
		{"each_two_params", h + "s = 0\nh.each { |k, v| s = s + v }\np s", "3\n"},
		{"each_one_param", `{:a => 1}.each { |pair| p pair }`, "[:a, 1]\n"},
		{"map", h + `p(h.map { |k, v| [k, v] })`, "[[:a, 1], [:b, 2]]\n"},
		{"map_values", h + `p(h.map { |k, v| v * 10 })`, "[10, 20]\n"},
		{"select", h + `p(h.select { |k, v| v > 1 })`, "{b: 2}\n"},
		{"reject", h + `p(h.reject { |k, v| v > 1 })`, "{a: 1}\n"},
		{"find", h + `p(h.find { |k, v| v == 2 })`, "[:b, 2]\n"},
		{"to_a", h + `p(h.to_a)`, "[[:a, 1], [:b, 2]]\n"},
		{"count", h + `p(h.count)`, "2\n"},
		{"any_true", h + `p(h.any? { |k, v| v > 1 })`, "true\n"},
		{"all_true", h + `p(h.all? { |k, v| v > 0 })`, "true\n"},
		{"none_true", h + `p(h.none? { |k, v| v > 9 })`, "true\n"},
		// native Hash methods still work
		{"native_keys", h + `p h.keys`, "[:a, :b]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestHashSelectRejectNoBlock(t *testing.T) {
	// select/reject with no block return an Enumerator (MRI semantics).
	for _, src := range []string{`p({a: 1}.select.to_a)`, `p({a: 1}.reject.to_a)`} {
		if got := eval(t, src); got != "[[:a, 1]]\n" {
			t.Fatalf("src=%q got %q", src, got)
		}
	}
}
