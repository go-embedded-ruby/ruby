// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

const setSubPre = `require "set"
class Sub < Set; end
`

// A Set subclass instance reports its own class, and MRI propagates it
// ASYMMETRICALLY: the SUBSETS that classify/divide build take the receiver's
// class (`rb_obj_class(set)`, set.c:890; `set_s_alloc(set_class)`, set.c:953)
// while the OUTER Set those two return is always `rb_cSet` (set.c:946, :1002).
//
// Every expectation was measured on `ruby 4.0.7 (2026-09-15 revision
// 229531a6cf) +PRISM [arm64-darwin25]` through
// scripts/conformance/rubyspec/cmd/oracle.
//
// ⛔ #903 blamed Set.new and Set.[] for ignoring their receiver. They did, and
// that was NOT the cause: `type Set struct` had no class field, so `classOf`
// answered vm.cSet for every *Set and honouring the receiver would have changed
// nothing observable. The class was unrepresentable, not discarded.
//
// ⛔⛔ AND THE ASYMMETRY BELOW COMES OUT FOR FREE, which is exactly why it is
// asserted here. The prelude already writes `self.class.new` for the subsets
// (prelude.rb:1258, :1276, :1442, :1519) and a bare `Set.new` for divide's
// outer Set, and `|` is `dup`. So one `cls` field plus `classOf` reading it
// produced MRI's whole shape with no prelude change — behaviour that is correct
// by accident of another change is behaviour nobody is holding in place.
//
// ⛔ WHICH ROWS GUARD WHICH DIRECTION, measured by disabling `classOf`'s read of
// `cls`: 8 of the 12 rows below fail. The 4 that do NOT are worth naming,
// because a reader could otherwise take them for evidence they are not:
//
//	"it is still a Set"                      is_a? holds either way
//	"a plain Set is unaffected"              a control: it SHOULD be unaffected
//	"the outer Set of divide stays a plain Set"
//	"a block makes to_set build a new Set"   Set in both states
//
// The last two answer `Set` with the fix and without it, so they cannot
// discriminate against the pre-fix engine. They are not redundant: they guard
// the OPPOSITE direction — a later change that propagated the class to divide's
// outer Set, or made to_set return self for a block, would fail them. A row
// that cannot fail from one direction may be the only row that fails from the
// other, and saying so is cheaper than re-deriving it.
func TestASetSubclassReportsItsOwnClass(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the bracket constructor keeps the subclass",
			setSubPre + `p Sub[1, 2].class`, `Sub`},

		{"new keeps the subclass, with and without an enumerable",
			setSubPre + `p Sub.new([1, 2]).class
p Sub.new.class`, "Sub\nSub"},

		{"an anonymous subclass works too",
			setSubPre + `k = Class.new(Set)
p k[1].class == k`, `true`},

		{"it is still a Set",
			setSubPre + `p Sub[1, 2].is_a?(Set)`, `true`},

		// ⛔ Set itself must be untouched: nil cls means Set, so every existing
		// allocation answers exactly as before.
		{"a plain Set is unaffected",
			setSubPre + `p Set[1, 2].class
p Set.new([1]).class
p Set[1].dup.class`, "Set\nSet\nSet"},

		// ⛔ THE ASYMMETRY. Subsets take the receiver's class...
		{"classify builds subsets of the receiver's class, in a plain Hash",
			setSubPre + `h = Sub[1, 2].classify { |x| x }
p h.values.map(&:class)
p h.class`, "[Sub, Sub]\nHash"},

		{"divide builds subsets of the receiver's class",
			setSubPre + `p Sub[1, 2].divide { |x| x }.map(&:class)
p Sub[1, 2].divide { |x, y| false }.map(&:class)`, "[Sub, Sub]\n[Sub, Sub]"},

		// ...and the OUTER Set of divide is a plain Set on BOTH arities.
		{"the outer Set of divide stays a plain Set",
			setSubPre + `p Sub[1, 2].divide { |x| x }.class
p Sub[1, 2].divide { |x, y| false }.class`, "Set\nSet"},

		// dup carries the class, which is what makes `|` carry it: the prelude
		// writes `n = dup` (prelude.rb:1247).
		{"dup, clone and the set operators keep the subclass",
			setSubPre + `p Sub[1, 2].dup.class
p Sub[1, 2].clone.class
p (Sub[1, 2] | Sub[3]).class
p (Sub[1, 2] & Sub[2]).class
p (Sub[1, 2] - Sub[2]).class`, "Sub\nSub\nSub\nSub\nSub"},

		// ⛔ to_set is the one that does NOT keep it, and the one this change
		// had to fix: `def to_set; self; end` answered Set for every receiver
		// while every Set reported Set — correct BY ACCIDENT. Giving a subclass
		// its own class turned that accident into a divergence.
		{"to_set returns a plain Set for a subclass, and self for a plain Set",
			setSubPre + `s = Set[1, 2]
p s.to_set.equal?(s)
sub = Sub[1, 2]
p sub.to_set.class
p sub.to_set.equal?(sub)`, "true\nSet\nfalse"},

		// The arity gap the one-line to_set also had: this raised
		// ArgumentError (given 1, expected 0).
		{"to_set takes an explicit class",
			setSubPre + `p Set[1, 2].to_set(Sub).class
p Set[1, 2].to_set(Sub).equal?(Set[1, 2])
p Sub[1, 2].to_set(Sub).class`, "Sub\nfalse\nSub"},

		{"a block makes to_set build a new Set",
			setSubPre + `p Set[1, 2].to_set { |x| x }.class`, `Set`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}
