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

// enumSized is an #each-only Enumerable that also answers #size, used to prove
// the origin-size enumerators report the receiver size while the same method on
// a size-less Enumerable reports nil.
const enumSized = "class ES\n include Enumerable\n def initialize(*a) @a=a end\n def each; @a.each { |x| yield x } end\n def size; @a.size end\nend\n"

// TestEnumerableAliasesAndSizes covers the genuine aliases (so their
// UnboundMethods are identical, as in MRI) and the enumerator #size functions:
// origin-size methods report the receiver size when it has one and nil
// otherwise, unknown-size methods always report nil.
func TestEnumerableAliasesAndSizes(t *testing.T) {
	cases := []struct{ src, want string }{
		// Genuine aliases: the UnboundMethod is the same object as its target.
		{`p Enumerable.instance_method(:collect) == Enumerable.instance_method(:map)`, "true\n"},
		{`p Enumerable.instance_method(:filter) == Enumerable.instance_method(:select)`, "true\n"},
		{`p Enumerable.instance_method(:detect) == Enumerable.instance_method(:find)`, "true\n"},
		{`p Enumerable.instance_method(:find_all) == Enumerable.instance_method(:select)`, "true\n"},
		{`p Enumerable.instance_method(:collect_concat) == Enumerable.instance_method(:flat_map)`, "true\n"},
		{`p Enumerable.instance_method(:inject) == Enumerable.instance_method(:reduce)`, "true\n"},
		{`p Enumerable.instance_method(:entries) == Enumerable.instance_method(:to_a)`, "true\n"},
		{`p Enumerable.instance_method(:member?) == Enumerable.instance_method(:include?)`, "true\n"},

		// Origin-size enumerators: receiver size when it has one, nil otherwise.
		{enumSized + `p ES.new(1, 2, 3).map.size`, "3\n"},
		{enumEach + `p E.new(1, 2, 3).map.size.inspect`, "\"nil\"\n"},
		{enumSized + `p ES.new(1, 2, 3, 4).flat_map.size`, "4\n"},
		{enumEach + `p E.new(1, 2, 3, 4).flat_map.size.inspect`, "\"nil\"\n"},
		{enumSized + `p ES.new(1, 2, 3).select.size`, "3\n"},
		{enumSized + `p ES.new(1, 2).min_by.size`, "2\n"},
		{enumSized + `p ES.new(1, 2).partition.size`, "2\n"},
		{enumSized + `p ES.new(1, 2).each_with_index.size`, "2\n"},
		// Unknown-size enumerators: always nil, even when the receiver has #size.
		{enumSized + `p ES.new(1, 2, 3).find.size.inspect`, "\"nil\"\n"},
		{enumSized + `p ES.new(1, 2, 3).find_index.size.inspect`, "\"nil\"\n"},
		{enumSized + `p ES.new(1, 2, 3).take_while.size.inspect`, "\"nil\"\n"},
		{enumSized + `p ES.new(1, 2, 3).drop_while.size.inspect`, "\"nil\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestEnumerableMinMaxReduceFind covers min/max/minmax comparator behaviour,
// reduce/inject operator forms, find's ifnone, and the arity/argument-forwarding
// completions, all against MRI Ruby 4.0 semantics on a custom Enumerable.
func TestEnumerableMinMaxReduceFind(t *testing.T) {
	cases := []struct{ src, want string }{
		// min/max/minmax compare with the block, else <=>.
		{enumEach + `p E.new("2", "33", "4").max { |a, b| b <=> a }`, "\"2\"\n"},
		{enumEach + `p E.new(3, 1, 2).min`, "1\n"},
		{enumEach + `p E.new(3, 1, 2).minmax`, "[1, 3]\n"},
		{enumEach + `p E.new(3, 1, 2).min(2)`, "[1, 2]\n"},
		{enumEach + `p E.new(3, 1, 2).max(2)`, "[3, 2]\n"},
		// nil comparison (incomparable elements, or a nil-returning block) raises.
		{enumEach + `begin; E.new(1, "a").max; rescue => e; p e.class; end`, "ArgumentError\n"},
		{enumEach + `begin; E.new(1, 2, 3).min { |a, b| nil }; rescue => e; p e.class; end`, "ArgumentError\n"},
		// multi-value yields are gathered and compared as arrays.
		{`class YM; include Enumerable; def each; yield 1,2; yield 6,7,8; yield 3,4; end; end
p YM.new.max`, "[6, 7, 8]\n"},

		// reduce/inject operator: Symbol, String, and #to_str all work; a lone
		// argument is init with a block and the operator without one.
		{enumEach + `p E.new(1, 2, 3, 4).inject(:+)`, "10\n"},
		{enumEach + `p E.new(1, 2, 3, 4).inject("+")`, "10\n"},
		{enumEach + `p E.new(1, 2, 3, 4).inject(10, :+)`, "20\n"},
		{enumEach + `p E.new(1, 2, 3, 4).inject(10) { |a, b| a + b }`, "20\n"},
		{enumEach + `s = Object.new; def s.to_str; "+"; end; p E.new(1, 2, 3).inject(s)`, "6\n"},

		// find/detect ifnone: called (once) when nothing matches, else nil.
		{enumEach + `p E.new(1, 2, 3).find(-> { :none }) { |x| x > 5 }`, ":none\n"},
		{enumEach + `p E.new(1, 2, 3).find { |x| x > 5 }.inspect`, "\"nil\"\n"},
		{enumEach + `p E.new(1, 2, 3).detect(-> { 42 }) { |x| x == 2 }`, "2\n"},

		// map/find_index/take_while forward the raw yield so block arity governs.
		{`class YM2; include Enumerable; def each; yield 1,2; yield 3,4; end; end
p YM2.new.map { |x| x }`, "[1, 3]\n"},
		{`class YM3; include Enumerable; def each; yield 1,2; yield 3,4; end; end
p YM3.new.map { |x, y| y }`, "[2, 4]\n"},

		// argument forwarding to #each.
		{`class EA; include Enumerable; attr_reader :got; def each(*a); @got = a; yield 1; yield 2; end; end
e = EA.new; e.to_a(:x, :y); p e.got`, "[:x, :y]\n"},

		// to_h coerces each pair with #to_ary.
		{`class Pairish; def to_ary; [:k, :v]; end; end
class EP; include Enumerable; def each; yield Pairish.new; end; end
p EP.new.to_h`, "{k: :v}\n"},

		// flat_map concatenates a #to_ary result, appends when to_ary is nil.
		{`class TA; def to_ary; [:a, :b]; end; end
p [1, TA.new, 2].flat_map { |x| x }`, "[1, :a, :b, 2]\n"},

		// lazy on a bare Enumerable, driven through #each.
		{enumEach + `p E.new(1, 2, 3, 4).lazy.map { |x| x * 2 }.first(2)`, "[2, 4]\n"},
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

		// reduce/inject: a lone non-symbol/string argument with no block, an
		// operator that is neither symbol nor string, no args without a block, and
		// too many arguments.
		{enumEach + `begin; E.new(1, 2).inject(5); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: 5 is not a symbol nor a string\n"},
		{enumEach + `begin; E.new(1, 2).inject(Object.new); rescue => e; p e.class; end`, "TypeError\n"},
		{enumEach + `begin; E.new(1, 2).inject; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: wrong number of arguments (given 0, expected 1..2)\n"},
		{enumEach + `begin; E.new(1, 2).inject(1, 2, 3); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: wrong number of arguments (given 3, expected 1..2)\n"},

		// find's ifnone must respond to #call.
		{enumEach + `begin; E.new(1, 2).find(42) { |x| x > 5 }; rescue => e; p e.class; end`,
			"NoMethodError\n"},

		// cycle coerces n with #to_int; an uncoercible n is a TypeError.
		{enumEach + `begin; E.new(1).cycle("cat") { }; rescue => e; p e.class; end`, "TypeError\n"},

		// zip requires each argument to respond to #to_ary or #each.
		{enumEach + `begin; E.new(1, 2).zip(Object.new); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: wrong argument type Object (must respond to :each)\n"},
		{enumEach + `begin; E.new(1, 2).zip(42); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: wrong argument type Integer (must respond to :each)\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
