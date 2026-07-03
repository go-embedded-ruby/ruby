// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"math/big"
	"testing"

	date "github.com/go-ruby-date/date"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// evalDate runs src through the full pipeline and returns captured stdout — the
// in-package counterpart of vm_test.eval, needed for the cases that also pin the
// nowUnix clock seam (an unexported package symbol).
func evalDate(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	return buf.String()
}

// TestDateClockSeam pins the nowUnix seam (the same clock Time.now uses) and
// checks that Date.today / DateTime.now read it deterministically through the
// library's SetTodayInstant override — Unix 1782045296 is 2026-06-21 12:34:56
// UTC. The seam is restored afterwards.
func TestDateClockSeam(t *testing.T) {
	saved := nowUnix
	defer func() { nowUnix = saved }()
	nowUnix = func() int64 { return 1782045296 }

	if got := evalDate(t, `puts Date.today.to_s`); got != "2026-06-21\n" {
		t.Errorf("Date.today = %q, want 2026-06-21", got)
	}
	if got := evalDate(t, `puts DateTime.now.to_s`); got != "2026-06-21T12:34:56+00:00\n" {
		t.Errorf("DateTime.now = %q, want the pinned instant", got)
	}
}

// TestDateOffsetForms covers offsetSeconds across its argument shapes — the
// numeric Integer / Float / Rational forms (read as a fraction of a day) MRI's
// DateTime.new accepts alongside the zone string — by building a DateTime and
// reading back its offset. 0.5 day == +12:00 == 43200s.
func TestDateOffsetForms(t *testing.T) {
	for _, c := range []struct {
		v    object.Value
		want int
	}{
		{object.IntValue(int64(object.Integer(0))), 0},
		{object.FloatValue(float64(object.Float(0.5))), 43200},
		{object.Wrap(&object.Rational{R: big.NewRat(1, 2)}), 43200},
	} {
		if got := offsetSeconds(c.v); got != c.want {
			t.Errorf("offsetSeconds(%v) = %d, want %d", c.v, got, c.want)
		}
	}
}

// TestDateOffsetBadType covers offsetSeconds' TypeError arm for a value that is
// neither a zone string nor a numeric.
func TestDateOffsetBadType(t *testing.T) {
	wantRaise(t, "TypeError", func() { offsetSeconds(object.NilVal()) })
}

// TestZoneOffsetSeconds covers zoneOffsetSeconds across the accepted forms and
// the rejected ones — the named-UTC sentinels, the "+HH" / "+HHMM" / "+HH:MM"
// signed forms, and malformed inputs (no sign, wrong length, non-digit, an
// out-of-range hour / minute).
func TestZoneOffsetSeconds(t *testing.T) {
	for _, c := range []struct {
		s     string
		want  int
		valid bool
	}{
		{"Z", 0, true},
		{"UTC", 0, true},
		{"GMT", 0, true},
		{"+00:00", 0, true},
		{"-00:00", 0, true},
		{"+02", 7200, true},
		{"+0200", 7200, true},
		{"+02:00", 7200, true},
		{"-05:30", -19800, true},
		{"-0530", -19800, true},
		{"", 0, false},        // too short
		{"02:00", 0, false},   // no sign
		{"+2", 0, false},      // body too short
		{"+02:0", 0, false},   // odd length after stripping colon
		{"+0x", 0, false},     // non-digit hour
		{"+24:00", 0, false},  // hour out of range
		{"+02:60", 0, false},  // minute out of range
		{"+02:6x", 0, false},  // non-digit minute
		{"+020000", 0, false}, // too long
	} {
		got, ok := zoneOffsetSeconds(c.s)
		if ok != c.valid || (ok && got != c.want) {
			t.Errorf("zoneOffsetSeconds(%q) = %d, %v; want %d, %v", c.s, got, ok, c.want, c.valid)
		}
	}
}

// TestDateOpUnsupported covers dateOp's final NoMethodError arm: an opcode
// outside +/-, unreachable from a Ruby program (only OpAdd / OpSub reach dateOp
// from binary()), exercised directly with the multiply opcode.
func TestDateOpUnsupported(t *testing.T) {
	wantRaise(t, "NoMethodError", func() {
		d, _ := date.NewDate(2026, 6, 29)
		dateOp(bytecode.OpMul, &Date{d: d}, object.IntValue(int64(object.Integer(1))))
	})
}

// TestPayloadDateArgumentError covers payloadDate's non-ErrInvalidDate arm: a
// generic library error re-raises as ArgumentError (the Date::Error arm is
// covered through the Ruby-level invalid-date cases).
func TestPayloadDateArgumentError(t *testing.T) {
	wantRaise(t, "ArgumentError", func() {
		payloadDate(nil, errGeneric{})
	})
}

// TestDateDefaultArgs covers compArg / strptimeFormat across the present and
// absent argument: comp defaults to true with no argument and reflects an
// explicit one, and the format defaults to "%F" with only the string argument.
func TestDateDefaultArgs(t *testing.T) {
	if got := compArg(nil, 1); got != true {
		t.Errorf("compArg default = %v, want true", got)
	}
	if got := compArg([]object.Value{object.Wrap(object.NewString("s")), object.BoolValue(bool(object.False))}, 1); got != false {
		t.Errorf("compArg explicit false = %v, want false", got)
	}
	if got := strptimeFormat([]object.Value{object.Wrap(object.NewString("2026-06-29"))}); got != "%F" {
		t.Errorf("strptimeFormat default = %q, want %%F", got)
	}
}

// TestDateInspectNegativeCarry covers dateInspect's UTC borrow for an east-of-UTC
// DateTime whose local time-of-day, shifted to UTC, lands on the previous day —
// the negative-nanosecond carry branch. 00:30 local at +02:00 is 22:30 the day
// before in UTC, so the bracketed jd is one less than the local Jd.
func TestDateInspectNegativeCarry(t *testing.T) {
	d, err := date.NewDateTime(2026, 6, 29, 0, 30, 0, 7200)
	if err != nil {
		t.Fatalf("NewDateTime: %v", err)
	}
	got := dateInspect(d)
	want := "#<DateTime: 2026-06-29T00:30:00+02:00 ((2461220j,81000s,0n),+7200s,2299161j)>"
	if got != want {
		t.Errorf("dateInspect negative carry = %q, want %q", got, want)
	}
}

// TestDateInspectPositiveCarry covers dateInspect's other UTC-borrow arm: a
// west-of-UTC DateTime whose local time-of-day, shifted to UTC, lands on the
// NEXT day (ns >= one day) — so the bracketed jd is one more than the local Jd.
// 23:00 local at -02:00 is 01:00 the next day in UTC.
func TestDateInspectPositiveCarry(t *testing.T) {
	d, err := date.NewDateTime(2026, 6, 29, 23, 0, 0, -7200)
	if err != nil {
		t.Fatalf("NewDateTime: %v", err)
	}
	got := dateInspect(d)
	want := "#<DateTime: 2026-06-29T23:00:00-02:00 ((2461222j,3600s,0n),-7200s,2299161j)>"
	if got != want {
		t.Errorf("dateInspect positive carry = %q, want %q", got, want)
	}
}

// errGeneric is a non-ErrInvalidDate error for TestPayloadDateArgumentError.
type errGeneric struct{}

func (errGeneric) Error() string { return "some other failure" }
