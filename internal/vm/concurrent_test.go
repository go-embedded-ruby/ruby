// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestConcurrentCollections covers the Concurrent::Hash / Map / Array aliases of
// the core collections (preserving the full core API) and ThreadLocalVar's
// value / value= with and without a default.
func TestConcurrentCollections(t *testing.T) {
	cases := []struct{ src, want string }{
		// the collections alias the core classes, so the full API works.
		{`require "concurrent"
h = Concurrent::Hash.new
h["a"] = 1
p h["a"]`, "1"},
		// Hash.new with a default block is preserved (Puppet relies on it).
		{`require "concurrent"
h = Concurrent::Hash.new { |hash, k| hash[k] = k.to_s }
p h[:z]`, `"z"`},
		{`require "concurrent"
m = Concurrent::Map.new
m[:x] = 2
p m[:x]`, "2"},
		{`require "concurrent"
a = Concurrent::Array.new
a << 3 << 4
p a`, "[3, 4]"},
		// ThreadLocalVar(default): value reads the default until first written.
		{`require "concurrent"
t = Concurrent::ThreadLocalVar.new(10)
p t.value
t.value = 20
p t.value`, "10\n20"},
		// no default -> value is nil.
		{`require "concurrent"
p Concurrent::ThreadLocalVar.new.value`, "nil"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
