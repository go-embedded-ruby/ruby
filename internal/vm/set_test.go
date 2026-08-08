package vm_test

import (
	"strings"
	"testing"
)

// TestSet covers Ruby Set (backed by an object.Hash of member => true, the way
// MRI's set.rb backs Set with a Hash): construction and the each_entry/each
// seeding protocol, membership (by #hash/#eql?, including container and nested-Set
// members), cardinality, mutation, iteration, conversion, the algebra operators
// (each accepting any Enumerable), the subset/superset/comparison predicates, the
// higher-order methods and the MRI "#<Set: {…}>" inspection — every value
// asserted against MRI 3.4's stdlib Set.
func TestSet(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction + inspect (insertion order, duplicates collapsed).
		{`p Set.new`, "#<Set: {}>\n"},
		{`p Set.new([1, 2, 2, 3])`, "#<Set: {1, 2, 3}>\n"},
		{`p Set.new(nil)`, "#<Set: {}>\n"},
		{`p Set[1, 2, 2, 3]`, "#<Set: {1, 2, 3}>\n"},
		{`p Set[]`, "#<Set: {}>\n"},
		{`p Set.new(Set.new([1, 2]))`, "#<Set: {1, 2}>\n"},            // seed from another Set
		{`p Set.new({})`, "#<Set: {}>\n"},                             // an (empty) Hash is enumerable
		{`p Set.new([1, 2, 3]) { |x| x * x }`, "#<Set: {1, 4, 9}>\n"}, // block preprocesses
		{`p Set.new([]) { |x| x }`, "#<Set: {}>\n"},                   // empty enum + block
		{`puts Set.new([1, 2])`, "#<Set: {1, 2}>\n"},                  // to_s == inspect
		{`p Set.new([1, 2]).inspect`, "\"#<Set: {1, 2}>\"\n"},
		{`p Set.new([1, 2]).to_s`, "\"#<Set: {1, 2}>\"\n"},
		// A Set that contains itself renders with the cycle marker.
		{`s = Set.new; s.add(s); p s`, "#<Set: {#<Set: {...}>}>\n"},
		// Heterogeneous members (String distinct from Symbol, Bignum, Float, …).
		{`p Set.new(["a", :a, 1, 1.5, true, nil])`, "#<Set: {\"a\", :a, 1, 1.5, true, nil}>\n"},
		{`p Set.new([10 ** 30, 10 ** 30]).size`, "1\n"}, // Bignum keying
		// Container members key by content (#hash/#eql?), like MRI's Hash-backed Set.
		{`p Set.new([[1, 2], [1, 2]]).size`, "1\n"},
		{`p Set.new([[1, 2]]).include?([1, 2])`, "true\n"},
		{`p Set[Set[1], Set[1]].size`, "1\n"},                       // nested-Set dedup
		{`p Set[Set[1, 2]].include?(Set[2, 1])`, "true\n"},          // nested-Set lookup
		{`p(Set[Set[1], Set[2]] == Set[Set[2], Set[1]])`, "true\n"}, // nested-Set equality
		// Arbitrary objects key by identity (two distinct objects stay distinct).
		{`class O; end; p Set.new([O.new, O.new]).size`, "2\n"},
		{`o = Object.new; s = Set.new; s << o << o; p s.size`, "1\n"},
		// add / << / add?
		{`s = Set.new([1]); s.add(2); p s`, "#<Set: {1, 2}>\n"},
		{`s = Set.new([1]); s << 2 << 3; p s`, "#<Set: {1, 2, 3}>\n"},
		{`s = Set.new([1]); s.add(1); p s.size`, "1\n"}, // idempotent
		{`s = Set.new([1]); p s.add?(2)`, "#<Set: {1, 2}>\n"},
		{`s = Set.new([1]); p s.add?(1)`, "nil\n"}, // already present
		// delete / delete? (present and absent).
		{`s = Set.new([1, 2, 3]); s.delete(2); p s`, "#<Set: {1, 3}>\n"},
		{`s = Set.new([1, 2]); s.delete(9); p s`, "#<Set: {1, 2}>\n"},
		{`s = Set.new([1, 2]); p s.delete?(1)`, "#<Set: {2}>\n"},
		{`s = Set.new([1, 2]); p s.delete?(9)`, "nil\n"},
		// membership + case equality + aliases.
		{`p Set.new([1, 2]).include?(2)`, "true\n"},
		{`p Set.new([1, 2]).member?(3)`, "false\n"},
		{`p(Set.new([1, 2]) === 1)`, "true\n"},
		// cardinality.
		{`p Set.new([1, 2, 3]).size`, "3\n"},
		{`p Set.new([1, 2, 3]).length`, "3\n"},
		{`p Set.new([1, 2, 3]).count`, "3\n"},             // Enumerable#count
		{`p Set.new([1, 2, 3, 4]).count(&:even?)`, "2\n"}, // Enumerable#count with a block
		{`p Set.new.empty?`, "true\n"},
		{`p Set.new([1]).empty?`, "false\n"},
		{`s = Set.new([1, 2]); s.clear; p s`, "#<Set: {}>\n"},
		// iteration / conversion / Enumerator.
		{`Set.new([1, 2, 3]).each { |x| print x }`, "123"},
		{`p Set.new([1, 2, 3]).each { |x| x }`, "#<Set: {1, 2, 3}>\n"}, // returns self
		{`p Set.new([1, 2]).each.class`, "Enumerator\n"},               // no block → Enumerator
		{`p Set.new([3, 1, 2]).to_a`, "[3, 1, 2]\n"},
		{`p Set.new([1]).to_set.equal?(Set.new([1]).to_set)`, "false\n"}, // sanity: distinct objects
		{`s = Set.new([1]); p s.to_set.equal?(s)`, "true\n"},             // Set#to_set is self
		{`p [1, 2, 2, 3].to_set.class`, "Set\n"},                         // Enumerable#to_set
		{`p((1..3).to_set { |x| x * 2 }.to_a.sort)`, "[2, 4, 6]\n"},      // to_set with a block
		// union (| / union / +): accepts any Enumerable; a's order first.
		{`p(Set.new([1, 2]) | Set.new([2, 3]))`, "#<Set: {1, 2, 3}>\n"},
		{`p(Set.new([1, 2]) | [2, 3])`, "#<Set: {1, 2, 3}>\n"},
		{`p Set.new([1]).union([2])`, "#<Set: {1, 2}>\n"},
		{`p(Set.new([1, 2]) + [3])`, "#<Set: {1, 2, 3}>\n"},
		// intersection (& / intersection).
		{`p(Set.new([1, 2, 3]) & [2, 3, 4])`, "#<Set: {2, 3}>\n"},
		{`p Set.new([1, 2]).intersection([2])`, "#<Set: {2}>\n"},
		// difference (- / difference) and subtract (mutating).
		{`p(Set.new([1, 2, 3]) - [2])`, "#<Set: {1, 3}>\n"},
		{`p Set.new([1, 2, 3]).difference([1, 3])`, "#<Set: {2}>\n"},
		{`s = Set.new([1, 2, 3]); p s.subtract([2]).equal?(s)`, "true\n"},
		{`s = Set.new([1, 2, 3]); s.subtract([2]); p s`, "#<Set: {1, 3}>\n"},
		// symmetric difference.
		{`p((Set.new([1, 2]) ^ [2, 3]).to_a.sort)`, "[1, 3]\n"},
		{`p((Set.new([1, 2, 3]) ^ Set.new([1, 2, 3])).to_a)`, "[]\n"},
		// subset / superset / proper / comparison.
		{`p(Set.new([1, 2]) <= Set.new([1, 2, 3]))`, "true\n"},
		{`p Set.new([1, 4]).subset?(Set.new([1, 2, 3]))`, "false\n"},
		{`p Set.new([1, 2, 3]).superset?(Set.new([1, 2]))`, "true\n"},
		{`p(Set.new([1, 2, 3]) >= Set.new([1, 2]))`, "true\n"},
		{`p(Set.new([1, 2]) < Set.new([1, 2, 3]))`, "true\n"},
		{`p(Set.new([1, 2]) < Set.new([1, 2]))`, "false\n"},
		{`p(Set.new([1, 2, 3]) > Set.new([1, 2]))`, "true\n"},
		{`p Set.new([1, 2]).proper_subset?(Set.new([1, 2, 3]))`, "true\n"},
		{`p Set.new([1, 2, 3]).proper_superset?(Set.new([1, 2]))`, "true\n"},
		{`p(Set.new([1, 2]) <=> Set.new([1, 2, 3]))`, "-1\n"},
		{`p(Set.new([1, 2, 3]) <=> Set.new([1, 2]))`, "1\n"},
		{`p(Set.new([1, 2]) <=> Set.new([1, 2]))`, "0\n"},
		{`p(Set.new([1, 2]) <=> Set.new([3, 4]))`, "nil\n"},
		{`p(Set.new([1, 2]) <=> 5)`, "nil\n"}, // non-Set argument
		// disjoint? / intersect?.
		{`p Set.new([1, 2]).disjoint?(Set.new([3, 4]))`, "true\n"},
		{`p Set.new([1, 2]).disjoint?(Set.new([2, 3]))`, "false\n"},
		{`p Set.new([1, 2]).intersect?(Set.new([2, 3]))`, "true\n"},
		{`p Set.new([1, 2, 3]).intersect?(Set.new([9]))`, "false\n"},
		// equality (operator dispatches Set#==; explicit method send too).
		{`p(Set.new([1, 2, 3]) == Set.new([3, 2, 1]))`, "true\n"},
		{`p(Set.new([1, 2]) == Set.new([1, 2, 3]))`, "false\n"},
		{`p(Set.new([1, 2]) == [1, 2])`, "false\n"},          // non-Set operand
		{`p(Set.new([1, 2]) == Set.new([1, 3]))`, "false\n"}, // same size, differing member
		{`p Set.new([1, 2]).eql?(Set.new([2, 1]))`, "true\n"},
		{`p Set.new([1, 2]).send(:==, 42)`, "false\n"},
		{`s = Set.new([1]); p(s == s)`, "true\n"}, // identity short-circuit
		// hash: equal for equal Sets, order-independent; distinct for distinct Sets.
		{`p(Set[1, 2, 3].hash == Set[3, 2, 1].hash)`, "true\n"},
		{`p(Set[].hash == Set[1, 2, 3].hash)`, "false\n"},
		// merge (mutating, accepts several enumerables); returns self.
		{`s = Set.new([1]); p s.merge([2, 3], Set.new([4])).equal?(s)`, "true\n"},
		{`s = Set.new([1]); s.merge([2, 3], Set.new([4])); p s`, "#<Set: {1, 2, 3, 4}>\n"},
		// replace (Set arg vs any Enumerable), returns self.
		{`s = Set.new([1, 2]); p s.replace([3, 4]).equal?(s)`, "true\n"},
		{`s = Set.new([1, 2]); s.replace(Set.new([9])); p s`, "#<Set: {9}>\n"},
		{`s = Set.new([1, 2]); s.reset; p s`, "#<Set: {1, 2}>\n"},
		// select / reject / map / collect come from Enumerable → Array results.
		{`p Set.new([1, 2, 3, 4]).select { |x| x.even? }.class`, "Array\n"},
		{`p Set.new([1, 2, 3, 4]).select { |x| x.even? }.sort`, "[2, 4]\n"},
		{`p Set.new([1, 2, 3, 4]).reject { |x| x.even? }.sort`, "[1, 3]\n"},
		{`p Set.new([1, 2, 3]).map { |x| x * x }.sort`, "[1, 4, 9]\n"},
		{`p Set.new([1, 2, 3]).find { |x| x > 1 }`, "2\n"},
		// bang mutators keep_if / delete_if / select! / reject! / map!.
		{`s = Set.new([1, 2, 3, 4]); p s.keep_if { |x| x.even? }.equal?(s)`, "true\n"},
		{`s = Set.new([1, 2, 3, 4]); s.keep_if { |x| x.even? }; p s`, "#<Set: {2, 4}>\n"},
		{`s = Set.new([1, 2, 3, 4]); p s.delete_if { |x| x.even? }.equal?(s)`, "true\n"},
		{`s = Set.new([1, 2, 3, 4]); s.delete_if { |x| x.even? }; p s`, "#<Set: {1, 3}>\n"},
		{`s = Set.new([1, 2, 3]); p s.select! { |x| x > 1 }`, "#<Set: {2, 3}>\n"}, // changed → self
		{`s = Set.new([1, 2, 3]); p s.select! { |x| true }`, "nil\n"},             // unchanged → nil
		{`s = Set.new([1, 2, 3]); p s.filter! { |x| x > 1 }`, "#<Set: {2, 3}>\n"}, // alias
		{`s = Set.new([1, 2, 3]); p s.reject! { |x| x > 1 }`, "#<Set: {1}>\n"},    // changed → self
		{`s = Set.new([1, 2, 3]); p s.reject! { |x| false }`, "nil\n"},            // unchanged → nil
		{`s = Set.new([1, 2, 3]); p s.map! { |x| x * 2 }.equal?(s)`, "true\n"},
		{`s = Set.new([1, 2, 3]); s.collect! { |x| x * 2 }; p s`, "#<Set: {2, 4, 6}>\n"}, // alias
		// Enumerator (no block) forms of the bang/higher-order methods.
		{`p Set.new([1, 2]).select!.class`, "Enumerator\n"},
		{`p Set.new([1, 2]).reject!.class`, "Enumerator\n"},
		{`p Set.new([1, 2]).delete_if.class`, "Enumerator\n"},
		{`p Set.new([1, 2]).keep_if.class`, "Enumerator\n"},
		{`p Set.new([1, 2]).map!.class`, "Enumerator\n"},
		{`p Set.new([1, 2]).classify.class`, "Enumerator\n"},
		{`p Set.new([1, 2]).divide.class`, "Enumerator\n"},
		// classify / divide / flatten / join.
		{`p Set.new([1, 2, 3, 4]).classify { |x| x.even? }`, "{false => #<Set: {1, 3}>, true => #<Set: {2, 4}>}\n"},
		{`p Set.new([1, 2, 3, 4]).divide { |x| x.even? }.map { |s| s.to_a.sort }.sort`, "[[1, 3], [2, 4]]\n"},
		{`p Set[1, 3, 4, 6].divide { |x, y| (x - y).abs == 1 }.map { |s| s.to_a.sort }.sort`, "[[1], [3, 4], [6]]\n"},
		{`p Set.new([1, 2, Set.new([3, 4, Set.new([5, 6])])]).flatten.to_a.sort`, "[1, 2, 3, 4, 5, 6]\n"},
		{`s = Set.new([1, 2, Set.new([3])]); p s.flatten!.equal?(s)`, "true\n"}, // changed → self
		{`p Set.new([1, 2, 3]).flatten!`, "nil\n"},                              // no nested Set → nil
		{`p Set.new([:a, :b, :c]).join`, "\"abc\"\n"},
		{`p Set.new([:a, :b, :c]).join("-")`, "\"a-b-c\"\n"},
		{`p Set.new.join`, "\"\"\n"},
		// dup / clone: shallow copy, independent of the original.
		{`s = Set.new([1, 2]); c = s.dup; c.add(3); p s.to_a.sort`, "[1, 2]\n"},
		{`s = Set.new([1, 2]); c = s.clone; c.add(3); p c.to_a.sort`, "[1, 2, 3]\n"},
		// Array membership of Sets goes through the value-equality (eq) fast path.
		{`p [Set.new([1, 2])].include?(Set.new([2, 1]))`, "true\n"},  // eq: equal members
		{`p [Set.new([1, 2])].include?(Set.new([1, 3]))`, "false\n"}, // eq: differing member
		{`p [Set.new([1, 2])].include?(Set.new([1]))`, "false\n"},    // eq: differing size
		// truthiness + class.
		{`p(Set.new ? "y" : "n")`, "\"y\"\n"},
		{`p Set.new.class`, "Set\n"},
		{`p Set.ancestors.include?(Enumerable)`, "true\n"},
		{`p Set.private_instance_methods(false).include?(:initialize)`, "true\n"},
		{`p Set.protected_instance_methods(false).include?(:flatten_merge)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSetCompareByIdentity covers the compare_by_identity flag: switching a Set to
// identity membership, the flag surviving dup, and its per-operator retention
// through the algebra (| / - retain it; & / ^ / map! / flatten produce a plain
// Set; replace transfers a Set argument's flag).
func TestSetCompareByIdentity(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Set.new.compare_by_identity?`, "false\n"},
		{`s = Set.new; p s.compare_by_identity.equal?(s)`, "true\n"},
		{`s = Set.new.compare_by_identity; p s.compare_by_identity?`, "true\n"},
		// Distinct String objects with equal content stay distinct under identity.
		{`s = Set.new.compare_by_identity; s.merge(["a", "a".dup]); p s.size`, "2\n"},
		// Immediates still collapse (equal by value == equal object).
		{`s = Set.new.compare_by_identity; s.merge([:a, :a]); p s.size`, "1\n"},
		// dup carries the flag.
		{`s = Set.new.compare_by_identity; s << :a; p s.dup.compare_by_identity?`, "true\n"},
		// Retention through the algebra.
		{`p((Set[1, 2].compare_by_identity | [3]).compare_by_identity?)`, "true\n"},
		{`p((Set[1, 2].compare_by_identity - [3]).compare_by_identity?)`, "true\n"},
		{`p((Set[1, 2].compare_by_identity & [1]).compare_by_identity?)`, "false\n"},
		{`p((Set[1, 2].compare_by_identity ^ [3]).compare_by_identity?)`, "false\n"},
		{`s = Set[1, 2].compare_by_identity; s.map! { |x| x }; p s.compare_by_identity?`, "false\n"},
		{`p(Set[1, 2, Set[3]].compare_by_identity.flatten.compare_by_identity?)`, "false\n"},
		// replace: a Set argument transfers its flag; any other Enumerable keeps self's.
		{`s = Set[:a].compare_by_identity; s.replace(Set[1]); p s.compare_by_identity?`, "false\n"},
		{`s = Set[:a]; s.replace(Set[1].compare_by_identity); p s.compare_by_identity?`, "true\n"},
		{`s = Set[:a].compare_by_identity; s.replace([1]); p s.compare_by_identity?`, "true\n"},
		// A compare_by_identity Set is not == a content-comparing one.
		{`p(Set[1, 2] == Set[1, 2].compare_by_identity)`, "false\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSetErrors covers the raising paths: a non-Enumerable seed / algebra operand
// (ArgumentError), a non-Set predicate operand (ArgumentError), an unsupported
// operator (NoMethodError), a recursive flatten, and structural modification
// during iteration (RuntimeError, for every guarded mutator).
func TestSetErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// Non-enumerable seed / algebra operand → ArgumentError "value must be enumerable".
		{`Set.new(5)`, "ArgumentError"},
		{`Set.new(Object.new)`, "ArgumentError"},
		{`Set.new([1]) | 2`, "ArgumentError"},
		{`Set.new([1]) & 3`, "ArgumentError"},
		{`Set.new([1]) - 2`, "ArgumentError"},
		{`Set.new([1]) + 2`, "ArgumentError"},
		{`Set.new([1]) ^ 3`, "ArgumentError"},
		{`Set.new([1]).merge(5)`, "ArgumentError"},
		{`Set.new([1]).subtract(5)`, "ArgumentError"},
		{`Set.new([1]).replace(5)`, "ArgumentError"},
		// Non-Set predicate operand → ArgumentError "value must be a set".
		{`Set.new([1]).subset?([1])`, "ArgumentError"},
		{`Set.new([1]).superset?(1)`, "ArgumentError"},
		{`Set.new([1]).proper_subset?(1)`, "ArgumentError"},
		{`Set.new([1]).proper_superset?(1)`, "ArgumentError"},
		{`Set.new([1]) < 3`, "ArgumentError"},
		{`Set.new([1]) > 3`, "ArgumentError"},
		// Unsupported operator.
		{`Set.new([1]) * Set.new([2])`, "NoMethodError"},
		// Recursive flatten.
		{`s = Set.new; s.add(s); s.flatten`, "tried to flatten recursive Set"},
		{`s = Set.new; s.add(s); s.flatten!`, "tried to flatten recursive Set"},
		// Structural modification during iteration → RuntimeError (each guarded path).
		{`s = Set.new([1, 2]); s.each { |_| s.add(3) }`, "iteration"},
		{`s = Set.new([1, 2]); s.each { |_| s << 3 }`, "iteration"},
		{`s = Set.new([1, 2]); s.each { |_| s.delete(1) }`, "iteration"},
		{`s = Set.new([1, 2]); s.each { |_| s.clear }`, "iteration"},
		{`s = Set.new([1, 2]); s.each { |_| s.merge([9]) }`, "iteration"},
		{`s = Set.new([1, 2]); s.each { |_| s.replace([9]) }`, "iteration"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
