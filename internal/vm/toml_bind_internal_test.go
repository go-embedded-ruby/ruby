// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"testing"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	toml "github.com/go-ruby-toml/toml"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestTOMLToBridge covers the Go-only arms of toTOML the Ruby round-trip tests
// do not reach directly: a plain Go nil, the object.Nil singleton, a Bignum, a
// Time, and the default (unmapped) fall-through.
func TestTOMLToBridge(t *testing.T) {
	if toTOML(nil) != nil {
		t.Error("go-nil should map to nil")
	}
	if toTOML(object.NilVal()) != nil {
		t.Error("object.NilV should map to nil")
	}
	if v, ok := toTOML(object.NormInt(big1e30())).(*big.Int); !ok || v.Sign() <= 0 {
		t.Errorf("bignum -> %T", toTOML(object.NormInt(big1e30())))
	}
	if v, ok := toTOML(object.Wrap(&Time{t: gotime.FromUnix(42)})).(stdtime.Time); !ok || v.Unix() != 42 {
		t.Errorf("time -> %T", toTOML(object.Wrap(&Time{t: gotime.FromUnix(42)})))
	}
	if _, ok := toTOML(object.Wrap(&Proc{})).(*Proc); !ok {
		t.Errorf("unmapped -> %T", toTOML(object.Wrap(&Proc{})))
	}
}

// TestTOMLKeyBridge covers tomlKey across its Symbol, String and to_s-default
// arms.
func TestTOMLKeyBridge(t *testing.T) {
	if got := tomlKey(object.SymVal(string(object.Symbol("s")))); got != "s" {
		t.Errorf("symbol key -> %q", got)
	}
	if got := tomlKey(object.Wrap(object.NewString("str"))); got != "str" {
		t.Errorf("string key -> %q", got)
	}
	// A non-Symbol / non-String key renders via to_s.
	if got := tomlKey(object.IntValue(int64(object.Integer(3)))); got != "3" {
		t.Errorf("integer key -> %q", got)
	}
}

// TestTOMLFromBridge covers the Go-only arms of fromTOML: a *big.Int and the
// four TOML datetime shapes collapsing onto Ruby Time.
func TestTOMLFromBridge(t *testing.T) {
	vm := New(nil)
	if v := fromTOML(vm, big1e30()); v == nil {
		t.Error("big.Int -> nil")
	}
	// Offset date-time.
	off := toml.OffsetDateTime{Time: stdtime.Unix(296638320, 0).UTC()}
	if tm, ok := object.KindOK[*Time](fromTOML(vm, off)); !ok || tm.t.ToUnix() != 296638320 {
		t.Errorf("offset dt -> %#v", fromTOML(vm, off))
	}
	// Local date-time (materialised UTC).
	ldt := toml.LocalDateTime{Year: 1979, Month: 5, Day: 27, Hour: 7, Minute: 32}
	want := stdtime.Date(1979, 5, 27, 7, 32, 0, 0, stdtime.UTC).Unix()
	if tm, ok := object.KindOK[*Time](fromTOML(vm, ldt)); !ok || tm.t.ToUnix() != want {
		t.Errorf("local dt -> %#v", fromTOML(vm, ldt))
	}
	// Local date.
	ld := toml.LocalDate{Year: 1979, Month: 5, Day: 27}
	if tm, ok := object.KindOK[*Time](fromTOML(vm, ld)); !ok || tm.t.ToUnix() != stdtime.Date(1979, 5, 27, 0, 0, 0, 0, stdtime.UTC).Unix() {
		t.Errorf("local date -> %#v", fromTOML(vm, ld))
	}
	// Local time (on the epoch date).
	lt := toml.LocalTime{Hour: 7, Minute: 32, Second: 5}
	if tm, ok := object.KindOK[*Time](fromTOML(vm, lt)); !ok || tm.t.ToUnix() != stdtime.Date(1970, 1, 1, 7, 32, 5, 0, stdtime.UTC).Unix() {
		t.Errorf("local time -> %#v", fromTOML(vm, lt))
	}
}

// TestTOMLFromDefaults covers fromTOML's defensive arms the parser never
// actually produces: a nil value and an unmodelled Go value both map to nil.
func TestTOMLFromDefaults(t *testing.T) {
	vm := New(nil)
	if v := fromTOML(vm, nil); v != object.NilV {
		t.Errorf("nil -> %v", v)
	}
	if v := fromTOML(vm, struct{}{}); v != object.NilV {
		t.Errorf("unmodelled -> %v", v)
	}
}

// TestTOMLSourceArgNonString covers tomlSourceArg's to_s branch for a non-String
// argument.
func TestTOMLSourceArgNonString(t *testing.T) {
	if got := tomlSourceArg(object.IntValue(int64(object.Integer(1)))); got != "1" {
		t.Errorf("non-string arg -> %q", got)
	}
}
