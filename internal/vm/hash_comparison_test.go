// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestHashComparisonAndToProc covers Hash#<, Hash#<=, Hash#>, Hash#>= (subset /
// superset by key-value pairs) and Hash#to_proc. Expectations were taken from
// MRI Ruby 3.4/4.0 (ruby/spec core/hash) and re-verified against the real ruby
// binary while implementing.
func TestHashComparisonAndToProc(t *testing.T) {
	cases := []struct{ src, want string }{
		// <= is subset-by-pair: true when every pair of the receiver is in other.
		{`p({a: 1} <= {a: 1, b: 2})`, "true\n"},
		{`p({a: 1, b: 2} <= {a: 1})`, "false\n"},
		{`p({a: 1} <= {a: 1})`, "true\n"},
		// a pair present in other but with a different value is not contained.
		{`p({a: 2} <= {a: 1, b: 2})`, "false\n"},
		// the empty hash is a subset of everything, including itself.
		{`p({} <= {})`, "true\n"},
		{`p({} <= {a: 1})`, "true\n"},
		// value comparison uses Ruby == (rb_equal), so 1 and 1.0 match.
		{`p({a: 1.0} <= {a: 1})`, "true\n"},

		// < is proper subset: <= and strictly fewer pairs.
		{`p({a: 1} < {a: 1})`, "false\n"},
		{`p({a: 1} < {a: 1, b: 2})`, "true\n"},
		{`p({} < {})`, "false\n"},
		{`p({} < {a: 1})`, "true\n"},
		{`p({a: 2} < {a: 1, b: 2})`, "false\n"},

		// >= is superset-by-pair (symmetric to <=).
		{`p({a: 1} >= {a: 1})`, "true\n"},
		{`p({a: 1, b: 2} >= {a: 1})`, "true\n"},
		{`p({a: 1} >= {a: 1, b: 2})`, "false\n"},
		{`p({a: 1} >= {})`, "true\n"},

		// > is proper superset.
		{`p({a: 1, b: 2} > {a: 1})`, "true\n"},
		{`p({a: 1} > {a: 1})`, "false\n"},
		{`p({a: 1} > {})`, "true\n"},
		{`p({} > {})`, "false\n"},

		// A non-Hash argument that defines #to_hash is coerced.
		{`class F1; def to_hash; {a: 1, b: 2}; end; end; p({a: 1} <= F1.new)`, "true\n"},
		{`class F2; def to_hash; {a: 1}; end; end; p({a: 1, b: 2} > F2.new)`, "true\n"},

		// to_proc: a unary lambda mapping key -> h[key].
		{`p({a: 1, b: 2}.to_proc.lambda?)`, "true\n"},
		{`p({a: 1, b: 2}.to_proc.arity)`, "1\n"},
		{`p({a: 1, b: 2}.to_proc.call(:a))`, "1\n"},
		{`p({a: 1, b: 2}.to_proc.call(:missing))`, "nil\n"},
		{`p(%w[a b].map(&{"a" => 1, "b" => 2}.to_proc))`, "[1, 2]\n"},
		// lookup goes through #[], so a default value is honored.
		{`h = Hash.new(99); h[:a] = 1; pr = h.to_proc; p(pr.call(:a)); p(pr.call(:x))`, "1\n99\n"},
		// and so is a default proc.
		{`h = Hash.new { |hh, k| k.to_s * 2 }; p(h.to_proc.call("x"))`, "\"xx\"\n"},
		// the hash is captured by reference: later mutations are visible.
		{`h = {x: 1}; pr = h.to_proc; h[:y] = 9; p(pr.call(:y))`, "9\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// A non-Hash argument without #to_hash raises TypeError for every operator.
	for _, op := range []string{"<", "<=", ">", ">="} {
		src := `{a: 1} ` + op + ` 3`
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "no implicit conversion of Integer into Hash") {
			t.Errorf("src=%q: got err=%v, want TypeError no-implicit-conversion", src, err)
		}
		src = `{a: 1} ` + op + ` [1, 2]`
		err = runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "no implicit conversion of Array into Hash") {
			t.Errorf("src=%q: got err=%v, want TypeError no-implicit-conversion", src, err)
		}
	}

	// to_proc yields an arity-strict lambda: wrong argument counts raise.
	for _, src := range []string{`{a: 1}.to_proc.call(1, 2)`, `{a: 1}.to_proc.call`} {
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "wrong number of arguments") {
			t.Errorf("src=%q: got err=%v, want ArgumentError", src, err)
		}
	}
}
