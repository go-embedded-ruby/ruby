// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math"
	"math/big"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

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
	vm.consts["Process"] = mod
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
			return object.NewArray()
		}
		elems := make([]object.Value, len(gids))
		for i, g := range gids {
			elems[i] = object.IntValue(int64(g))
		}
		return object.NewArrayFromSlice(elems)
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

	vm.registerProcessPosix(mod, def)
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
		if s, ok := args[1].(object.Symbol); ok {
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
		return object.Float(float64(d.Nanoseconds()) / 1e3)
	case "float_millisecond":
		return object.Float(float64(d.Nanoseconds()) / 1e6)
	default: // float_second
		return object.Float(d.Seconds())
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

// ---------------------------------------------------------------------------
// Resource limits, scheduling priority, process groups, sessions and signals
//
// The platform-bound half (the RLIMIT_ table and the syscall wrappers) lives in
// spawn_native.go; everything here is the portable argument peeling and error
// shaping, so it is exercised identically on every POSIX lane.
//
// Read for this: ruby/ruby v3_4_0 process.c — rlimit_resource_name2int,
// rlimit_resource_type, rlimit_resource_value, proc_getrlimit, proc_setrlimit,
// proc_getpgid/proc_setpgid/proc_getsid, proc_getpriority/proc_setpriority,
// and signal.c signm2signo / rb_f_kill.

// registerProcessPosix installs the constants and methods that need a POSIX
// kernel. The constants are always defined from whatever the platform table
// holds (empty off POSIX, so Process::RLIMIT_CORE simply does not exist there);
// the methods only when the platform has them, mirroring MRI, where an absent
// getrlimit(2) turns Process.getrlimit into rb_f_notimplement and
// Process.respond_to?(:getrlimit) into false.
func (vm *VM) registerProcessPosix(mod *RClass, def func(string, NativeFn)) {
	setConst := func(name string, v object.Value) {
		mod.consts[name] = v
		vm.consts["Process::"+name] = v
	}
	for name, res := range rlimitResources {
		setConst("RLIMIT_"+name, object.IntValue(int64(res)))
	}
	for name, lim := range rlimitValues {
		setConst("RLIM_"+name, rlimValue(lim))
	}
	for name, which := range prioTargets {
		setConst("PRIO_"+name, object.IntValue(int64(which)))
	}
	if procPosix {
		vm.defineProcessLimits(def)
		vm.defineProcessGroups(def)
		vm.defineProcessKill(def)
	}
}

// defineProcessLimits installs Process.getrlimit / setrlimit / getpriority /
// setpriority.
func (vm *VM) defineProcessLimits(def func(string, NativeFn)) {
	// getrlimit(resource) -> [cur, max]; proc_getrlimit coerces the resource
	// through rlimit_resource_type and fails with rb_sys_fail("getrlimit").
	def("getrlimit", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		cur, max, err := procGetrlimit(vm.rlimitResourceType(args[0]))
		if err != nil {
			sysFail(err, "getrlimit")
		}
		return object.NewArray(rlimValue(cur), rlimValue(max))
	})
	// setrlimit(resource, cur, max = cur) -> nil. proc_setrlimit converts BOTH
	// limits before the resource (rlimit_resource_value runs first in the C), and
	// an omitted or nil max repeats cur.
	def("setrlimit", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 || len(args) > 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
		}
		cur := vm.rlimitResourceValue(args[1])
		max := cur
		if len(args) == 3 && !object.IsNil(args[2]) {
			max = vm.rlimitResourceValue(args[2])
		}
		if err := procSetrlimit(vm.rlimitResourceType(args[0]), cur, max); err != nil {
			sysFail(err, "setrlimit")
		}
		return object.NilV
	})
	// getpriority(kind, id) -> int and setpriority(kind, id, priority) -> 0,
	// straight NUM2INT coercions over getpriority(2)/setpriority(2).
	def("getpriority", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		prio, err := procGetpriority(int(vm.procToInt(args[0])), int(vm.procToInt(args[1])))
		if err != nil && getpriorityFailed(prio, err) {
			sysFail(err, "")
		}
		return object.IntValue(int64(prio))
	})
	def("setpriority", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
		}
		err := procSetpriority(int(vm.procToInt(args[0])), int(vm.procToInt(args[1])), int(vm.procToInt(args[2])))
		if err != nil {
			sysFail(err, "")
		}
		return object.IntValue(0)
	})
}

