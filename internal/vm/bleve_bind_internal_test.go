// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math/big"
	"testing"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	libbleve "github.com/go-ruby-bleve/bleve"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestBleveValueToGo covers the branches of bleveValueToGo a Ruby program cannot
// reach directly: a Go-nil interface, a Bignum (a Ruby literal in int64 range is
// an Integer, never a Bignum), and the TypeError tail for an unmappable value.
func TestBleveValueToGo(t *testing.T) {
	if got := bleveValueToGo(nil); got != nil {
		t.Errorf("bleveValueToGo(nil) = %#v; want nil", got)
	}
	big30 := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	if got, ok := bleveValueToGo(&object.Bignum{I: big30}).(float64); !ok || got != 1e30 {
		t.Errorf("Bignum(10^30) -> %#v; want float64 1e30", got)
	}
	// A wrapper with no document representation raises TypeError.
	assertRaises(t, "TypeError", func() { bleveValueToGo(&BleveQuery{q: libbleve.MatchAll()}) })
}

// TestBleveFieldToValue covers bleveFieldToValue across every Go element bleve
// can yield for a stored field — nil, bool, float64, string, a Time and a
// multi-valued slice — plus the defensive tail for an element type a future
// bleve version might return.
func TestBleveFieldToValue(t *testing.T) {
	for _, c := range []struct {
		in   interface{}
		want string // Ruby #inspect of the produced value
	}{
		{nil, "nil"},
		{true, "true"},
		{float64(2.5), "2.5"},
		{"hi", "\"hi\""},
		{stdtime.Unix(1000, 0), ""}, // Time renders variably; only checked for non-empty below
		{[]interface{}{"a", float64(1)}, "[\"a\", 1.0]"},
		{struct{}{}, "nil"}, // defensive tail: an unmodelled element reads back nil
	} {
		got := bleveFieldToValue(c.in)
		if _, isTime := c.in.(stdtime.Time); isTime {
			if _, ok := got.(*Time); !ok {
				t.Errorf("Time element -> %T; want *Time", got)
			}
			continue
		}
		if got.Inspect() != c.want {
			t.Errorf("bleveFieldToValue(%#v) = %q; want %q", c.in, got.Inspect(), c.want)
		}
	}
}

// TestBleveKeyName covers the TypeError branch of bleveKeyName: a document field
// name that is neither a String nor a Symbol.
func TestBleveKeyName(t *testing.T) {
	if bleveKeyName(object.NewString("a")) != "a" {
		t.Errorf("String key not resolved")
	}
	if bleveKeyName(object.Symbol("b")) != "b" {
		t.Errorf("Symbol key not resolved")
	}
	assertRaises(t, "TypeError", func() { bleveKeyName(object.IntValue(7)) })
}

// TestBleveFloatAndTime covers the numeric- and Time-coercion helpers' error and
// Bignum branches, which a Ruby caller reaches only indirectly.
func TestBleveFloatAndTime(t *testing.T) {
	big30 := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	if bleveFloat(&object.Bignum{I: big30}) != 1e30 {
		t.Errorf("bleveFloat(Bignum) mismatch")
	}
	assertRaises(t, "TypeError", func() { bleveFloat(object.NewString("x")) })
	assertRaises(t, "TypeError", func() { bleveTime(object.NewString("x")) })

	tm := &Time{t: gotime.FromUnix(1000)}
	if bleveTime(tm).Unix() != 1000 {
		t.Errorf("bleveTime mismatch")
	}
}

// TestBleveRaiseErr covers raiseBleveErr: nil is a no-op, the closed and
// not-found sentinels map to their faithful Ruby classes, and any other bleve
// error falls back to Bleve::Error.
func TestBleveRaiseErr(t *testing.T) {
	raiseBleveErr(nil) // no panic

	assertRaises(t, "Bleve::ClosedError", func() {
		raiseBleveErr(&libbleve.Error{Op: "index", Err: libbleve.ErrClosed})
	})
	assertRaises(t, "Bleve::NotFoundError", func() {
		raiseBleveErr(&libbleve.Error{Op: "document", Err: libbleve.ErrNotFound})
	})
	assertRaises(t, "Bleve::Error", func() { raiseBleveErr(errors.New("boom")) })
}

// TestBleveWrapperRendering covers the object.Value surface (ToS / Inspect /
// Truthy) of every Bleve wrapper, reached by the VM's printing and
// boolean-coercion paths and pinned here directly.
func TestBleveWrapperRendering(t *testing.T) {
	ix, err := libbleve.NewMemIndex(nil)
	if err != nil {
		t.Fatalf("NewMemIndex: %v", err)
	}
	defer ix.Close()

	wrappers := []object.Value{
		&BleveIndex{ix: ix},
		&BleveMapping{m: libbleve.NewMapping()},
		&BleveQuery{q: libbleve.MatchAll()},
		&BleveSearchResult{},
		&BleveHit{h: libbleve.Hit{ID: "doc-1"}},
		&BleveBatch{},
		&BleveFacet{f: libbleve.TermFacet("tag", 3)},
	}
	for _, w := range wrappers {
		if !w.Truthy() {
			t.Errorf("%T must be truthy", w)
		}
		if w.ToS() == "" || w.Inspect() == "" {
			t.Errorf("%T rendered empty ToS/Inspect", w)
		}
	}
}
