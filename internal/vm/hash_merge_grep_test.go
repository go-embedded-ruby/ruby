package vm_test

import (
	"strings"
	"testing"
)

// TestHashMergeExceptGrep covers the fixed Hash#except (was a no-op) and
// Hash#merge/merge! conflict block + variadic forms (the block was ignored,
// breaking deep_merge), plus Hash#transform_values!/transform_keys! and
// Enumerable#grep/grep_v. Asserted against MRI Ruby 4.0.5.
func TestHashMergeExceptGrep(t *testing.T) {
	cases := []struct{ src, want string }{
		// except removes the named keys (by value, not object identity).
		{`p({"a" => 1, "b" => 2, "c" => 3}.except("a", "b"))`, "{\"c\" => 3}\n"},
		{`p({a: 1, b: 2}.except(:a))`, "{b: 2}\n"},
		// merge: conflict block, several hashes, plain merge, and a deep_merge build.
		{`p({a: 1, b: 2}.merge({b: 20, c: 3}) { |k, o, n| o + n })`, "{a: 1, b: 22, c: 3}\n"},
		{`p({a: 1}.merge({b: 2}, {c: 3}))`, "{a: 1, b: 2, c: 3}\n"},
		{`p({a: 1, b: 2}.merge({c: 3}))`, "{a: 1, b: 2, c: 3}\n"},
		{`p({x: {y: 1, z: 2}}.merge({x: {y: 9}}) { |k, o, n| o.merge(n) })`, "{x: {y: 9, z: 2}}\n"},
		// merge! / update mutate in place (with the conflict block).
		{`h = {a: 1, b: 2}; h.merge!({b: 9}) { |k, o, n| o + n }; p h`, "{a: 1, b: 11}\n"},
		{`h = {a: 1}; h.update({b: 2}); p h`, "{a: 1, b: 2}\n"},
		// transform_values! / transform_keys! mutate in place; no block -> Enumerator.
		{`p({a: 1, b: 2}.transform_values! { |v| v * 10 })`, "{a: 10, b: 20}\n"},
		{`h = {a: 1, b: 2}; h.transform_keys!(&:to_s); p h`, "{\"a\" => 1, \"b\" => 2}\n"},
		{`p [{a: 1}.transform_values!.class, {a: 1}.transform_keys!.class]`, "[Enumerator, Enumerator]\n"},
		// grep / grep_v: pattern === element, with an optional mapping block.
		{`p [[1, "a", 2, "b"].grep(Integer), [1, 2, 3, 4].grep(2..3), %w[foo bar baz].grep(/ba/)]`, "[[1, 2], [2, 3], [\"bar\", \"baz\"]]\n"},
		{`p [[1, "a", 2].grep_v(Integer), [1, 2, 3].grep(Integer) { |x| x * 10 }]`, "[[\"a\"], [10, 20, 30]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// merge with a non-Hash argument raises TypeError.
	if err := runErr(t, `{a: 1}.merge(5)`); err == nil || !strings.Contains(err.Error(), "into Hash") {
		t.Errorf("merge(5) err=%v, want a TypeError", err)
	}
}
