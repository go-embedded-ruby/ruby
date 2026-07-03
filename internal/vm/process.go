// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// processClockEpoch anchors CLOCK_MONOTONIC at the first reading so the value is
// a small, ever-increasing duration like MRI's monotonic clock (which is not
// tied to the wall clock). Go's time is internally monotonic but does not expose
// a raw counter, so subtracting a fixed start is the faithful equivalent.
var (
	processClockEpoch = time.Now()
	processMonoNow    = time.Now // seam for deterministic tests
)

// registerProcess installs the Process module — the subset of MRI's Process that
// Puppet touches at boot and on the local-apply path: identity queries (pid,
// ppid, uid/euid/gid/egid, groups), the clock_gettime timer with its CLOCK_*
// constants, and the maxgroups accessor. Methods that need a real fork/exec
// model are deliberately left out; this is identity + timing, all CGO=0 via Go's
// os and time packages.
func (vm *VM) registerProcess() {
	mod := newClass("Process", nil)
	mod.isModule = true
	vm.consts["Process"] = object.Wrap(mod)
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	def("pid", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(os.Getpid()))
	})
	def("ppid", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(os.Getppid()))
	})
	def("uid", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(processUID()))
	})
	def("euid", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(processEUID()))
	})
	def("gid", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(processGID()))
	})
	def("egid", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(processEGID()))
	})
	def("groups", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		gids, err := processGroups()
		if err != nil {
			return object.Wrap(object.NewArray())
		}
		elems := make([]object.Value, len(gids))
		for i, g := range gids {
			elems[i] = object.IntValue(int64(g))
		}
		return object.Wrap(object.NewArrayFromSlice(elems))
	})
	// maxgroups is a platform tunable; reading it returns the conventional 16 cap
	// and assigning it is accepted but ignored (the kernel limit is fixed), which
	// matches MRI on the platforms we target. Puppet sets it inside a rescue.
	def("maxgroups", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(processMaxGroups)
	})
	def("maxgroups=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return args[0] // accepted-and-ignored; returns the assigned value
	})

	def("clock_gettime", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		clk := intArg(args[0])
		var d time.Duration
		switch clk {
		case clockMonotonic:
			d = processMonoNow().Sub(processClockEpoch)
		default: // CLOCK_REALTIME and any other clock -> wall clock since the epoch
			now := processMonoNow()
			d = time.Duration(now.UnixNano())
		}
		return clockGettimeUnit(d, args)
	})

	mod.consts["CLOCK_REALTIME"] = object.IntValue(clockRealtime)
	vm.consts["Process::CLOCK_REALTIME"] = object.IntValue(clockRealtime)
	mod.consts["CLOCK_MONOTONIC"] = object.IntValue(clockMonotonic)
	vm.consts["Process::CLOCK_MONOTONIC"] = object.IntValue(clockMonotonic)
}

// Clock identifiers match the Linux/macOS values MRI exposes (CLOCK_REALTIME=0,
// CLOCK_MONOTONIC=6 on Darwin); only the relative ordering matters to callers,
// who pass the constant straight back.
const (
	clockRealtime    = 0
	clockMonotonic   = 6
	processMaxGroups = 16
)

// clockGettimeUnit converts a duration to the requested unit symbol (default
// :float_second), matching Process.clock_gettime's unit argument.
func clockGettimeUnit(d time.Duration, args []object.Value) object.Value {
	unit := "float_second"
	if len(args) > 1 {
		if s, ok := object.KindOK[object.Symbol](args[1]); ok {
			unit = string(s)
		}
	}
	switch unit {
	case "nanosecond":
		return object.IntValue(d.Nanoseconds())
	case "microsecond":
		return object.IntValue(d.Microseconds())
	case "millisecond":
		return object.IntValue(d.Milliseconds())
	case "second":
		return object.IntValue(int64(d.Seconds()))
	case "float_microsecond":
		return object.FloatValue(float64(object.Float(float64(d.Nanoseconds()) / 1e3)))
	case "float_millisecond":
		return object.FloatValue(float64(object.Float(float64(d.Nanoseconds()) / 1e6)))
	default: // float_second
		return object.FloatValue(float64(object.Float(d.Seconds())))
	}
}

// Identity seams over the os package so tests can drive every branch without
// depending on the host's actual uid/gid/groups.
var (
	processUID    = os.Getuid
	processEUID   = os.Geteuid
	processGID    = os.Getgid
	processEGID   = os.Getegid
	processGroups = os.Getgroups
)
