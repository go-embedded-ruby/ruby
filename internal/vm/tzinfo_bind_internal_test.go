// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"

	tzinfo "github.com/go-ruby-tzinfo/tzinfo"
)

// TestTZInfoValueMethods covers the ToS / Inspect / Truthy arms of the four
// TZInfo value wrappers directly (the p / inspect Ruby tests reach Inspect but
// not the bare ToS / Truthy the object protocol calls elsewhere).
func TestTZInfoValueMethods(t *testing.T) {
	tz, err := tzinfo.Get("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	zw := &Timezone{tz: tz}
	if zw.ToS() != "America/New_York" || !zw.Truthy() {
		t.Errorf("Timezone ToS/Truthy: %q %v", zw.ToS(), zw.Truthy())
	}
	if zw.Inspect() == "" {
		t.Error("Timezone Inspect empty")
	}

	per := tz.CurrentPeriod()
	pw := &TimezonePeriod{p: per}
	if pw.ToS() == "" || pw.Inspect() == "" || !pw.Truthy() {
		t.Error("TimezonePeriod methods")
	}

	ow := &TimezoneOffset{o: per.Offset}
	if ow.ToS() == "" || ow.Inspect() == "" || !ow.Truthy() {
		t.Error("TimezoneOffset methods")
	}

	c, err := tzinfo.GetCountry("US")
	if err != nil {
		t.Fatal(err)
	}
	cw := &Country{c: c}
	if cw.ToS() == "" || cw.Inspect() == "" || !cw.Truthy() {
		t.Error("Country methods")
	}
}

// TestTZCheck covers tzCheck's error arm — the defensive RuntimeError raised when
// the embedded database cannot be read (unreachable in a correct build).
func TestTZCheck(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("tzCheck(err) should raise")
		}
	}()
	tzCheck(errors.New("boom"))
}

// TestTZCheckNil covers tzCheck's happy arm (a nil error is a no-op).
func TestTZCheckNil(t *testing.T) { tzCheck(nil) }
