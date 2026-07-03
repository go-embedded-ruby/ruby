// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	tzinfo "github.com/go-ruby-tzinfo/tzinfo"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Timezone wraps a *tzinfo.Timezone as a Ruby TZInfo::Timezone object. The
// resolution, UTC↔local conversion and period computation all live in the
// github.com/go-ruby-tzinfo/tzinfo library; this shell only reports the Ruby
// class and delegates each method to the wrapped value (see tzinfo_bind.go).
type Timezone struct{ tz *tzinfo.Timezone }

func (t *Timezone) ToS() string     { return t.tz.Identifier() }
func (t *Timezone) Inspect() string { return "#<TZInfo::Timezone: " + t.tz.Identifier() + ">" }
func (t *Timezone) Truthy() bool    { return true }

// TimezonePeriod wraps a tzinfo.TimezonePeriod as a Ruby TZInfo::TimezonePeriod.
type TimezonePeriod struct{ p tzinfo.TimezonePeriod }

func (p *TimezonePeriod) ToS() string     { return "#<TZInfo::TimezonePeriod: " + p.p.Abbreviation() + ">" }
func (p *TimezonePeriod) Inspect() string { return p.ToS() }
func (p *TimezonePeriod) Truthy() bool    { return true }

// TimezoneOffset wraps a tzinfo.TimezoneOffset as a Ruby TZInfo::TimezoneOffset.
type TimezoneOffset struct{ o tzinfo.TimezoneOffset }

func (o *TimezoneOffset) ToS() string     { return "#<TZInfo::TimezoneOffset: " + o.o.Abbreviation + ">" }
func (o *TimezoneOffset) Inspect() string { return o.ToS() }
func (o *TimezoneOffset) Truthy() bool    { return true }

// Country wraps a *tzinfo.Country as a Ruby TZInfo::Country.
type Country struct{ c *tzinfo.Country }

func (c *Country) ToS() string     { return "#<TZInfo::Country: " + c.c.Code() + ">" }
func (c *Country) Inspect() string { return c.ToS() }
func (c *Country) Truthy() bool    { return true }

