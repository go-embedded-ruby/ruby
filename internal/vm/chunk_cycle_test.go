package vm_test

import "testing"

// TestChunkCycleStartWith covers Enumerable#chunk / #minmax_by / #cycle(n) and
// String#start_with? with multiple args and a Regexp. Asserted against MRI Ruby
// 4.0.5.
func TestChunkCycleStartWith(t *testing.T) {
	cases := []struct{ src, want string }{
		// String#start_with?: Regexp (must match at offset 0) and several prefixes.
		{`p "Hello".start_with?(/H/)`, "true\n"},
		{`p "Hello".start_with?(/x/)`, "false\n"},   // regex no match
		{`p "Hello".start_with?(/ell/)`, "false\n"}, // matches, but not at offset 0
		{`p "Hello".start_with?("He", "X")`, "true\n"},
		{`p "Hello".start_with?("Z", "He")`, "true\n"}, // first prefix misses, second hits
		{`p "Hello".start_with?("Z", "Y")`, "false\n"}, // none match
		// Enumerable#chunk.
		{`p [1, 2, 2, 3, 3, 3].chunk { |x| x }.map { |k, v| [k, v.size] }`, "[[1, 1], [2, 2], [3, 3]]\n"},
		{`p [1, 1, 2, 3, 3].chunk { |x| x.odd? }.to_a`, "[[true, [1, 1]], [false, [2]], [true, [3, 3]]]\n"},
		// Enumerable#minmax_by.
		{`p [1, 2, 3, 4, 5].minmax_by { |x| (x - 3).abs }`, "[3, 1]\n"},
		{`p (1..5).minmax_by { |x| -x }`, "[5, 1]\n"},
		{`p ["a", "bb", "ccc"].minmax_by(&:length)`, "[\"a\", \"ccc\"]\n"},
		// Enumerable#cycle(n): finite repetition (block and Enumerator forms).
		{`p [1, 2, 3].cycle(2).to_a`, "[1, 2, 3, 1, 2, 3]\n"},
		{`r = []; [1, 2, 3].cycle(2) { |x| r << x }; p r`, "[1, 2, 3, 1, 2, 3]\n"},
		{`p (1..3).cycle(2).to_a`, "[1, 2, 3, 1, 2, 3]\n"},
		{`p [].cycle(3).to_a`, "[]\n"}, // empty -> nothing
		// cycle with no count loops forever; break stops it (covers that branch).
		{`r = []; [1, 2, 3].cycle { |x| r << x; break if r.size >= 4 }; p r`, "[1, 2, 3, 1]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
