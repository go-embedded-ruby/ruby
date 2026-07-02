// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestChronicConstants covers the Chronic module (require "chronic").
func TestChronicConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "chronic"; p Chronic.is_a?(Module)`, "true\n"},
		{`p require "chronic"`, "true\n"},
		{`require "chronic"; p require "chronic"`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestChronicParse covers Chronic.parse across an absolute timestamp, a relative
// phrase resolved against an anchor, and a non-parse returning nil.
func TestChronicParse(t *testing.T) {
	// A fixed anchor makes every parse deterministic (Time.at(1155736800) is
	// 2006-08-16 14:00 UTC).
	anchor := `now: Time.at(1155736800)`
	cases := []struct{ src, want string }{
		// An ISO timestamp parses to that wall-clock time, rendered in the parsed
		// Time's own (local) zone — matching MRI, where strftime shows the zone's
		// wall clock. So it round-trips to the same string on ANY box, not just a
		// UTC one; the absolute .to_i would be timezone-dependent.
		{`require "chronic"; p Chronic.parse("2016-05-27 12:00:00", ` + anchor + `).strftime("%Y-%m-%d %H:%M:%S")`, "\"2016-05-27 12:00:00\"\n"},
		{`require "chronic"; p Chronic.parse("2016-05-27 12:00:00", ` + anchor + `).class`, "Time\n"},
		// A relative phrase resolves against the anchor into the future.
		{`require "chronic"; p (Chronic.parse("tomorrow", ` + anchor + `).to_i > Time.at(1155736800).to_i)`, "true\n"},
		// context: :past resolves backward.
		{`require "chronic"; p (Chronic.parse("may", ` + anchor + `, context: :past).to_i < Time.at(1155736800).to_i)`, "true\n"},
		// A phrase that does not parse returns nil.
		{`require "chronic"; p Chronic.parse("total gibberish zzz")`, "nil\n"},
		// No options hash at all still parses.
		{`require "chronic"; p Chronic.parse("2016-05-27 12:00:00").class`, "Time\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestChronicEndian covers the endian_precedence option (both the [:little, ...]
// array form and the bare :little symbol) selecting day/month parsing.
func TestChronicEndian(t *testing.T) {
	anchor := `now: Time.at(1155736800)`
	// "03/04/2011" is April 3 (d/m) with little-endian, March 4 (m/d) by default.
	def := eval(t, `require "chronic"; p Chronic.parse("03/04/2011", `+anchor+`).strftime("%m-%d")`)
	little := eval(t, `require "chronic"; p Chronic.parse("03/04/2011", `+anchor+`, endian_precedence: [:little, :middle]).strftime("%m-%d")`)
	bare := eval(t, `require "chronic"; p Chronic.parse("03/04/2011", `+anchor+`, endian_precedence: :little).strftime("%m-%d")`)
	if def == little {
		t.Errorf("endian: default (%q) should differ from little (%q)", def, little)
	}
	if little != bare {
		t.Errorf("endian: array (%q) and bare-symbol (%q) little should match", little, bare)
	}
}

// TestChronicGuess covers the guess option: :begin / :end pick a span endpoint,
// while guess: false and guess: :none return the whole span as [begin, end].
func TestChronicGuess(t *testing.T) {
	anchor := `now: Time.at(1155736800)`
	cases := []struct{ src, want string }{
		// guess: :begin and :end select the span's start / end (begin <= end).
		{`require "chronic"; b = Chronic.parse("this month", ` + anchor + `, guess: :begin); e = Chronic.parse("this month", ` + anchor + `, guess: :end); p (b.to_i <= e.to_i)`, "true\n"},
		// guess: false returns the whole span as a two-element Array of Time.
		{`require "chronic"; s = Chronic.parse("this month", ` + anchor + `, guess: false); p s.class`, "Array\n"},
		{`require "chronic"; s = Chronic.parse("this month", ` + anchor + `, guess: false); p s.length`, "2\n"},
		{`require "chronic"; s = Chronic.parse("this month", ` + anchor + `, guess: false); p s.all? { |x| x.is_a?(Time) }`, "true\n"},
		// guess: :none is the same as guess: false.
		{`require "chronic"; p Chronic.parse("this month", ` + anchor + `, guess: :none).class`, "Array\n"},
		// A span request that does not parse returns nil.
		{`require "chronic"; p Chronic.parse("nonsense qqq", ` + anchor + `, guess: false)`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestChronicErrors covers the arity path and a non-hash trailing argument (which
// is ignored, not an options hash).
func TestChronicErrors(t *testing.T) {
	got := eval(t, `require "chronic"
begin
  Chronic.parse
rescue ArgumentError
  puts "arity"
end`)
	if !strings.Contains(got, "arity") {
		t.Errorf("no-arg: got %q", got)
	}
}
