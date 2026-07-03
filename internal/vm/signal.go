// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerSignal installs the Signal module and Kernel#trap. A program installs
// a handler (Signal.trap(:INT) { ... }) and the VM records it, returning the
// previous handler the way MRI does ("DEFAULT" the first time, then the prior
// Proc/String). The embedded host does not yet wire OS signals to the recorded
// handlers — delivery is the scheduler's job in a later round — but registration
// must succeed because CLI applications such as `puppet apply` trap :INT during
// setup. Signal.list maps the common signal names to their numbers and
// Signal.signame inverts a number, both as plain lookups.
func (vm *VM) registerSignal() {
	mod := newClass("Signal", nil)
	mod.isModule = true
	vm.consts["Signal"] = mod

	// handlers records the most recently installed handler per normalised signal
	// name so trap can report the previous one. It lives on the VM-shared module
	// object via an ivar so a single map is reused.
	handlers := map[string]object.Value{}

	trap := func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			return raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		name := signalName(args[0])
		prev, had := handlers[name]
		if !had {
			prev = object.NewString("DEFAULT")
		}
		switch {
		case blk != nil:
			handlers[name] = blk // a Proc is itself a Value, returned as the previous handler later
		case len(args) >= 2:
			handlers[name] = args[1] // "DEFAULT" / "IGNORE" / "SIG_IGN" / a command string
		default:
			handlers[name] = object.NewString("DEFAULT")
		}
		return prev
	}

	mod.smethods["trap"] = &Method{name: "trap", owner: mod, native: trap}

	// Signal.list returns the {name => number} table; Signal.signame inverts it.
	mod.smethods["list"] = &Method{name: "list", owner: mod,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			h := object.NewHash()
			for name, num := range signalNumbers {
				h.Set(object.NewString(name), object.IntValue(int64(num)))
			}
			return h
		}}
	mod.smethods["signame"] = &Method{name: "signame", owner: mod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				return object.NilV
			}
			n, ok := object.AsIntegerOK(args[0])
			if !ok {
				return object.NilV
			}
			for name, num := range signalNumbers {
				if int64(num) == int64(n) {
					return object.NewString(name)
				}
			}
			return object.NilV
		}}

	// Kernel#trap is the same operation reachable without the Signal receiver.
	vm.cObject.define("trap", trap)
}

// signalNumbers maps the portable signal names to their POSIX numbers. The set
// covers the signals a Ruby program is likely to trap; it is not exhaustive.
var signalNumbers = map[string]int{
	"HUP": 1, "INT": 2, "QUIT": 3, "ILL": 4, "TRAP": 5, "ABRT": 6,
	"FPE": 8, "KILL": 9, "SEGV": 11, "PIPE": 13, "ALRM": 14, "TERM": 15,
	"USR1": 30, "USR2": 31, "CHLD": 20, "CONT": 19, "STOP": 17, "TSTP": 18,
	"WINCH": 28, "INFO": 29,
}

// signalName normalises a signal designator to its bare name (no "SIG" prefix),
// accepting a Symbol, a String ("INT"/"SIGINT") or an Integer.
func signalName(v object.Value) string {
	{
		__sw159 := v
		switch {
		case object.IsKind[object.Symbol](__sw159):
			s := object.Kind[object.Symbol](__sw159)
			_ = s
			return stripSIG(string(s))
		case object.IsKind[*object.String](__sw159):
			s := object.Kind[*object.String](__sw159)
			_ = s
			return stripSIG(string(s.Bytes()))
		case object.IsInt(__sw159):
			s := object.AsInteger(__sw159)
			_ = s
			for name, num := range signalNumbers {
				if int64(num) == int64(s) {
					return name
				}
			}
			return v.ToS()
		default:
			s := __sw159
			_ = s
			return v.ToS()
		}
	}
}

func stripSIG(name string) string {
	if len(name) > 3 && name[:3] == "SIG" {
		return name[3:]
	}
	return name
}
