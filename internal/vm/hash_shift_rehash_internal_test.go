// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestHashShift covers Hash#shift: it removes and returns the first inserted pair
// as [key, value], returns nil for an empty hash (ruby 3.4+, including one with a
// default/default_proc), and raises FrozenError on a frozen receiver. Verified
// against ruby 4.0.6.
func TestHashShift(t *testing.T) {
	cases := []struct{ src, want string }{
		{`h = { a: 1, b: 2 }; p h.shift; p h`, "[:a, 1]\n{b: 2}"},
		{`h = { a: 1 }; p h.shift; p h.shift; p h`, "[:a, 1]\nnil\n{}"},
		{`p({}.shift)`, "nil"},
		// An empty hash with a default / default proc still shifts to nil (not the default).
		{`p Hash.new(5).shift`, "nil"},
		{`p Hash.new { |*a| a }.shift`, "nil"},
		// String keys shift by content, preserving order.
		{`h = { "x" => 1, "y" => 2 }; p h.shift; p h`, `["x", 1]` + "\n" + `{"y" => 2}`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	if got := eval(t, `p (({ a: 1 }.freeze.shift; :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen shift: got=%q want FrozenError", got)
	}
}

// TestHashRehash covers Hash#rehash across every value-recovery path in
// storedValue: string keys, immediate keys, content-addressed collection keys, a
// key with a custom #hash from construction, and a plain object that gains a
// custom #hash after insertion (the identity-stored path). It also verifies the
// #hash-changed lookup recovery, #eql? duplicate collapse, self return and the
// frozen check. Verified against ruby 4.0.6.
func TestHashRehash(t *testing.T) {
	cases := []struct{ src, want string }{
		// Returns self.
		{`h = { a: 1 }; p h.rehash.equal?(h)`, "true"},
		// Every key kind survives a rehash (storedValue branches).
		{`h = { "a" => 1, "b" => 2 }; h.rehash; p h["a"]; p h["b"]`, "1\n2"},
		{`h = { 1 => :x, 2 => :y }; h.rehash; p h[1]; p h[2]`, ":x\n:y"},
		{`h = { [1] => :p, [2] => :q }; h.rehash; p h[[1]]; p h[[2]]`, ":p\n:q"},
		// A key whose #hash changed is found again only after rehash.
		{`k1 = Object.new; k2 = Object.new
		  def k1.hash; 0; end
		  def k2.hash; 1; end
		  h = {}; h[k1] = :v1; h[k2] = :v2
		  def k1.hash; 1; end
		  a = h.key?(k1); h.rehash; p [a, h.key?(k1), h[k1]]`, "[false, true, :v1]"},
		// A plain object that gains a custom #hash after insertion (identity path).
		{`o = Object.new; h = {}; h[o] = :z; def o.hash; 42; end; h.rehash; p h[o]`, ":z"},
		// Two keys that have become #eql? collapse to the first inserted.
		{`a = [1, 2]; b = [1]; h = {}; h[a] = true; h[b] = true; b << 2
		  h.rehash; p h.size; p h.keys`, "1\n[[1, 2]]"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	if got := eval(t, `p (({ a: 1 }.freeze.rehash; :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen rehash: got=%q want FrozenError", got)
	}
	// Rehash on an empty hash is a no-op returning self.
	if got := eval(t, `h = {}; p h.rehash.equal?(h); p h`); got != "true\n{}\n" {
		t.Errorf("empty rehash: got=%q", got)
	}
}
