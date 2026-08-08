package vm_test

import "testing"

// enumEach is a fresh #each-only Enumerable used to prove the residual prelude
// methods work over ANY Enumerable, not just Array/Range.
const enumEach = "class E\n include Enumerable\n def initialize(*a) @a=a end\n def each; @a.each { |x| yield x } end\nend\n"

// enumMulti yields a mix of scalar, multi-value, and zero-argument #each yields,
// exercising rb_enum_values_pack packing (scalar stays scalar, multi gathers into
// an Array, empty becomes nil).
const enumMulti = "class M\n include Enumerable\n def each; yield 1; yield 2, 3; yield; yield 4; end\nend\n"

// TestEnumerableResiduals covers the residual Enumerable methods added/completed
// in the prelude to MRI 4.0 semantics: #each_entry, #collect_concat, #tally with
// an accumulator Hash, #zip with a block, #find_index without arguments, and
// #minmax_by without a block. Every case is asserted against MRI Ruby 4.0.5 and
// exercises a custom #each-defining class so genericity is proven.
func TestEnumerableResiduals(t *testing.T) {
	cases := []struct{ src, want string }{
		// each_entry gathers each #each yield exactly as rb_enum_values_pack does:
		// scalar stays scalar, a multi-value yield becomes an Array, an empty yield
		// becomes nil. A one-arity block receives the whole packed value.
		{enumMulti + `r = []; M.new.each_entry { |e| r << e }; p r`, "[1, [2, 3], nil, 4]\n"},
		{enumMulti + `p M.new.each_entry.to_a`, "[1, [2, 3], nil, 4]\n"},
		// no block -> Enumerator; with a block -> returns self.
		{enumMulti + `p M.new.each_entry.class`, "Enumerator\n"},
		{enumEach + `e = E.new(1, 2); p e.each_entry { |x| }.equal?(e)`, "true\n"},
		// the returned Enumerator is sized when the receiver answers #size.
		{`p [1, 2, 3].each_entry.size`, "3\n"},

		// collect_concat is flat_map: array results flatten one level, non-arrays
		// pass through; no block -> Enumerator.
		{enumEach + `p E.new(1, [2, 3], 4).collect_concat { |x| x }`, "[1, 2, 3, 4]\n"},
		{enumEach + `p E.new(1, 2).collect_concat { |x| [x, -x] }`, "[1, -1, 2, -2]\n"},
		{enumEach + `p E.new(1, 2).collect_concat.class`, "Enumerator\n"},

		// tally with no argument builds a fresh count Hash.
		{enumEach + `p E.new("a", "b", "a").tally`, `{"a" => 2, "b" => 1}` + "\n"},
		// tally(hash) accumulates into and returns the SAME hash (missing key -> 0).
		{enumEach + `h = {"a" => 2}; r = E.new("a", "b", "a").tally(h); p r; p h.equal?(r)`,
			`{"a" => 4, "b" => 1}` + "\ntrue\n"},

		// zip with a block yields each row and returns nil.
		{enumEach + `o = []; E.new(1, 2, 3).zip([4, 5, 6]) { |row| o << row }; p o`,
			"[[1, 4], [2, 5], [3, 6]]\n"},
		{enumEach + `p(E.new(1, 2, 3).zip([4, 5, 6]) { |r| })`, "nil\n"},
		// a non-Array other (a Range) is taken via #to_a; a short other pads nil.
		{enumEach + `p E.new(1, 2).zip(3..4)`, "[[1, 3], [2, 4]]\n"},
		{enumEach + `p E.new(1, 2, 3).zip([9])`, "[[1, 9], [2, nil], [3, nil]]\n"},

		// find_index with neither value nor block returns an Enumerator.
		{enumEach + `p E.new(1, 2, 3).find_index.class`, "Enumerator\n"},
		{enumEach + `p E.new(10, 20, 30).find_index(20)`, "1\n"},
		{enumEach + `p E.new(10, 20, 30).find_index { |x| x > 15 }`, "1\n"},

		// minmax_by compares by the mapped value; empty -> [nil, nil]; no block ->
		// Enumerator.
		{enumEach + `p E.new(3, 1, 2).minmax_by { |x| -x }`, "[3, 1]\n"},
		{enumEach + `p E.new.minmax_by { |x| x }`, "[nil, nil]\n"},
		{enumEach + `p E.new(1, 2).minmax_by.class`, "Enumerator\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestEnumerableResidualErrors covers the error branches of the residual methods
// with the exact MRI Ruby 4.0.5 exception class and message: tally rejects a
// non-Hash argument, a non-Integer accumulated count, and surplus arguments;
// find_index rejects a second argument.
func TestEnumerableResidualErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{enumEach + `begin; E.new(1).tally("x"); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: no implicit conversion of String into Hash\n"},
		{enumEach + `begin; E.new(1).tally(nil); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: no implicit conversion of nil into Hash\n"},
		{enumEach + `begin; E.new("a").tally({"a" => "z"}); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: wrong argument type String (expected Integer)\n"},
		{enumEach + `begin; E.new("a").tally({"a" => nil}); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: wrong argument type nil (expected Integer)\n"},
		{enumEach + `begin; E.new("a").tally({"a" => true}); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: wrong argument type true (expected Integer)\n"},
		{enumEach + `begin; E.new(1).tally(1, 2); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: wrong number of arguments (given 2, expected 0..1)\n"},
		{enumEach + `begin; E.new(1).find_index(1, 2); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: wrong number of arguments (given 2, expected 0..1)\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
