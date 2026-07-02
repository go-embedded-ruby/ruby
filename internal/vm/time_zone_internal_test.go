// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"
	stdtime "time"
)

// TestGoTimeToRubyPreservesWallClock proves the Bug-1 fix: a Go time.Time in a
// non-UTC zone becomes an rbgo Time whose strftime renders the zone's wall clock
// (matching MRI, where Time.local(...).strftime shows the local hour), while
// getutc re-shifts to UTC. A FixedZone makes the assertion independent of $TZ.
func TestGoTimeToRubyPreservesWallClock(t *testing.T) {
	// +02:00 zone, wall clock 12:00:00 → UTC instant is 10:00:00.
	loc := stdtime.FixedZone("plus2", 2*3600)
	src := stdtime.Date(2016, 5, 27, 12, 0, 0, 0, loc)

	rt := goTimeToRuby(src).(*Time)

	// strftime path (Time#strftime delegates to t.Format(rubyLayout(...))): the
	// wall clock of the Time's own zone, NOT the UTC-shifted hour.
	if got := rt.t.Format(rubyLayout("%H:%M:%S")); got != "12:00:00" {
		t.Errorf("local strftime = %q, want %q (must show wall clock, not UTC)", got, "12:00:00")
	}
	if got := rt.t.Format(rubyLayout("%Y-%m-%d %H:%M:%S")); got != "2016-05-27 12:00:00" {
		t.Errorf("local strftime = %q, want %q", got, "2016-05-27 12:00:00")
	}

	// getutc path (Time#getutc → t.UTC()) still renders UTC: 12:00:00+02:00 is
	// 10:00:00Z.
	if got := rt.t.UTC().Format(rubyLayout("%H:%M:%S")); got != "10:00:00" {
		t.Errorf("getutc strftime = %q, want %q (must render UTC)", got, "10:00:00")
	}

	// The absolute instant is preserved regardless of the rendered zone.
	if got := rt.t.ToUnix(); got != src.Unix() {
		t.Errorf("ToUnix = %d, want %d (instant must be preserved)", got, src.Unix())
	}
}

// TestGoTimeToRubyNegativeOffset covers a west-of-UTC zone so the wall-clock vs
// UTC divergence is exercised in both directions.
func TestGoTimeToRubyNegativeOffset(t *testing.T) {
	loc := stdtime.FixedZone("minus5", -5*3600) // EST-like
	src := stdtime.Date(1979, 5, 27, 3, 32, 0, 0, loc)

	rt := goTimeToRuby(src).(*Time)

	if got := rt.t.Format(rubyLayout("%H:%M:%S")); got != "03:32:00" {
		t.Errorf("local strftime = %q, want %q", got, "03:32:00")
	}
	// 03:32-05:00 is 08:32Z.
	if got := rt.t.UTC().Format(rubyLayout("%H:%M:%S")); got != "08:32:00" {
		t.Errorf("getutc strftime = %q, want %q", got, "08:32:00")
	}
}

// TestGoTimeToRubySubSecondTruncated confirms sub-second input is truncated to
// whole seconds (the Time surface is second-resolution) without disturbing the
// preserved zone.
func TestGoTimeToRubySubSecondTruncated(t *testing.T) {
	loc := stdtime.FixedZone("plus1", 3600)
	src := stdtime.Date(2020, 1, 1, 6, 0, 0, 987654321, loc)

	rt := goTimeToRuby(src).(*Time)

	if got := rt.t.Format(rubyLayout("%H:%M:%S")); got != "06:00:00" {
		t.Errorf("truncated strftime = %q, want %q", got, "06:00:00")
	}
	if got := rt.t.ToUnix(); got != src.Truncate(stdtime.Second).Unix() {
		t.Errorf("ToUnix = %d, want %d", got, src.Truncate(stdtime.Second).Unix())
	}
}

// TestZonedFromRFC3339Fallback exercises the defensive error branch: a string
// that is not valid RFC3339 falls back to the supplied UTC instant rather than
// panicking. (goTimeToRuby never feeds it a bad string, but the guard exists so
// a hypothetical malformed layout is safe.)
func TestZonedFromRFC3339Fallback(t *testing.T) {
	got := zonedFromRFC3339("not-a-timestamp", 0)
	if got.ToUnix() != 0 {
		t.Errorf("fallback ToUnix = %d, want 0", got.ToUnix())
	}
	if s := got.Format("2006-01-02 15:04:05"); s != "1970-01-01 00:00:00" {
		t.Errorf("fallback render = %q, want epoch UTC", s)
	}
}
