package vm_test

import "testing"

// TestArrayHashValueEquality covers MRI's rb_equal-per-element rule: Array#== and
// Hash#== (and the membership/search methods that use ==) compare their members by
// dispatching each member's own Ruby #==, not by Go identity. A member with a
// redefined #== therefore participates in the comparison. Asserted against MRI
// Ruby 4.0.
func TestArrayHashValueEquality(t *testing.T) {
	// A class whose #== always answers true: every wrapping of it compares equal.
	always := "class C; def ==(o); true; end; end\n"
	// A class whose #== matches only the Integer 42 (used to exercise both operand
	// orders and the reflexive Integer#== path).
	only42 := "class D; def ==(o); o == 42; end; end\n"
	// A class whose #== matches any Integer (used for membership/subset).
	anyInt := "class E; def ==(o); o.is_a?(Integer); end; end\n"

	cases := []struct{ src, want string }{
		// Array#== dispatches each element's #== (value equality), so distinct
		// instances with a matching #== are equal.
		{always + "a = C.new; p [[a] == [a], [a] == [C.new], [a] == [1]]", "[true, true, true]\n"},
		// A length mismatch short-circuits before any element #==.
		{always + "a = C.new; p([a] == [a, a])", "false\n"},
		// Nested arrays recurse, still dispatching the leaf #==.
		{always + "a = C.new; p([[a], [a]] == [[C.new], [C.new]])", "true\n"},

		// Hash#== compares values with #==; keys must match (by eql?/hash).
		{always + "a = C.new; p [{x: a} == {x: C.new}, {x: a} == {y: a}, {x: a} == {x: a, z: a}]", "[true, false, false]\n"},

		// Membership and search use == with the element as the receiver.
		{always + "a = C.new; p [[a].include?(C.new), [a].index(C.new), [a].rindex(C.new), [a, a].count(C.new)]", "[true, 0, 0, 2]\n"},
		{always + "a = C.new; p({k: a}.assoc(:k) == [:k, a])", "true\n"},
		{always + "a = C.new; p [{1 => a}.key(C.new) == 1, {x: a}.value?(C.new), {x: a}.has_value?(C.new)]", "[true, true, true]\n"},

		// The reflexive Integer#== rule: 42 == D.new dispatches D.new == 42, so an
		// element order of [42] == [D.new] still holds — in both operand orders.
		{only42 + "p [[42] == [D.new], [D.new] == [42], [41] == [D.new]]", "[true, true, false]\n"},

		// Hash#<= (subset) compares the shared values with #==.
		{anyInt + "p [({a: E.new} <= {a: 7}), ({a: E.new} < {a: 7, b: 8})]", "[true, true]\n"},
		// Membership through a custom #==.
		{anyInt + "p [[E.new].include?(9), {x: E.new}.value?(9)]", "[true, true]\n"},

		// A self-referential array compares equal to itself without looping
		// (MRI's recursive-comparison rule), and two mutually recursive arrays too.
		{"a = []; a << a; p(a == a)", "true\n"},
		{"a = []; a << a; b = []; b << b; p(a == b)", "true\n"},

		// Plain value elements keep their ordinary numeric ==: 1 == 1.0 in an array.
		{"p [[1] == [1.0], [1, 2] == [1, 2], [1] == [2]]", "[true, true, false]\n"},

		// A built-in with a custom #== as an element (Set) is compared by value.
		{"require 'set'; p [[Set[1, 2]] == [Set[2, 1]], [Set[1]] == [Set[2]]]", "[true, false]\n"},

		// An Array subclass is still an Array: == compares structurally, in both
		// operand orders and through an explicit method send.
		{"class ML < Array; end; p [ML.new([1, 2]) == [1, 2], [1, 2] == ML.new([1, 2]), ML.new([1, 2]).send(:==, [1, 2])]", "[true, true, true]\n"},

		// [NaN] == [NaN] is true because Array#== checks #equal? per element first,
		// while a bare NaN == NaN stays false.
		{"p [[Float::NAN] == [Float::NAN], Float::NAN == Float::NAN]", "[true, false]\n"},

		// A built-in-with-custom-== member on the RIGHT still drives element equality
		// (the reflexive fall-through): 1 == Set[1] is false, so the arrays differ.
		{"require 'set'; p([1] == [Set[1]])", "false\n"},

		// Array#== vs a non-Array: a plain value, a user object without #to_ary, and
		// one with #to_ary (which dispatches other == self).
		{"p([1] == 1)", "false\n"},
		{"p([1] == Object.new)", "false\n"},
		{"class AL; def respond_to?(m, *); m == :to_ary; end; def ==(o); o == [1]; end; end; p([1] == AL.new)", "true\n"},

		// Hash#== treats a Hash subclass structurally (both orders), and a non-Hash
		// answering #to_hash dispatches other == self; a plain non-Hash is not equal.
		{"class MH < Hash; end; h = MH.new; h[:a] = 1; p [h == {a: 1}, {a: 1} == h]", "[true, true]\n"},
		{"p({a: 1} == 5)", "false\n"},
		{"class HL; def respond_to?(m, *); m == :to_hash; end; def ==(o); true; end; end; p({a: 1} == HL.new)", "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestArrayHashSetOpsAndEql covers the eql?/hash-based operations (uniq, &, |, -,
// difference, union, intersection) dispatching a member's own #eql?/#hash, and
// Hash#eql? / Array#eql? dispatching member #eql?. Asserted against MRI Ruby 4.0.
func TestArrayHashSetOpsAndEql(t *testing.T) {
	// A class with value-based #eql?/#hash keyed on @n.
	k := "class K; attr_reader :n; def initialize(n); @n = n; end; " +
		"def eql?(o); o.is_a?(K) && o.n == @n; end; def hash; @n; end; end\n"

	cases := []struct{ src, want string }{
		// uniq groups by #hash then #eql?.
		{k + "p [K.new(1), K.new(1), K.new(2)].uniq.size", "2\n"},
		// & / | / - dispatch #eql?/#hash.
		{k + "p ([K.new(1), K.new(2)] & [K.new(2)]).size", "1\n"},
		{k + "p ([K.new(1)] | [K.new(1), K.new(2)]).size", "2\n"},
		{k + "p ([K.new(1), K.new(2)] - [K.new(1)]).size", "1\n"},
		{k + "p [K.new(1), K.new(2)].difference([K.new(2)]).size", "1\n"},
		{k + "p [K.new(1)].union([K.new(1)], [K.new(3)]).size", "2\n"},
		{k + "p [K.new(1), K.new(2)].intersection([K.new(2)], [K.new(2)]).size", "1\n"},
		// A #hash mismatch keeps members distinct even when #eql? would say equal.
		{"class H; def eql?(o); true; end; def hash; object_id; end; end; p [H.new, H.new].uniq.size", "2\n"},
		// A Bignum #hash is folded to a bucket like any other.
		{"class B; def hash; 10**40; end; def eql?(o); o.is_a?(B); end; end; p [B.new, B.new].uniq.size", "1\n"},
		// A non-Integer #hash raises TypeError, as in MRI.
		{"class N; def hash; \"x\"; end; def eql?(o); true; end; end; " +
			"p(([N.new, N.new].uniq rescue $!.class))", "TypeError\n"},

		// Value-type set operations keep eql? (not ==) semantics: 1 and 1.0 differ.
		{"p [[1, 1.0, 1].uniq, [1] | [1.0], [1, 1.0] - [1]]", "[[1, 1.0], [1, 1.0], [1.0]]\n"},

		// Hash#eql? and Array#eql? dispatch each member's #eql?.
		{k + "p({1 => K.new(5)}.eql?({1 => K.new(5)}))", "true\n"},
		{k + "p [K.new(3)].eql?([K.new(3)])", "true\n"},
		// An Array-subclass member in an eql? comparison is unwrapped to its array.
		{"class MA < Array; end; p([MA.new([1])].eql?([[1]]))", "true\n"},
		// Two mutually recursive hashes are eql? (the recursive-pair terminates).
		{"h1 = {}; h2 = {}; h1[:x] = h1; h2[:x] = h2; p(h1.eql?(h2))", "true\n"},

		// A Set member (built-in value equality) inside a Struct compares by value
		// through the VM-less Struct#== path — equal, unequal, and size-mismatched.
		{"require 'set'; S = Struct.new(:a); p [S.new(Set[1]) == S.new(Set[1]), S.new(Set[1]) == S.new(Set[2]), S.new(Set[1]) == S.new(Set[1, 2])]", "[true, false, false]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestArrayComparison covers Array#<=> : returning an element's raw non-zero <=>
// result, length ordering, #to_ary coercion of the argument, and subclass
// handling. Asserted against MRI Ruby 4.0.
func TestArrayComparison(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p([1, 2, 3] <=> [1, 2, 3])", "0\n"},
		{"p([1, 2] <=> [1, 2, 3])", "-1\n"},
		{"p([1, 2, 3] <=> [1, 2])", "1\n"},
		{"p([1, 2, 3] <=> [1, 5, 3])", "-1\n"},
		// The element's raw non-zero <=> result propagates (here a nil → nil).
		{"class C; def <=>(o); nil; end; end; p([C.new] <=> [C.new])", "nil\n"},
		// A non-Array argument answering #to_ary is converted.
		{"class TA; def to_ary; [1, 2, 3]; end; end; p([1, 2, 3] <=> TA.new)", "0\n"},
		// An Array subclass argument is compared structurally (no #to_ary call).
		{"class ML < Array; end; p([5, 6, 7] <=> ML.new([5, 6, 7]))", "0\n"},
		// A non-array-like argument is incomparable.
		{"p([] <=> false)", "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
