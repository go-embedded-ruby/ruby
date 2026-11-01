// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestBigDecimalAndDateRenderThemselves covers three bindings that sat at
// **0.0%** in both recorded below-100 lists -- `(*BigDecimal).Inspect`,
// `(*Date).Inspect` and `(*Date).ToS`.
//
// #704 named the first two weeks ago and predicted exactly this: while the gate
// compared a total that rounded to 100.0%, a function at zero was invisible.
// The gate compares the SET now, so they are recorded rather than hidden -- and
// recorded is not covered.
//
// A function at 0.0% is a question: dead, or untested? These are reachable from
// one line of Ruby each, so they are untested. The expectations are MRI 4.0.7's
// output, measured on the same host, which is the part that makes this a
// conformance test rather than a coverage ritual: it would be trivial to reach
// these lines with an assertion that accepted anything they printed.
func TestBigDecimalAndDateRenderThemselves(t *testing.T) {
	checkCases(t, []runCase{
		// BigDecimal renders in MRI's scientific form, not as "1.5" -- `p` and
		// `puts` agree because Inspect and ToS both defer to the library.
		{`require "bigdecimal"
p BigDecimal("1.5")`, "0.15e1\n"},
		{`require "bigdecimal"
puts BigDecimal("1.5")`, "0.15e1\n"},
		{`require "bigdecimal"
puts BigDecimal("1.5").to_s`, "0.15e1\n"},
		// A value whose scientific form is not its decimal one, so a binding that
		// returned the plain digits would fail here and not above.
		{`require "bigdecimal"
p BigDecimal("1234.5")`, "0.12345e4\n"},
		{`require "bigdecimal"
p BigDecimal("-0.001")`, "-0.1e-2\n"},

		// Date#to_s is ISO; Date#inspect is MRI's bracketed triple, which is the
		// astronomical day count, seconds of the UTC day, leftover nanoseconds,
		// the offset and the calendar-reform start.
		{`require "date"
puts Date.new(2026, 10, 12)`, "2026-10-12\n"},
		{`require "date"
puts Date.new(2026, 10, 12).to_s`, "2026-10-12\n"},
		{`require "date"
p Date.new(2026, 10, 12)`, "#<Date: 2026-10-12 ((2461326j,0s,0n),+0s,2299161j)>\n"},
		// A second date, so the day count is not a constant the binding could
		// return regardless of its argument.
		{`require "date"
p Date.new(2000, 1, 1)`, "#<Date: 2000-01-01 ((2451545j,0s,0n),+0s,2299161j)>\n"},
	})
}