// defineProcessGroups installs the process-group and session accessors.
// Process.setsid is NOT among them: it would detach the interpreter from its
// controlling terminal, which is not something a spec run may do to its own
// shell, so the existing accepted-and-ignored stand-in is kept.
func (vm *VM) defineProcessGroups(def func(string, NativeFn)) {
	def("getpgid", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		pgid, err := procGetpgid(int(vm.procToInt(args[0])))
		if err != nil {
			sysFail(err, "")
		}
		return object.IntValue(int64(pgid))
	})
	// getpgrp takes no argument and is getpgid(0).
	def("getpgrp", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		pgid, err := procGetpgid(0)
		if err != nil {
			sysFail(err, "")
		}
		return object.IntValue(int64(pgid))
	})
	// getsid(pid = 0) -> sid.
	def("getsid", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		pid := 0
		if len(args) > 0 && !object.IsNil(args[0]) {
			pid = int(vm.procToInt(args[0]))
		}
		sid, err := procGetsid(pid)
		if err != nil {
			sysFail(err, "")
		}
		return object.IntValue(int64(sid))
	})
	// setpgid(pid, pgid) -> 0, and setpgrp, which is setpgid(0, 0).
	def("setpgid", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		if err := procSetpgid(int(vm.procToInt(args[0])), int(vm.procToInt(args[1]))); err != nil {
			sysFail(err, "")
		}
		return object.IntValue(0)
	})
	def("setpgrp", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := procSetpgid(0, 0); err != nil {
			sysFail(err, "")
		}
		return object.IntValue(0)
	})
}

// defineProcessKill installs Process.kill(signal, pid, ...) -> count.
// signal.c rb_f_kill: at least two arguments; a Fixnum signal is used as-is,
// anything else goes through signm2signo (which accepts a leading '-' to mean
// "signal the process group"); every remaining argument is a pid coerced with
// NUM2PIDT, and the return value is the number of pids signalled.
func (vm *VM) defineProcessKill(def func(string, NativeFn)) {
	def("kill", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2+)", len(args))
		}
		sig := vm.killSignalNumber(args[0])
		for _, pv := range args[1:] {
			pid, send := int(vm.procToInt(pv)), sig
			if sig < 0 { // killpg(pgid, sig) == kill(-pgid, sig)
				pid, send = -pid, -sig
			}
			if err := procKill(pid, send); err != nil {
				sysFail(err, "")
			}
		}
		return object.IntValue(int64(len(args) - 1))
	})
}

// rlimValue wraps a rlim_t as a Ruby Integer. RLIM_INFINITY is ~0 on Linux,
// which does not fit in an int64, so an out-of-range limit promotes to a Bignum
// rather than wrapping negative (MRI's RLIM2NUM does the same through
// ULONG2NUM).
func rlimValue(lim uint64) object.Value {
	if lim <= math.MaxInt64 {
		return object.IntValue(int64(lim))
	}
	return object.NormInt(new(big.Int).SetUint64(lim))
}

// rlimitResourceType coerces a resource designator to this host's resource
// number (process.c rlimit_resource_type). A Symbol, a String, or an object with
// #to_str names the resource; the name must be exactly the uppercase short form,
// since rlimit_resource_name2int matches case-insensitively and then rejects
// anything that is not all-uppercase. Anything else is NUM2INT.
func (vm *VM) rlimitResourceType(v object.Value) int {
	name, named := vm.rlimitName(v)
	if !named {
		return int(vm.procToInt(v))
	}
	if res, ok := rlimitResources[name]; ok {
		return res
	}
	raise("ArgumentError", "invalid resource name: %s", name)
	return 0
}

// rlimitResourceValue coerces a limit to a rlim_t (process.c
// rlimit_resource_value): the symbolic names INFINITY / SAVED_MAX / SAVED_CUR
// (matched exactly, by strcmp), otherwise NUM2RLIM.
func (vm *VM) rlimitResourceValue(v object.Value) uint64 {
	name, named := vm.rlimitName(v)
	if !named {
		return uint64(vm.procToInt(v))
	}
	if lim, ok := rlimitValues[name]; ok {
		return lim
	}
	raise("ArgumentError", "invalid resource value: %s", name)
	return 0
}

// rlimitName returns the name a resource/limit designator carries, and named =
// false when it carries none and the caller must fall through to NUM2INT. An
// Integer never reaches #to_str: process.c lists T_FIXNUM/T_BIGNUM as their own
// switch cases, ahead of the rb_check_string_type fallback.
func (vm *VM) rlimitName(v object.Value) (name string, named bool) {
	switch s := v.(type) {
	case object.Symbol:
		return string(s), true
	case *object.String:
		return s.Str(), true
	}
	if _, isInt := object.BigOf(v); isInt {
		return "", false
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Str(), true
		}
	}
	return "", false
}

