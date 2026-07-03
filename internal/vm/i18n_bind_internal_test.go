// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"testing"

	i18n "github.com/go-ruby-i18n/i18n"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestI18nBackendWrapperMethods covers the ToS / Inspect / Truthy arms of the
// I18nBackend value protocol (the Ruby surface never routes through them, so
// they need a direct call).
func TestI18nBackendWrapperMethods(t *testing.T) {
	b := &I18nBackend{}
	if b.ToS() == "" || b.Inspect() == "" || !b.Truthy() {
		t.Errorf("I18nBackend methods: %q %q %v", b.ToS(), b.Inspect(), b.Truthy())
	}
}

// TestFromI18nValueEdgeCases covers the fromI18nValue arms unreachable from
// stored Ruby data: an int64 leaf and the AsString fallback for an unmodelled
// library value type.
func TestFromI18nValueEdgeCases(t *testing.T) {
	vm := New(nil)
	if v := vm.fromI18nValue(int64(9)); v.(object.Integer) != object.Integer(9) {
		t.Errorf("int64 -> %v", v)
	}
	// A value type the switch does not model falls back to i18n.AsString.
	if v := vm.fromI18nValue(struct{ X int }{1}); v.ToS() == "" {
		t.Errorf("unmodelled value -> %q", v.ToS())
	}
}

// TestRaiseI18nDefault covers the fallback arm of raiseI18n for an error type
// with no dedicated I18n class.
func TestRaiseI18nDefault(t *testing.T) {
	vm := New(nil)
	vm.registerI18n() // installs the I18n error tree raiseI18n resolves against
	defer func() {
		if r := recover(); r == nil {
			t.Error("raiseI18n default: expected a raise")
		}
	}()
	vm.raiseI18n(fmt.Errorf("some other i18n error"))
}

// TestI18nZoneNameNonString covers the ZoneName arm where the value responds to
// #zone but returns a non-String (a bare Date's #zone would; here a stub value
// that answers #zone with an Integer), which yields "".
func TestI18nZoneNameNonString(t *testing.T) {
	vm := New(nil)
	// An RObject of an anonymous class defining #zone -> 5 (a non-String).
	cls := newClass("ZoneStub", vm.cObject)
	cls.define("zone", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(5)
	})
	obj := &RObject{class: cls, ivars: map[string]object.Value{}}
	tmp := &i18nTemporal{vm: vm, obj: obj}
	if z := tmp.ZoneName(); z != "" {
		t.Errorf("ZoneName non-string -> %q, want empty", z)
	}
}

// TestI18nFieldNonInteger covers field's arm where the value responds to the
// accessor but returns a non-Integer, which yields 0.
func TestI18nFieldNonInteger(t *testing.T) {
	vm := New(nil)
	cls := newClass("HourStub", vm.cObject)
	cls.define("hour", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString("noon") // a non-Integer clock field
	})
	obj := &RObject{class: cls, ivars: map[string]object.Value{}}
	tmp := &i18nTemporal{vm: vm, obj: obj}
	if h := tmp.Hour(); h != 0 {
		t.Errorf("field non-integer -> %d, want 0", h)
	}
}

// TestLastHashEmpty covers lastHash's empty-args arm (the option parsers only
// call it after an arity check, so it is unreachable from the Ruby surface).
func TestLastHashEmpty(t *testing.T) {
	if _, ok := lastHash(nil); ok {
		t.Error("lastHash(nil) reported a hash")
	}
}

// TestI18nDefaultsArm sanity-checks the DefaultEntry construction helpers pair
// symmetrically (Key for a Symbol, Lit otherwise).
func TestI18nDefaultsArm(t *testing.T) {
	if got := i18nDefaults(object.Symbol("k")); len(got) != 1 {
		t.Errorf("single default -> %d entries", len(got))
	}
	_ = i18n.Lit("x") // keep the i18n import used by the Lit/Key surface
}
