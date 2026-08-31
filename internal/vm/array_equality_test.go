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
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
