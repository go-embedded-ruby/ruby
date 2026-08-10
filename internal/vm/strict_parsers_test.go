// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestStrictTimeParsers covers the strict "require 'time'" class-method parsers
// Time.iso8601 / .xmlschema / .rfc2822 / .rfc822 / .httpdate: each accepts only
// its own wire format (round-tripping through the matching instance formatter)
// and raises ArgumentError with MRI's exact message on anything else. The fixed
// instant 2026-06-21T12:34:56Z = 1782045296; the +02:00 wall clock of the same
// second is 1782038096.
func TestStrictTimeParsers(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// iso8601 / xmlschema: the "Z" UTC instant, a fixed offset (round-tripped),
		// a fractional second (round-tripped at 3 digits) and the zone-less form
		// (resolved to UTC here — the one deterministic divergence from MRI's local).
		{`require "time"; p Time.iso8601("2026-06-21T12:34:56Z").to_i`, "1782045296\n"},
		{`require "time"; p Time.iso8601("2026-06-21T12:34:56+02:00").iso8601`, "\"2026-06-21T12:34:56+02:00\"\n"},
		{`require "time"; p Time.iso8601("2026-06-21T12:34:56.789+02:00").iso8601(3)`, "\"2026-06-21T12:34:56.789+02:00\"\n"},
		{`require "time"; p Time.iso8601("2026-06-21T12:34:56").to_i`, "1782045296\n"},
		{`require "time"; p Time.xmlschema("2026-06-21T12:34:56Z").xmlschema`, "\"2026-06-21T12:34:56Z\"\n"},
		{`require "time"; p Time.iso8601("2026-06-21T12:34:56Z").class`, "Time\n"},
		// rfc2822 / rfc822: a fixed-offset mail date (round-tripped) and the "-0000"
		// unknown-zone form (which denotes UTC).
		{`require "time"; p Time.rfc2822("Sun, 21 Jun 2026 12:34:56 +0200").to_i`, "1782038096\n"},
		{`require "time"; p Time.rfc2822("Sun, 21 Jun 2026 12:34:56 +0200").rfc2822`, "\"Sun, 21 Jun 2026 12:34:56 +0200\"\n"},
		{`require "time"; p Time.rfc822("Sun, 21 Jun 2026 12:34:56 -0000").to_i`, "1782045296\n"},
		// httpdate: RFC 1123 (round-tripped), the obsolete RFC 850 and asctime forms.
		{`require "time"; p Time.httpdate("Sun, 21 Jun 2026 12:34:56 GMT").to_i`, "1782045296\n"},
		{`require "time"; p Time.httpdate("Sun, 21 Jun 2026 12:34:56 GMT").httpdate`, "\"Sun, 21 Jun 2026 12:34:56 GMT\"\n"},
		{`require "time"; p Time.httpdate("Sunday, 21-Jun-26 12:34:56 GMT").to_i`, "1782045296\n"},
		{`require "time"; p Time.httpdate("Sun Jun 21 12:34:56 2026").to_i`, "1782045296\n"},
		// Malformed input → ArgumentError with the exact MRI message per format.
		{`require "time"; begin; Time.iso8601("nope"); rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"ArgumentError\", \"invalid xmlschema format: \\\"nope\\\"\"]\n"},
		{`require "time"; begin; Time.xmlschema("2026-06-21 12:34"); rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"ArgumentError\", \"invalid xmlschema format: \\\"2026-06-21 12:34\\\"\"]\n"},
		{`require "time"; begin; Time.rfc2822("garbage"); rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"ArgumentError\", \"not RFC 2822 compliant date: \\\"garbage\\\"\"]\n"},
		{`require "time"; begin; Time.httpdate("2026-06-21T12:34:56Z"); rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"ArgumentError\", \"not RFC 2616 compliant date: \\\"2026-06-21T12:34:56Z\\\"\"]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestStrictDateParsers covers the strict Date / DateTime class-method parsers
// (iso8601 / xmlschema / rfc3339 / rfc2822 / rfc822 / httpdate). A Date keeps
// only the calendar day; a DateTime keeps the wall clock and offset; each
// round-trips through the matching instance formatter and raises Date::Error
// "invalid date" (MRI's own class and message) on a malformed string.
func TestStrictDateParsers(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// Date.iso8601 / .xmlschema accept a bare day (dashed or compact) and a full
		// datetime (dropping the time), always returning a Date.
		{`require "date"; p Date.iso8601("2026-06-21").to_s`, "\"2026-06-21\"\n"},
		{`require "date"; p Date.iso8601("2026-06-21").class`, "Date\n"},
		{`require "date"; p Date.iso8601("2026-06-21T12:34:56+02:00").to_s`, "\"2026-06-21\"\n"},
		{`require "date"; p Date.iso8601("2026-06-21T12:34:56").to_s`, "\"2026-06-21\"\n"},
		{`require "date"; p Date.iso8601("20260621").to_s`, "\"2026-06-21\"\n"},
		{`require "date"; p Date.iso8601("2026-06-21").iso8601`, "\"2026-06-21\"\n"},
		{`require "date"; p Date.xmlschema("2026-06-21").to_s`, "\"2026-06-21\"\n"},
		// DateTime.iso8601 / .xmlschema keep the wall clock and offset; a bare day
		// becomes midnight UTC.
		{`require "date"; p DateTime.iso8601("2026-06-21T12:34:56+02:00").to_s`, "\"2026-06-21T12:34:56+02:00\"\n"},
		{`require "date"; p DateTime.iso8601("2026-06-21T12:34:56+02:00").class`, "DateTime\n"},
		{`require "date"; p DateTime.iso8601("2026-06-21").to_s`, "\"2026-06-21T00:00:00+00:00\"\n"},
		{`require "date"; p DateTime.xmlschema("2026-06-21T12:34:56+02:00").xmlschema`, "\"2026-06-21T12:34:56+02:00\"\n"},
		// rfc3339 requires the full wall clock (no bare day).
		{`require "date"; p Date.rfc3339("2026-06-21T12:34:56+02:00").to_s`, "\"2026-06-21\"\n"},
		{`require "date"; p DateTime.rfc3339("2026-06-21T12:34:56+02:00").to_s`, "\"2026-06-21T12:34:56+02:00\"\n"},
		// rfc2822 / rfc822 mail date.
		{`require "date"; p Date.rfc2822("Sun, 21 Jun 2026 00:00:00 +0000").to_s`, "\"2026-06-21\"\n"},
		{`require "date"; p Date.rfc822("Sun, 21 Jun 2026 00:00:00 +0000").to_s`, "\"2026-06-21\"\n"},
		{`require "date"; p DateTime.rfc2822("Sun, 21 Jun 2026 12:34:56 +0200").to_s`, "\"2026-06-21T12:34:56+02:00\"\n"},
		// httpdate (RFC 2616 / GMT).
		{`require "date"; p Date.httpdate("Sun, 21 Jun 2026 00:00:00 GMT").to_s`, "\"2026-06-21\"\n"},
		{`require "date"; p DateTime.httpdate("Sun, 21 Jun 2026 12:34:56 GMT").to_s`, "\"2026-06-21T12:34:56+00:00\"\n"},
		// Malformed → Date::Error "invalid date" on both a Date and a DateTime parser.
		{`require "date"; begin; Date.iso8601("garbage"); rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"Date::Error\", \"invalid date\"]\n"},
		{`require "date"; begin; DateTime.rfc2822("x"); rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"Date::Error\", \"invalid date\"]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestComparableResiduals guards the Comparable module (clamp / between? and the
// <=>-derived operators) — implemented in the prelude — against MRI 4.0.5: the
// two-argument and Range (including beginless / endless) clamp forms, the
// exclusive-range / reversed-bound / non-Range ArgumentError & TypeError, and
// the "comparison of X with Y failed" error the operators raise when <=> is nil.
func TestComparableResiduals(t *testing.T) {
	// A tiny Comparable value wrapping an Integer, plus a peer that is incomparable
	// with it (its <=> always returns nil) — the fixture for the operator errors.
	const prelude = `
class N
  include Comparable
  attr_reader :v
  def initialize(v); @v = v; end
  def <=>(o); o.is_a?(N) ? (v <=> o.v) : nil; end
  def inspect; "N(#{v})"; end
end
`
	for _, c := range []struct{ src, want string }{
		// clamp: two-argument, Range, beginless / endless Range, and one-sided nil.
		{`p 5.clamp(1, 10)`, "5\n"},
		{`p 15.clamp(1, 10)`, "10\n"},
		{`p 0.clamp(1, 10)`, "1\n"},
		{`p 5.clamp(1..10)`, "5\n"},
		{`p 5.clamp(..3)`, "3\n"},
		{`p 5.clamp(8..)`, "8\n"},
		{`p 5.clamp(nil, 3)`, "3\n"},
		{`p 5.clamp(8, nil)`, "8\n"},
		// clamp error cases: exclusive range, reversed bounds, non-Range single arg.
		{`begin; 5.clamp(1...10); rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"ArgumentError\", \"cannot clamp with an exclusive range\"]\n"},
		{`begin; 5.clamp(10, 1); rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"ArgumentError\", \"min argument must be less than or equal to max argument\"]\n"},
		{`begin; 5.clamp(1); rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"TypeError\", \"wrong argument type Integer (expected Range)\"]\n"},
		// between?.
		{`p 5.between?(1, 10)`, "true\n"},
		{`p 0.between?(1, 10)`, "false\n"},
		{`p 15.between?(1, 10)`, "false\n"},
		// The <=>-derived operators on a mixed-in Comparable value.
		{prelude + `p(N.new(1) < N.new(2))`, "true\n"},
		{prelude + `p(N.new(2) <= N.new(2))`, "true\n"},
		{prelude + `p(N.new(3) > N.new(2))`, "true\n"},
		{prelude + `p(N.new(2) >= N.new(3))`, "false\n"},
		{prelude + `p(N.new(2) == N.new(2))`, "true\n"},
		// == is lenient: an incomparable pair (<=> nil) is simply unequal.
		{prelude + `p(N.new(2) == 2)`, "false\n"},
		// The ordering operators raise the exact rb_cmperr message when <=> is nil —
		// rendering an immediate via #inspect and a non-immediate by its class name.
		{prelude + `begin; N.new(2) < 5; rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"ArgumentError\", \"comparison of N with 5 failed\"]\n"},
		{prelude + `begin; N.new(2) < "x"; rescue => e; p [e.class.to_s, e.message]; end`,
			"[\"ArgumentError\", \"comparison of N with String failed\"]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
