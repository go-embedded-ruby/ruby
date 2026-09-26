// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"
	"strings"
	"testing"
	stdtime "time"
)

// The tests here cover #689: Time.now read the wall clock through a seam typed
// `func() int64`, so every instant it built landed on a whole second and the
// universal Ruby elapsed-time idiom (`t = Time.now; work; Time.now - t`)
// measured 0.0 whatever the work cost.
//
// Time is the one subject where a test must not assert a literal, so nothing
// below compares an instant to a constant. Two complementary shapes are used:
//
//   - pinned: the seam is replaced by a fixed nanosecond-bearing instant and the
//     value Ruby reads back is asserted exactly. Deterministic, but it proves
//     only that the *wrapper* carries nanoseconds — a production seam that
//     truncated would still pass, because pinning replaces it;
//   - live: the real clock runs, and only properties are asserted — range,
//     ordering, and that a non-zero sub-second is observed within a bounded
//     sampling window. This is the leg that witnesses the production seam.
//
// The live leg cannot flake. A single Time.now may legitimately land on a whole
// second, so no single sample is asserted; instead it samples until it sees one
// non-zero sub-second or the budget runs out. On any clock with better than
// one-second resolution that happens on the first or second sample, and the
// budget is only ever spent by the defect itself — a clock that reports whole
// seconds and nothing else, which is precisely what must fail.

// timeNowSampleBudget bounds the live sampling loops. It is generous on purpose:
// it is spent only when the property is false.
const timeNowSampleBudget = 3 * stdtime.Second

