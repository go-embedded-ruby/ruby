// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestARValueToRubyGoOnly covers the arValueToRuby arms a live sqlite3 driver
// never produces (int / bool) and its terminal fallback (an unmapped Go type),
// which are reachable only from Go. The int64 / float64 / string / []byte / nil
// arms are exercised through real queries by the vm_test suite.
func TestARValueToRubyGoOnly(t *testing.T) {
	if got := arValueToRuby(int(7)); got != object.Integer(7) {
		t.Errorf("int arm got=%v", got)
	}
	if got := arValueToRuby(true); got != object.Bool(true) {
		t.Errorf("bool arm got=%v", got)
	}
	// A Go value of no mapped type degrades to nil, matching a column the driver
	// could not classify.
	if got := arValueToRuby(struct{}{}); got != object.NilV {
		t.Errorf("fallback arm got=%v", got)
	}
	// int64 / float64 / string / []byte / nil, for completeness of the mapper.
	if got := arValueToRuby(int64(3)); got != object.Integer(3) {
		t.Errorf("int64 arm got=%v", got)
	}
	if got := arValueToRuby(float64(1.5)); got != object.Float(1.5) {
		t.Errorf("float arm got=%v", got)
	}
	if s, ok := object.KindOK[*object.String](arValueToRuby("x")); !ok || s.Str() != "x" {
		t.Errorf("string arm got=%v", arValueToRuby("x"))
	}
	if s, ok := object.KindOK[*object.String](arValueToRuby([]byte("ab"))); !ok || s.Str() != "ab" {
		t.Errorf("bytes arm got=%v", arValueToRuby([]byte("ab")))
	}
	if got := arValueToRuby(nil); got != object.NilV {
		t.Errorf("nil arm got=%v", got)
	}
}

// TestARAdapterName covers the arSQLiteAdapter.AdapterName reporter, which the
// activerecord core reaches only through DialectFor (a dialect-selection path the
// rbgo binding never takes, since the SQLite dialect is the default), so it has
// no Ruby-level trigger.
func TestARAdapterName(t *testing.T) {
	if got := (&arSQLiteAdapter{}).AdapterName(); got != "sqlite3" {
		t.Errorf("AdapterName got=%q", got)
	}
}

// TestARIntEmpty covers arInt's empty-argument arm (a chained #limit / #offset
// called with no argument yields 0). The non-integer arm is exercised from Ruby.
func TestARIntEmpty(t *testing.T) {
	if got := arInt(nil); got != 0 {
		t.Errorf("arInt(nil) got=%d", got)
	}
}