// procToInt is NUM2INT/NUM2PIDT: an Integer passes through, nil raises the
// dedicated TypeError the C macro produces, and anything else must answer
// #to_int with an Integer (rb_to_int).
func (vm *VM) procToInt(v object.Value) int64 {
	if i, ok := v.(object.Integer); ok {
		return int64(i)
	}
	if object.IsNil(v) {
		raise("TypeError", "no implicit conversion from nil to integer")
	}
	if !vm.respondsToDynamic(v, "to_int") {
		raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
	}
	r := vm.send(v, "to_int", nil, nil)
	i, ok := r.(object.Integer)
	if !ok {
		raise("TypeError", "can't convert %s to Integer (%s#to_int gives %s)",
			classNameOf(v), classNameOf(v), classNameOf(r))
	}
	return int64(i)
}

// errnoClasses inverts errnoNumbers so a failed syscall can name its Errno::Exxx
// class. Names are folded in sorted order so a platform where two names share a
// number (EAGAIN/EWOULDBLOCK on some kernels) always picks the same one.
var errnoClasses = func() map[int64]string {
	names := make([]string, 0, len(errnoNumbers))
	for name := range errnoNumbers {
		names = append(names, name)
	}
	sort.Strings(names)
	m := make(map[int64]string, len(names))
	for _, name := range names {
		m[errnoNumbers[name]] = name
	}
	return m
}()

// sysFail raises the exception MRI's rb_sys_fail(op) would: the Errno::Exxx
// subclass for the failed call's errno, with the message "<strerror> - <op>" (or
// the bare strerror when op is empty, which is rb_sys_fail(0) — what rb_f_kill
// uses). An errno with no registered class falls back to SystemCallError, so an
// exotic failure is still a rescuable SystemCallError rather than a Go panic.
func sysFail(err error, op string) {
	cls, msg := "SystemCallError", errnoMessage(err)
	var eno syscall.Errno
	if errors.As(err, &eno) {
		if name, ok := errnoClasses[int64(eno)]; ok {
			cls = "Errno::" + name
		}
	}
	if op == "" {
		raise(cls, "%s", msg)
	}
	raise(cls, "%s - %s", msg, op)
}

// killSignalNumber resolves Process.kill's first argument to a signal number,
// following signal.c signm2signo: a Fixnum is taken as-is; a Symbol, String or
// #to_str object names a signal, optionally with a leading '-' (signal the
// process group) and optionally with the "SIG" prefix; anything else is
// "bad signal type <class>". An unknown name is reported with the SIG prefix
// restored, which is what MRI prints for both "FOO" and "SIGFOO".
func (vm *VM) killSignalNumber(v object.Value) int {
	if i, ok := v.(object.Integer); ok {
		return int(i)
	}
	name, named := vm.rlimitName(v) // Symbol / String / #to_str, as signm2signo takes
	if !named {
		raise("ArgumentError", "bad signal type %s", classNameOf(v))
	}
	if strings.ContainsRune(name, 0) {
		raise("ArgumentError", "signal name with null byte")
	}
	negative := strings.HasPrefix(name, "-")
	if negative {
		name = name[1:]
	}
	bare := strings.TrimPrefix(name, "SIG")
	num, ok := signalNumbers[bare]
	if !ok {
		raise("ArgumentError", "unsupported signal 'SIG%s'", bare)
	}
	if negative {
		return -num
	}
	return num
}

// getpriorityFailed reports whether a getpriority(2) result is really a failure.
// The call legitimately returns -1 — a nice value of -1 — which at the Darwin
// libc boundary is indistinguishable from an error return: Go's wrapper decides
// by the return value and then reports whatever errno happened to hold, so
// Process.getpriority(PRIO_USER, uid) on a host whose user priority is -1 comes
// back with a stale, unrelated Errno. MRI has the same call and the same -1, and
// resolves it by clearing errno before the call and testing errno afterwards
// (process.c proc_getpriority); Go cannot read errno, so the next best rule is
// to believe a -1 result only when the reported errno is one getpriority(2) is
// documented to set: ESRCH for an unknown target, EINVAL for an unknown kind,
// EACCES/EPERM for one this process may not read.
func getpriorityFailed(prio int, err error) bool {
	if prio != -1 {
		return true
	}
	var eno syscall.Errno
	if !errors.As(err, &eno) {
		return false
	}
	switch eno {
	case syscall.ESRCH, syscall.EINVAL, syscall.EACCES, syscall.EPERM:
		return true
	}
	return false
}

// errnoMessage renders an error the way C's strerror(3) does, which is the text
// MRI's rb_sys_fail embeds: Go's errno strings are the same table with a
// lowercased first letter ("no such process" against "No such process"), so the
// only difference to undo is that letter.
func errnoMessage(err error) string {
	msg := []byte(err.Error())
	if len(msg) > 0 {
		msg[0] = byte(unicode.ToUpper(rune(msg[0])))
	}
	return string(msg)
}