// TestTimeNowPinnedSeamCarriesNanoseconds pins the wall-clock seam at an instant
// with a sub-second part and checks Ruby reads that part back through every
// accessor MRI exposes. Whole-second truncation anywhere between the seam and
// the Ruby value fails this outright.
func TestTimeNowPinnedSeamCarriesNanoseconds(t *testing.T) {
	saved := nowWall
	defer func() { nowWall = saved }()
	nowWall = func() stdtime.Time { return stdtime.Unix(1782045296, 123456789).UTC() }

	for _, c := range []struct{ src, want string }{
		{`p Time.now.nsec`, "123456789\n"},
		{`p Time.now.usec`, "123456\n"},
		{`p Time.now.subsec`, "(123456789/1000000000)\n"},
		{`p Time.now.strftime("%N")`, "\"123456789\"\n"},
		{`p Time.now.strftime("%6N")`, "\"123456\"\n"},
		// Time.new with no arguments is Time.now, and must not lose it either.
		{`p Time.new.nsec`, "123456789\n"},
		// The in: keyword takes the zoned path (applyInstantZone), a second
		// construction site that used to be handed a nanosecond of 0.
		{`p Time.now(in: "+02:00").nsec`, "123456789\n"},
		{`p Time.now(in: "+02:00").usec`, "123456\n"},
		{`p Time.new(in: "-03:00").nsec`, "123456789\n"},
		// Two reads of a pinned clock are the same instant, so their difference
		// is exactly zero — the arithmetic does not invent a sub-second either.
		{`p Time.now - Time.now`, "0.0\n"},
		// Date.today has no time of day; DateTime.now does, and keeps it.
		{`require "date"; p DateTime.now.sec_fraction`, "(123456789/1000000000)\n"},
	} {
		if got := runTC(t, c.src); got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
}

// TestTimeNowLiveSubSecondProperties runs the real clock and asserts only
// properties: the sub-second fields stay in range and agree with one another,
// successive reads never go backwards, and a non-zero sub-second is observed
// within the sampling budget.
func TestTimeNowLiveSubSecondProperties(t *testing.T) {
	// Range and internal agreement, on every sample: 0 <= nsec < 1e9,
	// 0 <= usec < 1e6, usec == nsec / 1000, 0 <= subsec < 1.
	const invariants = `
ok = true
2000.times do
  t = Time.now
  ok &&= (0...1_000_000_000).cover?(t.nsec)
  ok &&= (0...1_000_000).cover?(t.usec)
  ok &&= (t.nsec / 1000 == t.usec)
  ok &&= (t.subsec >= 0 && t.subsec < 1)
  ok &&= (t.to_f >= t.to_i && t.to_f < t.to_i + 1)
end
puts ok
`
	if got := runTC(t, invariants); got != "true\n" {
		t.Errorf("Time.now sub-second invariants held = %q, want true", got)
	}

	// Ordering: 2000 consecutive reads, none earlier than the one before it.
	const ordering = `
prev = Time.now
ok = true
2000.times do
  cur = Time.now
  ok &&= (cur >= prev)
  prev = cur
end
puts ok
`
	if got := runTC(t, ordering); got != "true\n" {
		t.Errorf("Time.now ordering held = %q, want true", got)
	}

	// Liveness: sample until a non-zero sub-second appears. A correct clock
	// answers on the first sample or two; only a whole-second clock spends the
	// budget, and that is the defect.
	deadline := stdtime.Now().Add(timeNowSampleBudget)
	samples, sawSubSecond := 0, false
	for stdtime.Now().Before(deadline) {
		samples++
		out := runTC(t, `print Time.now.nsec`)
		n, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil {
			t.Fatalf("Time.now.nsec = %q, not an integer: %v", out, err)
		}
		if n != 0 {
			sawSubSecond = true
			break
		}
	}
	if !sawSubSecond {
		t.Errorf("no non-zero Time.now.nsec in %d samples over %s: the wall clock "+
			"reaches Ruby truncated to whole seconds (#689)", samples, timeNowSampleBudget)
	}
}

// TestTimeNowMeasuresElapsedTime is the idiom from the issue, asserted as a
// property: time spent between two Time.now reads must eventually show up in
// their difference. It sleeps rather than busy-loops so the elapsed interval is
// bounded below by the runtime's own timer, and it asserts only that the
// difference is positive and not absurd — never a duration literal.
func TestTimeNowMeasuresElapsedTime(t *testing.T) {
	const src = `
t0 = Time.now
sleep 0.05
d = Time.now - t0
puts(d > 0.0 && d < 30.0)
`
	if got := runTC(t, src); got != "true\n" {
		t.Errorf("elapsed across sleep 0.05 was positive = %q, want true "+
			"(0.0 is the #689 signature: a measuring instrument reading zero)", got)
	}
}

// TestTimecopFreezeKeepsSubSecond covers the other truncation the same defect
// left behind: Timecop.freeze(t) resolved its Time argument through
// time.Unix(sec, 0), so freezing at an instant carrying a sub-second silently
// moved the clock back to the start of that second.
func TestTimecopFreezeKeepsSubSecond(t *testing.T) {
	withPinnedNow(func() {
		const src = `
require "timecop"
Timecop.freeze(Time.at(1600000000.25))
p Time.now.nsec
# The exact rational, not #to_f: MRI 4.0.5 renders this instant's #to_f as
# 1600000000.2499998, and Time.at(1600000000.25).to_f == 1600000000.25 is false
# there too, so a float expectation here would pin a rendering rather than the
# instant. #to_r is exact and was read off MRI 4.0.5.
p Time.now.to_r
Timecop.return
`
		if got := runTC(t, src); got != "250000000\n(6400000001/4)\n" {
			t.Errorf("Timecop.freeze at a sub-second instant = %q, want the .25 kept", got)
		}
	})
}

// TestTimeMinusTimeIsExact covers the sub-nanosecond leg of Time#-: the
// difference of two Times used to be time.Time.Sub, whose time.Duration is
// nanosecond-granular and drops the sub-nanosecond remainder a Time carries in
// frac. (t + 0.000001) - t then read 9.99e-07 where MRI reads 1.0e-06.
func TestTimeMinusTimeIsExact(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`t = Time.at(1700000000.5); p((t + 0.000001) - t)`, "1.0e-06\n"},
		{`t = Time.at(0); p((t + Rational(1, 3)) - t)`, "0.3333333333333333\n"},
		{`p Time.at(2.5) - Time.at(1.25)`, "1.25\n"},
		{`p Time.at(1.5) - Time.at(1.5)`, "0.0\n"},
		{`p Time.at(1) - Time.at(2)`, "-1.0\n"},
	} {
		if got := runTC(t, c.src); got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
}
