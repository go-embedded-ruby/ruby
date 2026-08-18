// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestArrayFreeze covers Array#freeze / #frozen? and every in-place Array mutator
// raising FrozenError on a frozen receiver (even when the operation would make no
// change), while a non-frozen array mutates as before. Verified against
// ruby 4.0.6.
func TestArrayFreeze(t *testing.T) {
	// freeze marks the array; frozen? reports it; a fresh array is not frozen.
	cases := []struct{ src, want string }{
		{`a = [1, 2, 3]; p a.frozen?; p a.freeze.equal?(a); p a.frozen?`, "false\ntrue\ntrue"},
		{`p [1, 2, 3].frozen?`, "false"},
		// Non-frozen mutators still work.
		{`a = [1, 2, 3]; a.push(4); a.map! { |x| x * 2 }; a.reject! { |x| x > 6 }; p a`, `[2, 4, 6]`},
		{`a = [1, 2, 3]; a.fill(0); p a`, `[0, 0, 0]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// Every in-place mutator raises FrozenError on a frozen array (even a no-op).
	mutators := []string{
		`push(4)`, `<< 4`, `pop`, `shift`, `unshift(0)`, `insert(0, 9)`, `delete(1)`,
		`concat([9])`, `clear`, `replace([9])`, `fill(9)`, `map! { |x| x }`,
		`reverse!`, `sort!`, `select! { true }`, `reject! { false }`, `compact!`,
		`uniq!`, `flatten!`,
	}
	for _, m := range mutators {
		src := `a = [1, 2, 3].freeze; p ((a.` + m + `; :no_raise) rescue $!.class)`
		if got := eval(t, src); got != "FrozenError\n" {
			t.Errorf("frozen [1,2,3].%s: got=%q want FrozenError", m, got)
		}
	}
	// Element-assignment (the []= operator) also raises on a frozen array.
	if got := eval(t, `a = [1, 2, 3].freeze; p ((a[0] = 9; :no_raise) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen []=: got=%q want FrozenError", got)
	}
	// A frozen empty array also raises (the check is upfront, not change-gated).
	if got := eval(t, `a = [].freeze; p ((a.map! { |x| x }; :no_raise) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen [].map!: got=%q want FrozenError", got)
	}
	// Re-initialising a frozen array raises; Array.new (a fresh array) does not.
	if got := eval(t, `p Array.new(3, 0)`); got != "[0, 0, 0]\n" {
		t.Errorf("Array.new: got=%q", got)
	}
	if got := eval(t, `a = [1, 2].freeze; p ((a.send(:initialize, 3); :no_raise) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen initialize: got=%q want FrozenError", got)
	}
}
