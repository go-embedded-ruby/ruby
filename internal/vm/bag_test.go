package vm_test

import (
	"strings"
	"testing"
)

// TestBag covers Ruby Bag (backed by github.com/go-composites/bag) — a multiset
// / Counter, a go-composites extension rather than a Ruby core class:
// construction, the count/membership queries, the cardinality queries, mutation,
// iteration, conversion, the multiset combinators (sum/union/intersection/
// difference), equality and inspection. Renderings sort members by their
// inspected form, so map order never makes the assertions flaky.
func TestBag(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction + inspect (duplicates counted, sorted by inspected member).
		{`p Bag.new`, "#<Bag: {}>\n"},
		{`p Bag.new(["a", "a", "b"])`, "#<Bag: {\"a\"=>2, \"b\"=>1}>\n"},
		{`p Bag.new(nil)`, "#<Bag: {}>\n"},
		{`p Bag[1, 1, 2, 3]`, "#<Bag: {1=>2, 2=>1, 3=>1}>\n"},
		{`p Bag.new(Bag.new([1, 1, 2]))`, "#<Bag: {1=>2, 2=>1}>\n"}, // seed from another Bag
		{`p Bag.new(Set.new([1, 2, 2]))`, "#<Bag: {1=>1, 2=>1}>\n"}, // seed from a Set (once each)
		{`puts Bag.new([1, 2])`, "#<Bag: {1=>1, 2=>1}>\n"},          // to_s == inspect
		{`p Bag.new([1]).inspect`, "\"#<Bag: {1=>1}>\"\n"},
		{`p Bag.new([1]).to_s`, "\"#<Bag: {1=>1}>\"\n"},
		// Heterogeneous comparable members.
		{`p Bag.new(["a", :a, 1, 1.5, true, nil]).distinct_size`, "6\n"},
		{`p Bag.new([10 ** 30, 10 ** 30]).count(10 ** 30)`, "2\n"}, // Bignum keying
		// add / << (with multiplicity).
		{`b = Bag.new([1]); b.add(1); p b.count(1)`, "2\n"},
		{`b = Bag.new; b << 1 << 1 << 2; p b`, "#<Bag: {1=>2, 2=>1}>\n"},
		// remove / delete (present, drops at zero, and absent no-op).
		{`b = Bag.new([1, 1]); b.remove(1); p b.count(1)`, "1\n"},
		{`b = Bag.new([1]); b.delete(1); p b.include?(1)`, "false\n"},
		{`b = Bag.new([1]); b.remove(9); p b`, "#<Bag: {1=>1}>\n"},
		// count / membership.
		{`p Bag.new([1, 1]).count(1)`, "2\n"},
		{`p Bag.new([1]).count(9)`, "0\n"},
		{`p Bag.new([1, 2]).include?(2)`, "true\n"},
		{`p Bag.new([1, 2]).member?(3)`, "false\n"},
		// cardinality (size counts multiplicity; distinct_size ignores it).
		{`p Bag.new([1, 1, 2]).size`, "3\n"},
		{`p Bag.new([1, 1, 2]).length`, "3\n"},
		{`p Bag.new([1, 1, 2]).distinct_size`, "2\n"},
		{`p Bag.new.empty?`, "true\n"},
		{`p Bag.new([1]).empty?`, "false\n"},
		{`b = Bag.new([1, 1, 2]); b.clear; p b`, "#<Bag: {}>\n"},
		// iteration / conversion.
		{`Bag.new(["a", "a", "b"]).each { |item, n| print "#{item}:#{n} " }`, "a:2 b:1 "},
		{`p Bag.new(["b", "a", "a"]).to_a`, "[\"a\", \"a\", \"b\"]\n"}, // with multiplicity, sorted
		{`p Bag.new(["b", "a", "a"]).distinct`, "[\"a\", \"b\"]\n"},    // each once, sorted
		// sum / + (counts add).
		{`p(Bag.new([1, 1]) + Bag.new([1, 2]))`, "#<Bag: {1=>3, 2=>1}>\n"},
		{`p Bag.new([1]).send(:+, Bag.new([1]))`, "#<Bag: {1=>2}>\n"}, // explicit method send
		// union (| / union): max of counts.
		{`p(Bag.new([1, 1]) | Bag.new([1, 1, 1]))`, "#<Bag: {1=>3}>\n"},
		{`p Bag.new([1, 2]).union(Bag.new([2, 2]))`, "#<Bag: {1=>1, 2=>2}>\n"},
		// intersection (& / intersection): min of counts.
		{`p(Bag.new([1, 1, 2]) & Bag.new([1, 2, 2]))`, "#<Bag: {1=>1, 2=>1}>\n"},
		{`p Bag.new([1, 1]).intersection(Bag.new([1]))`, "#<Bag: {1=>1}>\n"},
		// difference (- / difference): counts subtract, floored at zero.
		{`p(Bag.new([1, 1, 1]) - Bag.new([1]))`, "#<Bag: {1=>2}>\n"},
		{`p Bag.new([1, 1, 2]).difference(Bag.new([1]))`, "#<Bag: {1=>1, 2=>1}>\n"},
		// equality (operator routes through valueEqual; the explicit .== too).
		{`p(Bag.new([1, 1, 2]) == Bag.new([2, 1, 1]))`, "true\n"}, // order-independent
		{`p(Bag.new([1, 1]) == Bag.new([1]))`, "false\n"},         // differing multiplicity
		{`p(Bag.new([1]) == [1])`, "false\n"},                     // non-Bag operand
		{`p Bag.new([1, 1]).send(:==, Bag.new([1, 1]))`, "true\n"},
		{`p Bag.new([1]).send(:==, 42)`, "false\n"}, // method, non-Bag
		// truthiness + class.
		{`p(Bag.new ? "y" : "n")`, "\"y\"\n"},
		{`p Bag.new.class`, "Bag\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBagMostCommon covers most_common: all pairs most-common-first (no arg or
// explicit nil), the top n (Integer arg), ties broken by the inspected member,
// and the n edge cases (n larger than the distinct count, zero, negative).
func TestBagMostCommon(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// No arg → all distinct [item, count] pairs, descending by count.
		{`p Bag.new(["a", "a", "b", "c", "c", "c"]).most_common`, `[["c", 3], ["a", 2], ["b", 1]]` + "\n"},
		// Explicit nil behaves like no arg.
		{`p Bag.new(["a", "a", "b", "c", "c", "c"]).most_common(nil)`, `[["c", 3], ["a", 2], ["b", 1]]` + "\n"},
		// Integer n → the top n only.
		{`p Bag.new(["a", "a", "b", "c", "c", "c"]).most_common(2)`, `[["c", 3], ["a", 2]]` + "\n"},
		{`p Bag.new(["a", "a", "b", "c", "c", "c"]).most_common(1)`, `[["c", 3]]` + "\n"},
		// Ties broken by the inspected member (ascending): equal counts → "a" before "b".
		{`p Bag.new(["b", "a"]).most_common`, `[["a", 1], ["b", 1]]` + "\n"},
		// n larger than the distinct count returns everything.
		{`p Bag.new(["a", "b"]).most_common(10)`, `[["a", 1], ["b", 1]]` + "\n"},
		// n == 0 and n < 0 both yield an empty Array.
		{`p Bag.new(["a", "b"]).most_common(0)`, "[]\n"},
		{`p Bag.new(["a", "b"]).most_common(-1)`, "[]\n"},
		// Empty Bag → empty Array, with and without an arg.
		{`p Bag.new.most_common`, "[]\n"},
		{`p Bag.new.most_common(3)`, "[]\n"},
		// Integer members (count descending, ties by inspected member).
		{`p Bag.new([1, 1, 1, 2, 2, 3]).most_common`, `[[1, 3], [2, 2], [3, 1]]` + "\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBagErrors covers the raising paths: a non-comparable member, a non-Bag
// operand to a combinator, a non-enumerable seed argument, and each without a
// block.
func TestBagErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Bag.new([[1, 2]])`, "TypeError"},     // Array member (non-comparable)
		{`Bag.new({})`, "TypeError"},           // Hash is not enumerable here
		{`Bag.new([1]).add([2])`, "TypeError"}, // non-comparable add
		{`Bag.new([1]).remove([2])`, "TypeError"},
		{`Bag.new([1]).count([2])`, "TypeError"},
		{`Bag.new([1]).include?([2])`, "TypeError"},
		{`Bag.new([1]) | [2]`, "TypeError"}, // non-Bag operand
		{`Bag.new([1]) & 3`, "TypeError"},   // non-Bag operand
		{`Bag.new([1]).union(5)`, "TypeError"},
		{`Bag.new([1]).intersection(5)`, "TypeError"},
		{`Bag.new([1]).difference(5)`, "TypeError"},
		{`Bag.new([1, 2]).each`, "LocalJumpError"},
		{`Bag.new([1]) + 2`, "TypeError"},                // + operator, non-Bag right operand
		{`Bag.new([1]) - 2`, "TypeError"},                // - operator, non-Bag right operand
		{`Bag.new([1]) * Bag.new([2])`, "NoMethodError"}, // unsupported operator
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
