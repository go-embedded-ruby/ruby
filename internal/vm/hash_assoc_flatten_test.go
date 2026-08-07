// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestHashAssocFlattenCompact covers the newly ported Hash#assoc, Hash#rassoc,
// Hash#flatten, Hash#compact and Hash#compact!. Expectations were taken from
// MRI Ruby 3.4 (ruby/spec core/hash).
func TestHashAssocFlattenCompact(t *testing.T) {
	cases := []struct{ src, want string }{
		// assoc: first pair whose key is == to the argument, else nil; uses Ruby
		// == so an Integer argument matches a Float key of equal value.
		{`p({apple: :green, grape: :green, banana: :yellow}.assoc(:banana))`, "[:banana, :yellow]\n"},
		{`p({a: 1}.assoc(:missing))`, "nil\n"},
		{`h = {}; h[1.0] = :v; p(h.assoc(1))`, "[1.0, :v]\n"},
		// assoc never triggers the default.
		{`p(Hash.new(42).merge!(foo: :bar).assoc(42))`, "nil\n"},
		// rassoc: first pair whose value is == to the argument, else nil.
		{`p({apple: :green, orange: :orange}.rassoc(:orange))`, "[:orange, :orange]\n"},
		{`p({apple: :green, grape: :green}.rassoc(:green))`, "[:apple, :green]\n"},
		{`p({a: 1}.rassoc(:nope))`, "nil\n"},
		{`h = {}; h[:k] = 1.0; p(h.rassoc(1))`, "[:k, 1.0]\n"},
		// flatten: default depth 1 leaves Array values intact; empty hash -> [].
		{`p({plato: :greek, wittgenstein: [:austrian, :british]}.flatten)`, "[:plato, :greek, :wittgenstein, [:austrian, :british]]\n"},
		{`p({}.flatten)`, "[]\n"},
		// flatten(n>=2) recurses n further levels into Array values.
		{`p({a: [1, 2]}.flatten(2))`, "[:a, 1, 2]\n"},
		{`p({a: [[1, 2], [3, 4]]}.flatten(2))`, "[:a, [1, 2], [3, 4]]\n"},
		// flatten(0) removes nothing (pairs stay as arrays).
		{`p({a: 1}.flatten(0))`, "[[:a, 1]]\n"},
		// compact: copy without nil-valued pairs; source is untouched.
		{`h = {truthy: true, none: nil, keep: 3}; p(h.compact); p h`, "{truthy: true, keep: 3}\n{truthy: true, none: nil, keep: 3}\n"},
		// compact preserves the default value and the default proc.
		{`h = Hash.new(1); h[:a] = 1; p(h.compact.default)`, "1\n"},
		{`pr = proc { |h, k| 5 }; h = Hash.new(&pr); p(h.compact.default_proc.class)`, "Proc\n"},
		// compact! removes nil-valued pairs in place, returning self.
		{`h = {a: 1, b: nil, c: 3}; p(h.compact!.equal?(h)); p h`, "true\n{a: 1, c: 3}\n"},
		// compact! returns nil when nothing was removed.
		{`p({a: 1, b: 2}.compact!)`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// flatten with a non-Integer depth raises TypeError.
	if err := runErr(t, `{a: 1}.flatten(Object.new)`); err == nil || !strings.Contains(err.Error(), "into Integer") {
		t.Errorf("flatten(Object.new) err=%v, want a TypeError", err)
	}
}
