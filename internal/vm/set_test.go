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
		// Arbitrary objects are valid members, keyed by identity like Ruby (an
		// Array member is allowed; two distinct objects stay distinct).
		{`p Set.new([[1, 2]]).size`, "1\n"},
		{`s = Set.new([1]); s.add([2]); p s.size`, "2\n"},
		{`p Set.new([1]).include?([2])`, "false\n"},
		{`class O; end; p Set.new([O.new, O.new]).size`, "2\n"},
		{`o = Object.new; s = Set.new; s << o << o; p s.size`, "1\n"}, // same object collapses
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
		// map / collect: yield each member, results into a (deterministic-sorted) Array.
		{`p Set.new([1, 2, 3]).map { |x| x * x }.sort`, "[1, 4, 9]\n"},
		{`p Set.new([1, 2, 3]).collect { |x| x + 1 }.sort`, "[2, 3, 4]\n"},
		{`p Set.new([1, 2, 3]).map { |x| x }.class`, "Array\n"}, // returns an Array, not a Set
		{`p Set.new.map { |x| x }`, "[]\n"},                     // empty
		// select / filter / reject: new Sets (assert via sorted to_a).
		{`p Set.new([1, 2, 3, 4]).select { |x| x.even? }.to_a.sort`, "[2, 4]\n"},
		{`p Set.new([1, 2, 3, 4]).filter { |x| x.even? }.to_a.sort`, "[2, 4]\n"},
		{`p Set.new([1, 2, 3, 4]).reject { |x| x.even? }.to_a.sort`, "[1, 3]\n"},
		{`p Set.new([1, 2, 3]).select { |x| x > 1 }.class`, "Set\n"},
		{`p Set.new([1, 2]).reject { |x| true }.to_a`, "[]\n"}, // all rejected
		// find / detect: first truthy member, else nil.
		{`p Set.new([1, 2, 3]).find { |x| x > 1 }`, "2\n"},
		{`p Set.new([1, 2, 3]).detect { |x| x > 1 }`, "2\n"},
		{`p Set.new([1, 2, 3]).find { |x| x > 9 }`, "nil\n"}, // none match
		// all? / any? / none? over the block.
		{`p Set.new([2, 4, 6]).all? { |x| x.even? }`, "true\n"},
		{`p Set.new([2, 3, 6]).all? { |x| x.even? }`, "false\n"},
		{`p Set.new([1, 2, 3]).any? { |x| x > 2 }`, "true\n"},
		{`p Set.new([1, 2, 3]).any? { |x| x > 9 }`, "false\n"},
		{`p Set.new([1, 2, 3]).none? { |x| x > 9 }`, "true\n"},
		{`p Set.new([1, 2, 3]).none? { |x| x > 2 }`, "false\n"},
		// empty-set semantics for all? / any? / none?.
		{`p Set.new.all? { |x| x > 0 }`, "true\n"},
		{`p Set.new.any? { |x| x > 0 }`, "false\n"},
		{`p Set.new.none? { |x| x > 0 }`, "true\n"},
		// ^: symmetric difference (members in exactly one operand).
		{`p((Set.new([1, 2]) ^ Set.new([2, 3])).to_a.sort)`, "[1, 3]\n"},
		{`p((Set.new([1, 2, 3]) ^ Set.new([1, 2, 3])).to_a)`, "[]\n"}, // identical → empty
		{`p((Set.new([1, 2]) ^ Set.new([3, 4])).to_a.sort)`, "[1, 2, 3, 4]\n"},
		// disjoint? / intersect?.
		{`p Set.new([1, 2]).disjoint?(Set.new([3, 4]))`, "true\n"},
		{`p Set.new([1, 2]).disjoint?(Set.new([2, 3]))`, "false\n"},
		{`p Set.new([1, 2]).intersect?(Set.new([2, 3]))`, "true\n"},
		{`p Set.new([1, 2]).intersect?(Set.new([3, 4]))`, "false\n"},
		// proper subset / superset (< / >).
		{`p(Set.new([1, 2]) < Set.new([1, 2, 3]))`, "true\n"},
		{`p(Set.new([1, 2]) < Set.new([1, 2]))`, "false\n"}, // equal → not proper
		{`p(Set.new([1, 2, 3]) < Set.new([1, 2]))`, "false\n"},
		{`p(Set.new([1, 2, 3]) > Set.new([1, 2]))`, "true\n"},
		{`p(Set.new([1, 2]) > Set.new([1, 2]))`, "false\n"}, // equal → not proper
		{`p(Set.new([1, 2]) > Set.new([1, 2, 3]))`, "false\n"},
		// dup / clone: shallow copy, independent of the original.
		{`p Set.new([1, 2, 3]).dup`, "#<Set: {1, 2, 3}>\n"},
		{`p Set.new([1, 2, 3]).clone`, "#<Set: {1, 2, 3}>\n"},
		{`s = Set.new([1, 2]); c = s.dup; c.add(3); p s.to_a.sort`, "[1, 2]\n"},      // original untouched
		{`s = Set.new([1, 2]); c = s.clone; c.add(3); p c.to_a.sort`, "[1, 2, 3]\n"}, // copy mutated
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

