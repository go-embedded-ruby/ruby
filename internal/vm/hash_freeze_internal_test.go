// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestHashFreeze covers Hash#freeze / #frozen? and every in-place Hash mutator
// raising FrozenError on a frozen receiver (even when the operation would make no
// change), while a non-frozen hash mutates as before. Verified against
// ruby 4.0.6.
func TestHashFreeze(t *testing.T) {
	cases := []struct{ src, want string }{
		{`h = { a: 1 }; p h.frozen?; p h.freeze.equal?(h); p h.frozen?`, "false\ntrue\ntrue"},
		{`p({ a: 1 }.frozen?)`, "false"},
		// Non-frozen mutators still work.
		{`h = { a: 1 }; h[:b] = 2; h.merge!(c: 3); h.delete(:a); p h`, `{b: 2, c: 3}`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// Every in-place mutator raises FrozenError on a frozen hash (even a no-op).
	mutators := []string{
		`store(:b, 2)`, `clear`, `merge!(b: 2)`, `update(b: 2)`, `replace(b: 2)`,
		`delete(:a)`, `delete_if { true }`, `reject! { true }`, `keep_if { true }`,
		`select! { true }`, `compact!`, `transform_values! { |v| v }`,
		`transform_keys! { |k| k }`, `default = 9`, `default_proc = ->(h, k) {}`,
		`compare_by_identity`,
	}
	for _, m := range mutators {
		src := `h = { a: 1 }.freeze; p ((h.` + m + `; :no_raise) rescue $!.class)`
		if got := eval(t, src); got != "FrozenError\n" {
			t.Errorf("frozen {a:1}.%s: got=%q want FrozenError", m, got)
		}
	}
	// Element-assignment ([]=) and re-initialize also raise.
	if got := eval(t, `h = { a: 1 }.freeze; p ((h[:b] = 2; :no_raise) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen []=: got=%q want FrozenError", got)
	}
	if got := eval(t, `h = { a: 1 }.freeze; p ((h.send(:initialize); :no_raise) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen initialize: got=%q want FrozenError", got)
	}
	// A frozen empty hash also raises (the check is upfront, not change-gated).
	if got := eval(t, `h = {}.freeze; p ((h.compact!; :no_raise) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen {}.compact!: got=%q want FrozenError", got)
	}
	// Freezing a Hash does not affect Array/String freezing.
	if got := eval(t, `p [1].freeze.frozen?; p "x".freeze.frozen?; p({}.frozen?)`); got != "true\ntrue\nfalse\n" {
		t.Errorf("cross-type freeze: got=%q", got)
	}
}