// registerTZInfo installs the TZInfo module (require "tzinfo") and its native
// value classes (Timezone / TimezonePeriod / TimezoneOffset / Country) plus the
// error tree (InvalidTimezoneIdentifier / InvalidCountryCode / AmbiguousTime /
// PeriodNotFound). The four value classes report their own Ruby class via
// classOf (looked up from vm.consts) so instance methods dispatch correctly.
func (vm *VM) registerTZInfo() {
	mod := newClass("TZInfo", nil)
	mod.isModule = true
	vm.consts["TZInfo"] = object.Wrap(mod)
	vm.registerTZInfoErrors(mod)

	tzCls := vm.tzNestedClass(mod, "Timezone")
	vm.tzNestedClass(mod, "TimezonePeriod")
	vm.tzNestedClass(mod, "TimezoneOffset")
	countryCls := vm.tzNestedClass(mod, "Country")

	// TZInfo::Timezone.get(id) / .all / .all_identifiers, and the gem's Time-ish
	// convenience Timezone.now on the class (delegates to the default? no — the
	// gem has no Timezone.now; keep it on the instance).
	tzDef := func(name string, fn NativeFn) {
		tzCls.smethods[name] = &Method{name: name, owner: tzCls, native: fn}
	}
	tzDef("get", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return tzGet(strArg(args[0]))
	})
	tzDef("all_identifiers", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return tzIdentifiers()
	})
	tzDef("all", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return tzAll()
	})

	// TZInfo::Timezone instance methods.
	tzCls.define("identifier", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*Timezone](self).tz.Identifier()))
	})
	tzCls.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*Timezone](self).tz.Identifier()))
	})
	tzCls.define("canonical_identifier", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*Timezone](self).tz.CanonicalIdentifier()))
	})
	tzCls.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*Timezone](self).tz.String()))
	})
	tzCls.define("now", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return goTimeToRuby(object.Kind[*Timezone](self).tz.Now())
	})
	tzCls.define("utc_to_local", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return goTimeToRuby(object.Kind[*Timezone](self).tz.UTCToLocal(rubyTimeArg(args)))
	})
	tzCls.define("local_to_utc", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out, err := object.Kind[*Timezone](self).tz.LocalToUTC(rubyTimeArg(args))
		if err != nil {
			raise("TZInfo::PeriodNotFound", "%s", err.Error())
		}
		return goTimeToRuby(out)
	})
	tzCls.define("period_for_utc", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(&TimezonePeriod{p: object.Kind[*Timezone](self).tz.PeriodForUTC(rubyTimeArg(args))})
	})
	tzCls.define("current_period", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&TimezonePeriod{p: object.Kind[*Timezone](self).tz.CurrentPeriod()})
	})
	tzCls.define("utc_offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(object.Kind[*Timezone](self).tz.UTCOffset()))
	})
	tzCls.define("abbreviation", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*Timezone](self).tz.Abbreviation(rubyTimeArg(args))))
	})
	tzCls.define("dst?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*Timezone](self).tz.DST(rubyTimeArg(args)))))
	})

	// TZInfo::TimezonePeriod instance methods.
	pCls := object.Kind[*RClass](vm.consts["TZInfo::TimezonePeriod"])
	pCls.define("offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&TimezoneOffset{o: object.Kind[*TimezonePeriod](self).p.Offset})
	})
	pCls.define("abbreviation", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*TimezonePeriod](self).p.Abbreviation()))
	})
	pCls.define("dst?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*TimezonePeriod](self).p.DST())))
	})
	pCls.define("base_utc_offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(object.Kind[*TimezonePeriod](self).p.BaseUTCOffset()))
	})
	pCls.define("std_offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(object.Kind[*TimezonePeriod](self).p.STDOffset()))
	})
	pCls.define("utc_total_offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(object.Kind[*TimezonePeriod](self).p.UTCTotalOffset()))
	})

	// TZInfo::TimezoneOffset instance methods.
	oCls := object.Kind[*RClass](vm.consts["TZInfo::TimezoneOffset"])
	oCls.define("base_utc_offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(object.Kind[*TimezoneOffset](self).o.BaseUTCOffset))
	})
	oCls.define("std_offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(object.Kind[*TimezoneOffset](self).o.STDOffset))
	})
	oCls.define("utc_total_offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(object.Kind[*TimezoneOffset](self).o.UTCTotalOffset()))
	})
	oCls.define("abbreviation", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*TimezoneOffset](self).o.Abbreviation))
	})
	oCls.define("dst?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*TimezoneOffset](self).o.DST())))
	})

	// TZInfo::Country.get(code) / .all_codes.
	countryCls.smethods["get"] = &Method{name: "get", owner: countryCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return tzGetCountry(strArg(args[0]))
		}}
	countryCls.smethods["all_codes"] = &Method{name: "all_codes", owner: countryCls,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return tzCountryCodes()
		}}
	countryCls.define("code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*Country](self).c.Code()))
	})
	countryCls.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(object.Kind[*Country](self).c.Name()))
	})
	countryCls.define("zone_identifiers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return strSliceToArray(object.Kind[*Country](self).c.ZoneIdentifiers())
	})
}

// tzNestedClass creates a TZInfo::<name> class, registering it both as a nested
// constant of mod and under its qualified name in the top-level table so classOf
// can resolve it for native value dispatch.
func (vm *VM) tzNestedClass(mod *RClass, name string) *RClass {
	c := newClass("TZInfo::"+name, vm.cObject)
	mod.consts[name] = object.Wrap(c)
	vm.consts["TZInfo::"+name] = object.Wrap(c)
	return c
}

// registerTZInfoErrors installs the TZInfo error tree mirroring the gem
// (InvalidTimezoneIdentifier / InvalidCountryCode / AmbiguousTime /
// PeriodNotFound < StandardError). Each is registered scoped and flat exactly as
// the JSON:: classes are.
func (vm *VM) registerTZInfoErrors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = object.Wrap(c)
		vm.consts[qualified] = object.Wrap(c)
		return c
	}
	reg("InvalidTimezoneIdentifier", "TZInfo::InvalidTimezoneIdentifier", std)
	reg("InvalidCountryCode", "TZInfo::InvalidCountryCode", std)
	reg("AmbiguousTime", "TZInfo::AmbiguousTime", std)
	reg("PeriodNotFound", "TZInfo::PeriodNotFound", std)
}