// TestSetAggregates covers the Enumerable aggregates: sort, min/max, sum (with
// and without an init), and reduce/inject (block form, with and without an
// initial value), every value asserted against MRI's Set / Enumerable.
func TestSetAggregates(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// sort → a new Array ordered by <=> (mixed-comparable numerics coerce).
		{`p Set.new([3, 1, 2]).sort`, "[1, 2, 3]\n"},
		{`p Set.new([3, 1, 2]).sort.class`, "Array\n"},
		{`p Set.new.sort`, "[]\n"},
		{`p Set.new(["b", "a", "c"]).sort`, "[\"a\", \"b\", \"c\"]\n"},
		{`p Set.new([3, 1.5, 2]).sort`, "[1.5, 2, 3]\n"}, // Integer/Float coercion
		// min / max by <=>; nil on the empty set.
		{`p Set.new([3, 1, 2]).min`, "1\n"},
		{`p Set.new([3, 1, 2]).max`, "3\n"},
		{`p Set.new([1]).min`, "1\n"},
		{`p Set.new([1]).max`, "1\n"},
		{`p Set.new.min`, "nil\n"},
		{`p Set.new.max`, "nil\n"},
		// sum: default init 0, and an explicit init (numeric and String).
		{`p Set.new([1, 2, 3]).sum`, "6\n"},
		{`p Set.new([1, 2, 3]).sum(10)`, "16\n"},
		{`p Set.new.sum`, "0\n"},
		{`p Set.new.sum(5)`, "5\n"},
		{`p Set.new([1.5, 2.5]).sum`, "4.0\n"},
		// reduce / inject: with an initial value, and without (first member seeds).
		{`p Set.new([1, 2, 3]).reduce(0) { |a, x| a + x }`, "6\n"},
		{`p Set.new([1, 2, 3]).reduce { |a, x| a + x }`, "6\n"},
		{`p Set.new([1, 2, 3]).inject(100) { |a, x| a + x }`, "106\n"},
		{`p Set.new([4]).reduce { |a, x| a + x }`, "4\n"}, // single member, no init
		{`p Set.new.reduce { |a, x| a + x }`, "nil\n"},    // empty, no init → nil
		{`p Set.new.reduce(0) { |a, x| a + x }`, "0\n"},   // empty, with init
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSetAggregateErrors covers the raising paths of the aggregates: sorting
// mixed-incomparable members (the Array#sort ArgumentError) and reduce/inject
// without a block.
func TestSetAggregateErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Set.new([1, "a"]).sort`, "ArgumentError"}, // Integer vs String incomparable
		{`Set.new([1, 2]).reduce`, "LocalJumpError"},
		{`Set.new([1, 2]).inject`, "LocalJumpError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestSetErrors covers the raising paths: a non-comparable member, a non-Set
// operand to an algebraic combinator, a non-enumerable seed/merge argument, and
// each without a block.
func TestSetErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Set.new({})`, "TypeError"},        // Hash is not enumerable here
		{`Set.new([1]) | [2]`, "TypeError"}, // non-Set operand
		{`Set.new([1]) & 3`, "TypeError"},   // non-Set operand
		{`Set.new([1]).merge(5)`, "TypeError"},
		{`Set.new([1, 2]).each`, "LocalJumpError"},
		{`Set.new([1]) + 2`, "TypeError"},                // + operator, non-Set right operand
		{`Set.new([1]) - 2`, "TypeError"},                // - operator, non-Set right operand
		{`Set.new([1]) * Set.new([2])`, "NoMethodError"}, // unsupported operator
		// Block-taking methods without a block raise LocalJumpError (like each).
		{`Set.new([1, 2]).map`, "LocalJumpError"},
		{`Set.new([1, 2]).collect`, "LocalJumpError"},
		{`Set.new([1, 2]).select`, "LocalJumpError"},
		{`Set.new([1, 2]).filter`, "LocalJumpError"},
		{`Set.new([1, 2]).reject`, "LocalJumpError"},
		{`Set.new([1, 2]).find`, "LocalJumpError"},
		{`Set.new([1, 2]).detect`, "LocalJumpError"},
		{`Set.new([1, 2]).all?`, "LocalJumpError"},
		{`Set.new([1, 2]).any?`, "LocalJumpError"},
		{`Set.new([1, 2]).none?`, "LocalJumpError"},
		// Set-argument methods raise TypeError on a non-Set operand.
		{`Set.new([1]) ^ [2]`, "TypeError"},
		{`Set.new([1]).disjoint?(3)`, "TypeError"},
		{`Set.new([1]).intersect?(3)`, "TypeError"},
		{`Set.new([1]) < 3`, "TypeError"},
		{`Set.new([1]) > 3`, "TypeError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
