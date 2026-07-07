// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	stdtime "time"

	gotime "github.com/go-composites/time/src"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// timecop binds github.com/go-ruby-timecop/timecop — a pure-Go reimplementation
// of Ruby's timecop gem — into rbgo. The library provides a controllable Clock
// (freeze / travel / scale, block-scoped nesting and a return-to-baseline
// mechanism); the VM holds one such Clock (vm.clock) that Time.now, Date.today
// and DateTime.now already read (see nowInstant). require "timecop" installs the
// Ruby-level Timecop module registered here, whose class methods drive that
// clock, so mocking time from Ruby transparently mocks all three constructors.
//
// A program that never requires "timecop" is unaffected: the clock is created
// unmocked (its Now seam is nowUnix, the whole-second determinism seam Time.now
// already honoured), so Current() simply reports the real instant.

// nowInstant is the single current-time source behind Time.now / Date.today /
// DateTime.now: the VM's controllable clock. Unmocked it returns nowUnix's real
// instant; under a Timecop freeze / travel / scale it returns the mocked instant.
func (vm *VM) nowInstant() stdtime.Time { return vm.clock.Current() }

// timeFromInstant wraps a Go instant in the Ruby Time value (whole-second
// resolution, matching Time.now / Time.at), used for the values Timecop.freeze /
// travel / return_to_baseline hand back and for the block-form yield argument.
func timeFromInstant(t stdtime.Time) *Time { return &Time{t: gotime.FromUnix(t.Unix())} }

// timecopInstant resolves the optional time argument of Timecop.freeze / travel
// (index i) to a Go instant. With no argument it is the current mock now; a Time
// or a Date / DateTime is taken as-is; an Integer or Float is that many seconds
// after the current now (matching the gem's Timecop.freeze(seconds) form). Any
// other type raises TypeError.
func (vm *VM) timecopInstant(args []object.Value, i int) stdtime.Time {
	if len(args) <= i {
		return vm.clock.Current()
	}
	switch v := args[i].(type) {
	case *Time:
		return stdtime.Unix(v.t.ToUnix(), 0).UTC()
	case *Date:
		return dateInstant(v)
	case object.Integer:
		return vm.clock.Current().Add(stdtime.Duration(int64(v)) * stdtime.Second)
	case object.Float:
		return vm.clock.Current().Add(stdtime.Duration(float64(v) * float64(stdtime.Second)))
	}
	raise("TypeError", "no implicit conversion of %s into Time", args[i].Inspect())
	return stdtime.Time{}
}

// dateInstant reads a Ruby Date / DateTime value's calendar fields and wall clock
// into a Go instant, honouring its UTC offset (a plain Date has a zero time-of-
// day and offset, so it maps to that day's midnight UTC).
func dateInstant(d *Date) stdtime.Time {
	loc := stdtime.FixedZone("", d.d.Offset())
	return stdtime.Date(d.d.Year(), stdtime.Month(d.d.Month()), d.d.Day(),
		d.d.Hour(), d.d.Min(), d.d.Sec(), int(d.d.SecFractionNanos()), loc).UTC()
}

// timecopFactor reads Timecop.scale's leading factor argument as a float,
// raising TypeError for a non-numeric value.
func timecopFactor(args []object.Value) float64 {
	f, ok := toFloat(args[0])
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Float", args[0].Inspect())
	}
	return f
}

// registerTimecop installs the Ruby Timecop module (require "timecop"). Each
// class method drives vm.clock; the block forms run the Ruby block inline under
// the GVL through the library's With* helpers, which pop the mock-time frame on
// normal exit AND on a raised Ruby exception (their deferred pop unwinds the Go
// panic raise uses), then re-propagate it — so the clock is always restored.
func (vm *VM) registerTimecop() {
	mod := newClass("Timecop", nil)
	mod.isModule = true
	vm.consts["Timecop"] = mod

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// Timecop.freeze([t]) / freeze([t]) { |frozen| … } — pin time at t (default
	// now). The block form freezes for the block's dynamic extent, yields the
	// frozen Time and restores the prior clock afterwards, returning the block's
	// value. The non-block form returns the frozen Time.
	sm("freeze", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		t := vm.timecopInstant(args, 0)
		if blk != nil {
			return vm.timecopBlock(blk, t, func(fn func()) { vm.clock.WithFrozen(t, fn) })
		}
		vm.clock.Freeze(t)
		return timeFromInstant(t)
	})

	// Timecop.travel([t]) / travel([t]) { … } — jump to t (default now) and keep
	// ticking at 1×.
	sm("travel", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		t := vm.timecopInstant(args, 0)
		if blk != nil {
			return vm.timecopBlock(blk, t, func(fn func()) { vm.clock.WithTravel(t, fn) })
		}
		vm.clock.Travel(t)
		return timeFromInstant(t)
	})

	// Timecop.scale(factor[, t]) / scale(factor[, t]) { … } — from t (default now)
	// run time at factor× real speed.
	sm("scale", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		factor := timecopFactor(args)
		t := vm.timecopInstant(args, 1)
		if blk != nil {
			return vm.timecopBlock(blk, t, func(fn func()) { vm.clock.WithScale(factor, t, fn) })
		}
		vm.clock.Scale(factor, t)
		return timeFromInstant(t)
	})

	// Timecop.return / return { … } — unwind every mock-time frame back to real
	// time. The block form restores real time only for the block's extent, then
	// re-establishes the prior mock stack.
	sm("return", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			var ret object.Value = object.NilV
			vm.clock.WithReturn(func() { ret = vm.callBlock(blk, nil) })
			return ret
		}
		vm.clock.Return()
		return object.NilV
	})

	// Timecop.baseline = t — record a baseline instant (a travel frame at t) that
	// return_to_baseline unwinds to. Returns its argument, as an assignment does.
	sm("baseline=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.clock.SetBaseline(vm.timecopInstant(args, 0))
		return args[0]
	})

	// Timecop.return_to_baseline — unwind every nested frame down to the baseline;
	// returns the baseline Time now in effect.
	sm("return_to_baseline", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return timeFromInstant(vm.clock.ReturnToBaseline())
	})

	// State predicates: frozen? / travelled? / scaled?.
	sm("frozen?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.clock.Frozen())
	})
	sm("travelled?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.clock.Travelled())
	})
	sm("scaled?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.clock.Scaled())
	})
}

// timecopBlock runs a Timecop block form. with is the matching library helper
// (WithFrozen/WithTravel/WithScale) bound over its target, taking the frame body
// as its argument: it pushes the mock-time frame, runs the body, then pops it —
// on a normal return and, because raise panics, on a raised exception too (its
// deferred pop unwinds the Go panic). The body runs the Ruby block, yielding the
// target Time; timecopBlock returns the block's value.
func (vm *VM) timecopBlock(blk *Proc, t stdtime.Time, with func(fn func())) object.Value {
	var ret object.Value = object.NilV
	with(func() { ret = vm.callBlock(blk, []object.Value{timeFromInstant(t)}) })
	return ret
}
