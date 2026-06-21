package vm_test

import (
	"strings"
	"testing"
)

// TestSet covers Ruby Set (backed by github.com/go-composites/set):
// construction, membership, the cardinality queries, mutation, iteration,
// conversion, the algebraic combinators (union/intersection/difference), the
// subset/superset/equality predicates, and inspection — every value asserted
// against MRI's stdlib Set.
func TestSet(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction + inspect (insertion order, duplicates collapsed).
		{`p Set.new`, "#<Set: {}>\n"},
		{`p Set.new([1, 2, 2, 3])`, "#<Set: {1, 2, 3}>\n"},
		{`p Set.new(nil)`, "#<Set: {}>\n"},
		{`p Set[1, 2, 2, 3]`, "#<Set: {1, 2, 3}>\n"},
		{`p Set.new(Set.new([1, 2]))`, "#<Set: {1, 2}>\n"}, // seed from another Set
		{`puts Set.new([1, 2])`, "#<Set: {1, 2}>\n"},       // to_s == inspect
		{`p Set.new([1, 2]).inspect`, "\"#<Set: {1, 2}>\"\n"},
		{`p Set.new([1, 2]).to_s`, "\"#<Set: {1, 2}>\"\n"},
		// Heterogeneous comparable members (String distinct from Symbol, Bignum, etc.).
		{`p Set.new(["a", :a, 1, 1.5, true, nil])`, "#<Set: {\"a\", :a, 1, 1.5, true, nil}>\n"},
		{`p Set.new([10 ** 30, 10 ** 30]).size`, "1\n"}, // Bignum keying
		// add / << / add?
		{`s = Set.new([1]); s.add(2); p s`, "#<Set: {1, 2}>\n"},
		{`s = Set.new([1]); s << 2 << 3; p s`, "#<Set: {1, 2, 3}>\n"},
		{`s = Set.new([1]); s.add(1); p s.size`, "1\n"}, // idempotent
		{`s = Set.new([1]); p s.add?(2)`, "#<Set: {1, 2}>\n"},
		{`s = Set.new([1]); p s.add?(1)`, "nil\n"}, // already present
		// delete (present and absent).
		{`s = Set.new([1, 2, 3]); s.delete(2); p s`, "#<Set: {1, 3}>\n"},
		{`s = Set.new([1, 2]); s.delete(9); p s`, "#<Set: {1, 2}>\n"},
		// membership.
		{`p Set.new([1, 2]).include?(2)`, "true\n"},
		{`p Set.new([1, 2]).member?(3)`, "false\n"},
		{`p(Set.new([1, 2]) === 1)`, "true\n"},
		// cardinality.
		{`p Set.new([1, 2, 3]).size`, "3\n"},
		{`p Set.new([1, 2, 3]).length`, "3\n"},
		{`p Set.new([1, 2, 3]).count`, "3\n"},
		{`p Set.new.empty?`, "true\n"},
		{`p Set.new([1]).empty?`, "false\n"},
		{`s = Set.new([1, 2]); s.clear; p s`, "#<Set: {}>\n"},
		// iteration / conversion.
		{`Set.new([1, 2, 3]).each { |x| print x }`, "123"},
		{`p Set.new([3, 1, 2]).to_a`, "[3, 1, 2]\n"},
		{`p Set.new([1]).to_set.class`, "Set\n"},
		// union (| / union / +): a's order first, then b's new members.
		{`p(Set.new([1, 2]) | Set.new([2, 3]))`, "#<Set: {1, 2, 3}>\n"},
		{`p Set.new([1]).union(Set.new([2]))`, "#<Set: {1, 2}>\n"},
		{`p(Set.new([1, 2]) + Set.new([3]))`, "#<Set: {1, 2, 3}>\n"},
		// intersection (& / intersection).
		{`p(Set.new([1, 2, 3]) & Set.new([2, 3, 4]))`, "#<Set: {2, 3}>\n"},
		{`p Set.new([1, 2]).intersection(Set.new([2]))`, "#<Set: {2}>\n"},
		// difference (- / difference).
		{`p(Set.new([1, 2, 3]) - Set.new([2]))`, "#<Set: {1, 3}>\n"},
		{`p Set.new([1, 2, 3]).difference(Set.new([1, 3]))`, "#<Set: {2}>\n"},
		// subset / superset / <= / >=.
		{`p(Set.new([1, 2]) <= Set.new([1, 2, 3]))`, "true\n"},
		{`p Set.new([1, 4]).subset?(Set.new([1, 2, 3]))`, "false\n"},
		{`p Set.new([1, 2, 3]).superset?(Set.new([1, 2]))`, "true\n"},
		{`p(Set.new([1, 2, 3]) >= Set.new([1, 2]))`, "true\n"},
		// equality (operator routes through valueEqual; the explicit .== method is
		// exercised too, including its non-Set short-circuit).
		{`p(Set.new([1, 2, 3]) == Set.new([3, 2, 1]))`, "true\n"}, // order-independent
		{`p(Set.new([1, 2]) == Set.new([1, 2, 3]))`, "false\n"},
		{`p(Set.new([1, 2]) == [1, 2])`, "false\n"},                    // non-Set operand
		{`p Set.new([1, 2]).send(:==, Set.new([2, 1]))`, "true\n"},     // explicit method send
		{`p Set.new([1, 2]).send(:==, Set.new([2, 1, 3]))`, "false\n"}, // method, differing
		{`p Set.new([1, 2]).send(:==, 42)`, "false\n"},                 // method, non-Set
		// merge (mutating, accepts several enumerables).
		{`s = Set.new([1]); s.merge([2, 3], Set.new([4])); p s`, "#<Set: {1, 2, 3, 4}>\n"},
		// truthiness + class.
		{`p(Set.new ? "y" : "n")`, "\"y\"\n"},
		{`p Set.new.class`, "Set\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSetErrors covers the raising paths: a non-comparable member, a non-Set
// operand to an algebraic combinator, a non-enumerable seed/merge argument, and
// each without a block.
func TestSetErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Set.new([[1, 2]])`, "TypeError"},     // Array member (non-comparable)
		{`Set.new({})`, "TypeError"},           // Hash is not enumerable here
		{`Set.new([1]).add([2])`, "TypeError"}, // non-comparable add
		{`Set.new([1]).include?([2])`, "TypeError"},
		{`Set.new([1]) | [2]`, "TypeError"}, // non-Set operand
		{`Set.new([1]) & 3`, "TypeError"},   // non-Set operand
		{`Set.new([1]).merge(5)`, "TypeError"},
		{`Set.new([1, 2]).each`, "LocalJumpError"},
		{`Set.new([1]) + 2`, "TypeError"},                // + operator, non-Set right operand
		{`Set.new([1]) - 2`, "TypeError"},                // - operator, non-Set right operand
		{`Set.new([1]) * Set.new([2])`, "NoMethodError"}, // unsupported operator
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
