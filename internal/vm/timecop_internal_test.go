// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"strings"
	"testing"
)

// TestTimecopRequire checks require "timecop" is a provided feature (true first
// load, false after) and installs the Timecop module.
func TestTimecopRequire(t *testing.T) {
	if got := evalTC(t, `puts require "timecop"; puts require "timecop"; puts Timecop.class`); got != "true\nfalse\nModule\n" {
		t.Errorf("require timecop = %q", got)
	}
}

// TestTimecopFreeze covers freeze at an explicit Time: Time.now / Date.today /
// DateTime.now all report the frozen instant, frozen? is true, and Timecop.return
// restores the real (pinned) clock.
func TestTimecopFreeze(t *testing.T) {
	got := evalTC(t, `
Timecop.freeze(Time.at(1000000000))
puts Timecop.frozen?
puts Time.now.to_i
puts Date.today.to_s
puts DateTime.now.year
Timecop.return
puts Timecop.frozen?
puts Time.now.to_i`)
	want := "true\n1000000000\n2001-09-09\n2001\nfalse\n1782045296\n"
	if got != want {
		t.Errorf("freeze = %q, want %q", got, want)
	}
}

// TestTimecopFreezeBlock covers the block form: time is frozen only inside the
// block, the frozen Time is yielded, and the block's value is returned; the clock
// is restored afterwards.
func TestTimecopFreezeBlock(t *testing.T) {
	got := evalTC(t, `
r = Timecop.freeze(Time.at(1000000000)) { |frz| puts frz.to_i; puts Time.now.to_i; 42 }
puts r
puts Time.now.to_i`)
	want := "1000000000\n1000000000\n42\n1782045296\n"
	if got != want {
		t.Errorf("freeze block = %q, want %q", got, want)
	}
}

// TestTimecopFreezeBlockRaise checks the block form restores the clock even when
// the block raises — the library's deferred pop unwinds the panic raise uses.
func TestTimecopFreezeBlockRaise(t *testing.T) {
	got := evalTC(t, `
begin
  Timecop.freeze(Time.at(1000000000)) { raise "boom" }
rescue => e
  puts e.message
end
puts Timecop.frozen?
puts Time.now.to_i`)
	want := "boom\nfalse\n1782045296\n"
	if got != want {
		t.Errorf("freeze block raise = %q, want %q", got, want)
	}
}

// TestTimecopFreezeNoArg covers freeze with no argument (freeze at the current
// now) and freeze with an Integer / Float second-offset from now.
func TestTimecopFreezeNoArg(t *testing.T) {
	got := evalTC(t, `
Timecop.freeze
puts Time.now.to_i
Timecop.return
Timecop.freeze(10)
puts Time.now.to_i
Timecop.return
Timecop.freeze(2.0)
puts Time.now.to_i`)
	want := "1782045296\n1782045306\n1782045298\n"
	if got != want {
		t.Errorf("freeze no-arg/offset = %q, want %q", got, want)
	}
}

// TestTimecopFreezeDate covers freezing at a Date value (its midnight instant).
func TestTimecopFreezeDate(t *testing.T) {
	got := evalTC(t, `
Timecop.freeze(Date.new(2000, 1, 1))
puts Time.now.to_i
puts Date.today.to_s`)
	want := "946684800\n2000-01-01\n"
	if got != want {
		t.Errorf("freeze date = %q, want %q", got, want)
	}
}

// TestTimecopTravel covers travel (jump then keep ticking; with the clock pinned,
// no real time elapses so Time.now stays at the target) in both forms.
func TestTimecopTravel(t *testing.T) {
	got := evalTC(t, `
Timecop.travel(Time.at(1500000000))
puts Timecop.travelled?
puts Time.now.to_i
Timecop.return
r = Timecop.travel(Time.at(1600000000)) { puts Time.now.to_i; :done }
puts r
puts Time.now.to_i`)
	want := "true\n1500000000\n1600000000\ndone\n1782045296\n"
	if got != want {
		t.Errorf("travel = %q, want %q", got, want)
	}
}

// TestTimecopScale covers scale (with the clock pinned, elapsed real time is zero
// so scaled Time.now equals the target) in both forms, plus scale with no time
// argument (scale from the current now).
func TestTimecopScale(t *testing.T) {
	got := evalTC(t, `
Timecop.scale(4, Time.at(1700000000))
puts Timecop.scaled?
puts Time.now.to_i
Timecop.return
r = Timecop.scale(2, Time.at(1710000000)) { puts Time.now.to_i; 7 }
puts r
Timecop.scale(3)
puts Time.now.to_i`)
	want := "true\n1700000000\n1710000000\n7\n1782045296\n"
	if got != want {
		t.Errorf("scale = %q, want %q", got, want)
	}
}

// TestTimecopReturnBlock covers Timecop.return's block form: real time inside the
// block, the prior mock stack restored afterwards.
func TestTimecopReturnBlock(t *testing.T) {
	got := evalTC(t, `
Timecop.freeze(Time.at(1000000000))
r = Timecop.return { puts Time.now.to_i; :real }
puts r
puts Time.now.to_i`)
	want := "1782045296\nreal\n1000000000\n"
	if got != want {
		t.Errorf("return block = %q, want %q", got, want)
	}
}

// TestTimecopBaseline covers baseline= (returns its argument) and
// return_to_baseline (unwinds nested frames down to the baseline instant).
func TestTimecopBaseline(t *testing.T) {
	got := evalTC(t, `
r = (Timecop.baseline = Time.at(1000000000))
puts r.to_i
Timecop.travel(Time.at(1600000000))
puts Time.now.to_i
b = Timecop.return_to_baseline
puts b.to_i
puts Time.now.to_i`)
	want := "1000000000\n1600000000\n1000000000\n1000000000\n"
	if got != want {
		t.Errorf("baseline = %q, want %q", got, want)
	}
}

// TestTimecopTypeErrors covers the two TypeError branches: a non-Time freeze
// argument and a non-numeric scale factor.
func TestTimecopTypeErrors(t *testing.T) {
	got := evalTC(t, `
begin
  Timecop.freeze("nope")
rescue TypeError => e
  puts e.class
end
begin
  Timecop.scale("x")
rescue TypeError => e
  puts e.class
end`)
	if !strings.Contains(got, "TypeError\nTypeError\n") {
		t.Errorf("type errors = %q", got)
	}
}

// TestTimecopUnaffected checks a program that never touches Timecop still reads
// the real (pinned) clock — the mock-clock hook leaves default behaviour intact.
func TestTimecopUnaffected(t *testing.T) {
	if got := evalTC(t, `puts Time.now.to_i; puts Timecop.frozen?`); got != "1782045296\nfalse\n" {
		t.Errorf("unaffected = %q", got)
	}
}
