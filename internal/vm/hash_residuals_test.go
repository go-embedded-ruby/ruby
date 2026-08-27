// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestHashFetchValues covers Hash#fetch_values: the values are returned in the
// order requested, a missing key falls back to the block (called with the key)
// or raises KeyError, and no keys yields an empty Array. Expectations match MRI
// Ruby 4.0 (ruby/spec core/hash/fetch_values).
func TestHashFetchValues(t *testing.T) {
	cases := []struct{ src, want string }{
		// Matched keys: values in the requested order (not insertion order).
		{`p({a: 1, b: 2, c: 3}.fetch_values(:a))`, "[1]\n"},
		{`p({a: 1, b: 2, c: 3}.fetch_values(:a, :c))`, "[1, 3]\n"},
		{`p({a: 1, b: 2, c: 3}.fetch_values(:c, :a))`, "[3, 1]\n"},
		// No keys -> empty Array.
		{`p({a: 1}.fetch_values)`, "[]\n"},
		// A missing key uses the block, called with the key; matched keys keep
		// their stored value.
		{`p({a: 1}.fetch_values(:z) { |k| "no#{k}" })`, "[\"noz\"]\n"},
		{`p({a: 1}.fetch_values(:a, :z) { |k| "no#{k}" })`, "[1, \"noz\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A missing key with no block raises KeyError with MRI's "key not found: %p".
	if err := runErr(t, `{a: 1}.fetch_values(:z)`); err == nil || !strings.Contains(err.Error(), "key not found: :z") {
		t.Errorf("fetch_values(:z) err=%v, want KeyError \"key not found: :z\"", err)
	}
}

// TestHashReplace covers Hash#replace: the receiver's contents are replaced with
// the argument's (coerced via #to_hash), it returns self, and it transfers the
// argument's default value, default proc and compare_by_identity flag while
// dropping the receiver's own. Expectations match MRI Ruby 4.0
// (ruby/spec core/hash/replace).
func TestHashReplace(t *testing.T) {
	cases := []struct{ src, want string }{
		// Replaces contents and returns self.
		{`h = {a: 1, b: 2}; p(h.replace(c: -1, d: -2).equal?(h)); p h`, "true\n{c: -1, d: -2}\n"},
		// The receiver's own default is dropped.
		{`p(Hash.new(1).replace(b: 2).default)`, "nil\n"},
		// The argument's default value transfers.
		{`p(({a: 1}).replace(Hash.new(1)).default)`, "1\n"},
		// The receiver's own default proc is dropped.
		{`pr = proc { |h, k| [] }; p(Hash.new(&pr).replace(b: 2).default_proc)`, "nil\n"},
		// The argument's default proc transfers (same proc object).
		{`pr = proc { |h, k| [] }; p(({a: 1}).replace(Hash.new(&pr)).default_proc == pr)`, "true\n"},
		// compare_by_identity transfers from the argument.
		{`h = {a: 1}; h.replace({b: 2}.compare_by_identity); p(h.compare_by_identity?)`, "true\n"},
		// The receiver's own compare_by_identity flag is dropped.
		{`h = {a: 1}.compare_by_identity; h.replace(b: 2); p(h.compare_by_identity?)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A non-Hash argument without #to_hash raises TypeError.
	if err := runErr(t, `({}).replace(5)`); err == nil || !strings.Contains(err.Error(), "no implicit conversion of Integer into Hash") {
		t.Errorf("replace(5) err=%v, want a TypeError", err)
	}
}

// TestHashCompareByIdentity covers Hash#compare_by_identity and
// Hash#compare_by_identity?: after the switch, keys compare by object identity
// (distinct objects with equal content are distinct keys) while immediate values
// (Symbol/Integer) still coincide; existing entries are rehashed so they stay
// reachable; the flag persists over dup/clone; and Hash#== / #eql? treat two
// hashes differing only by the flag as unequal (unless both empty). Expectations
// match MRI Ruby 4.0 (ruby/spec core/hash/compare_by_identity).
func TestHashCompareByIdentity(t *testing.T) {
	cases := []struct{ src, want string }{
		// Off by default; on after the call.
		{`p({}.compare_by_identity?)`, "false\n"},
		{`h = {}; h.compare_by_identity; p(h.compare_by_identity?)`, "true\n"},
		// Returns self.
		{`h = {}; p(h.compare_by_identity.equal?(h))`, "true\n"},
		// Distinct String objects become distinct keys.
		{`h = {}.compare_by_identity; a = "k"; b = "k".dup; h[a] = 1; h[b] = 2; p([h.size, h[a], h["k".dup]])`, "[2, 1, nil]\n"},
		// A Symbol is a singleton, so it stays a single key.
		{`h = {}.compare_by_identity; h[:s] = 1; h[:s] = 2; p([h.size, h[:s]])`, "[1, 2]\n"},
		// Integer keys (singletons) survive the rehash of a populated hash.
		{`h = {}; (1..3).each { |k| h[k] = k }; h.compare_by_identity; p(h[2])`, "2\n"},
		// An Array key stored before the switch is rehashed by identity: a fresh
		// equal Array no longer matches.
		{`h = {}; h[[1]] = :a; h.compare_by_identity; p([h[[1].dup], h.size])`, "[nil, 1]\n"},
		// Calling it again on an identity hash is a no-op that returns self.
		{`h = {}.compare_by_identity; h[:foo] = :bar; p(h.compare_by_identity.equal?(h) && h[:foo] == :bar)`, "true\n"},
		// The flag persists over #dup (contents and size preserved).
		{`x = {}.compare_by_identity; x["foo".dup] = :a; x["foo".dup] = :b; d = x.dup; p([d.compare_by_identity?, d.size, d == x])`, "[true, 2, true]\n"},
		// ... and over #clone.
		{`x = {}.compare_by_identity; x["foo".dup] = :a; c = x.clone; p([c.compare_by_identity?, c.size])`, "[true, 1]\n"},
		// String keys are stored as-is (not copied): keys.first is the same object.
		{`foo = "foo"; h = {}.compare_by_identity; h[foo] = true; h[foo] = true; p([h.size, h.keys.first.equal?(foo)])`, "[1, true]\n"},
		// Deleting an identity-keyed entry works through the general path.
		{`h = {}.compare_by_identity; k = "a"; h[k] = 1; h.delete(k); p(h.size)`, "0\n"},
		// Inspect of an identity hash renders its string key content.
		{`h = {}.compare_by_identity; h["x".dup] = 1; puts h.inspect`, "{\"x\" => 1}\n"},
		// == : two empty hashes are equal regardless of the flag.
		{`p({} == {}.compare_by_identity)`, "true\n"},
		// == : two non-empty hashes differing only by the flag are unequal.
		{`p(({1 => 2}) == ({1 => 2}.compare_by_identity))`, "false\n"},
		// eql? follows the same identity-flag rule.
		{`p(({1 => 2}).eql?({1 => 2}.compare_by_identity))`, "false\n"},
		// eql? : matching flags and content are eql?.
		{`p(({1 => 2}.compare_by_identity).eql?({1 => 2}.compare_by_identity))`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestHashKeyAndRuby2Keywords covers Hash#key (reverse value lookup) and the
// Hash.ruby2_keywords_hash / ruby2_keywords_hash? pair, asserted against MRI
// Ruby 4.0.6.
func TestHashKeyAndRuby2Keywords(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p({a: 1, b: 2, c: 1}.key(1))`, ":a\n"},
		{`p({a: 1}.key(9))`, "nil\n"},
		{`p({"x" => 5}.key(5))`, "\"x\"\n"},
		{`kh = Hash.ruby2_keywords_hash({a: 1}); p [kh, Hash.ruby2_keywords_hash?(kh)]`, "[{a: 1}, true]\n"},
		{`p Hash.ruby2_keywords_hash?({a: 1})`, "false\n"},
		// The original hash is unchanged; only the returned copy carries the flag.
		{`h = {a: 1}; Hash.ruby2_keywords_hash(h); p Hash.ruby2_keywords_hash?(h)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	errs := []struct{ src, want string }{
		{`Hash.ruby2_keywords_hash?(5)`, "wrong argument type Integer (expected Hash)"},
		{`Hash.ruby2_keywords_hash("x")`, "wrong argument type String (expected Hash)"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
