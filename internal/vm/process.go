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
	// maxgroups is a process-local tunable MRI keeps in a static (process.c
	// maxgroups / proc_setmaxgroups): it caps how many gids Process.groups= will
	// accept, and reading it back returns whatever was last assigned. It starts at
	// the conventional 16. Puppet sets it inside a rescue.
	maxgroups := int64(processMaxGroups)
	def("maxgroups", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(maxgroups)
	})
	def("maxgroups=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		maxgroups = vm.procToInt(args[0])
		return args[0]
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

	vm.defineProcessExit(def) // unconditional: Init_process declares these on every platform
	vm.registerProcessPosix(mod, def)
	vm.registerProcessResiduals(mod, def)
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
//
// The pid == self arm is the subject of issue #691 and does NOT reach kill(2):
// rb_f_kill consults the installed disposition and, for a signal Ruby handles
// itself, enqueues it and runs rb_thread_execute_interrupts() before returning,
// so the Ruby-level effect (a trap, a SignalException, an Interrupt) happens
// synchronously inside Process.kill. vm.selfSignal is that arm plus
// rb_signal_exec; it reports false when MRI would really signal the OS.
func (vm *VM) defineProcessKill(def func(string, NativeFn)) {
	def("kill", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2+)", len(args))
		}
		sig := vm.killSignalNumber(args[0])
		self := os.Getpid()
		for _, pv := range args[1:] {
			pid, send := int(vm.procToInt(pv)), sig
			if sig < 0 { // killpg(pgid, sig) == kill(-pgid, sig)
				pid, send = -pid, -sig
			}
			// rb_f_kill takes the self arm only for a POSITIVE signal aimed at this
			// very process; a process-group kill (sig < 0) always goes to the OS.
			if sig > 0 && pid == self && vm.selfSignal(send) {
				continue
			}
			if err := procKill(pid, send); err != nil {
				sysFail(err, "")
			}
		}
		return object.IntValue(int64(len(args) - 1))
	})
}

// defineProcessExit installs Process.exit, Process.exit! and Process.abort.
// process.c Init_process declares all three with rb_define_module_function on
// rb_mProcess, which makes them PUBLIC singleton methods on the module as well
// as private instance methods — so `Process.exit(5)` is a legal spelling where
// rbgo previously reached only Kernel's private copies and raised NoMethodError
// (issue #676's second defect). The bodies are rb_f_exit / rb_f_exit_bang /
// rb_f_abort, the same functions Kernel's copies name, so they share
// exitStatusArg's exit_status_code mapping and raiseSystemExit.
func (vm *VM) defineProcessExit(def func(string, NativeFn)) {
	def("exit", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.raiseSystemExit(vm.exitStatusArg(args, 0), "exit")
	})
	def("exit!", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.raiseSystemExit(vm.exitStatusArg(args, 1), "exit")
	})
	def("abort", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.send(vm.main, "abort", args, blk)
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
		return vm.rlimToUint(v)
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
	if _, big := v.(*object.Bignum); big {
		// Any Bignum reaching here is out of int64 range by construction —
		// object.NormInt demotes everything that fits — and NUM2INT reports that as
		// a RangeError, never as the TypeError a non-integer gets.
		raise("RangeError", "bignum too big to convert into 'long'")
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

// rlimToUint is NUM2RLIM. rlim_t is UNSIGNED and 64 bits wide on every platform
// MRI builds for — configure.ac's RUBY_REPLACE_TYPE(rlim_t, ...) resolves it to
// "unsigned long long", so RLIM2NUM is ULL2NUM and NUM2RLIM is NUM2ULL — and
// ULL2NUM of a value above LONG_MAX is a BIGNUM, not a Fixnum. That is not a
// corner case: RLIM_INFINITY is ~0 on Linux, so process.c's own example reads
//
//	Process.getrlimit(:CORE) # => [0, 18446744073709551615]
//
// and the getrlimit -> setrlimit round trip the specs make (and that
// Process.setrlimit(:CORE, *Process.getrlimit(:CORE)) is) hands this function a
// Bignum on Linux where it hands it a Fixnum on Darwin. Both have to arrive as
// the same 64 bits. A negative limit wraps, as NUM2ULL's cast does — MRI takes
// setrlimit(:CORE, -1, -1) all the way to the kernel, which answers EPERM.
func (vm *VM) rlimToUint(v object.Value) uint64 {
	if b, ok := v.(*object.Bignum); ok {
		if !b.I.IsUint64() {
			raise("RangeError", "bignum too big to convert into 'unsigned long long'")
		}
		return b.I.Uint64()
	}
	return uint64(vm.procToInt(v))
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
		// The errno's own text, not the wrapper's: an os.PathError would otherwise
		// repeat the operation and the path that op already names.
		msg = errnoMessage(eno)
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

// ---------------------------------------------------------------------------
// Process identity modules, CPU times, clock resolution and the small residuals
//
// Read for this: ruby/ruby v3_4_0 process.c — rb_proc_times and the
// rb_cProcessTms Struct, rb_clock_getres (the documented
// GETTIMEOFDAY_BASED_CLOCK_REALTIME / TIME_BASED_CLOCK_REALTIME /
// GETRUSAGE_BASED_CLOCK_PROCESS_CPUTIME_ID resolutions), proc_setproctitle and
// rb_proc_warmup, proc_setmaxgroups / proc_setgroups, and the
// Process::UID / Process::GID / Process::Sys module definitions at the foot of
// InitVM_process.

// registerProcessResiduals installs the rest of the Process surface: the
// identity modules MRI defines beside Process itself, Process.times and its
// Process::Tms, Process.clock_getres, Process.argv0, and the small accessors.
func (vm *VM) registerProcessResiduals(mod *RClass, def func(string, NativeFn)) {
	vm.registerProcessIdentityModules(mod)
	vm.registerProcessTms(mod, def)

	// clock_getres(clock, unit = :float_second). The three documented symbolic
	// clocks have fixed resolutions; the numeric clocks report the 1µs this
	// host's clock_getres(2) does, which is what MRI answers on Linux and Darwin.
	def("clock_getres", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		res := time.Microsecond
		if sym, ok := args[0].(object.Symbol); ok && string(sym) == "TIME_BASED_CLOCK_REALTIME" {
			res = time.Second
		}
		return clockGettimeUnit(res, args)
	})

	// argv0 is the name the main script was given, frozen, and the SAME object on
	// every call — process.c keeps rb_progname in a single VALUE.
	var argv0 object.Value
	def("argv0", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if argv0 == nil {
			argv0 = object.NewFrozenStringView(vm.scriptName)
		}
		return argv0
	})

	// setproctitle records the title and answers with it. Rewriting the real
	// process title needs the argv area the C runtime owns, which a Go program
	// cannot reach, so `ps` keeps showing the original command — the return value
	// and the fact that $0 is left alone are what this reproduces.
	def("setproctitle", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return args[0]
	})

	// warmup asks the VM to prepare for a steady-state workload; every part of it
	// (a compaction, a heap preallocation) is implementation-specific, and MRI
	// documents other implementations making it a no-op that answers true.
	def("warmup", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(true)
	})
}

// registerProcessIdentityModules installs Process::UID, Process::GID and
// Process::Sys — the three modules MRI defines beside Process for the same
// identity queries under their POSIX names. Only the queries are provided: the
// privilege switches they also carry would change this interpreter's own
// credentials, which a spec run must never do to the shell it was started from.
func (vm *VM) registerProcessIdentityModules(mod *RClass) {
	sub := func(name string, methods map[string]func() int) *RClass {
		m := newClass("Process::"+name, nil)
		m.isModule = true
		m.named = true
		mod.consts[name] = m
		vm.consts["Process::"+name] = m
		for mname, fn := range methods {
			get := fn
			m.smethods[mname] = &Method{name: mname, owner: m,
				native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
					return object.IntValue(int64(get()))
				}}
		}
		return m
	}
	sub("UID", map[string]func() int{"rid": processUID, "eid": processEUID})
	sub("GID", map[string]func() int{"rid": processGID, "eid": processEGID})
	sub("Sys", map[string]func() int{
		"getuid": processUID, "geteuid": processEUID,
		"getgid": processGID, "getegid": processEGID,
	})
}

// registerProcessTms installs Process::Tms and Process.times. Tms is a Struct in
// MRI (rb_struct_define "utime", "stime", "cutime", "cstime"); here it is a
// plain class with the same four readers and writers, built so
// Process::Tms.new(a, b, c, d) and Process::Tms.new both work.
func (vm *VM) registerProcessTms(mod *RClass, def func(string, NativeFn)) {
	tms := newClass("Tms", vm.cObject)
	tms.name, tms.named = "Process::Tms", true
	mod.consts["Tms"] = tms
	vm.consts["Process::Tms"] = tms
	fields := []string{"utime", "stime", "cutime", "cstime"}
	tms.smethods["new"] = &Method{name: "new", owner: tms, native: func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := &RObject{class: self.(*RClass), ivars: map[string]object.Value{}}
		for i, f := range fields {
			o.ivars["@"+f] = object.NilV
			if i < len(args) {
				o.ivars["@"+f] = args[i]
			}
		}
		return o
	}}
	for _, f := range fields {
		name := f
		tms.define(name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return self.(*RObject).ivars["@"+name]
		})
		tms.define(name+"=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			self.(*RObject).ivars["@"+name] = args[0]
			return args[0]
		})
	}

	// times reports this process's accumulated CPU time. The child fields are
	// zero: a child of this VM runs inside it, so its CPU time is already in the
	// process's own utime/stime rather than credited separately.
	def("times", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		utime, stime := procRusage()
		o := &RObject{class: tms, ivars: map[string]object.Value{}}
		o.ivars["@utime"], o.ivars["@stime"] = object.Float(utime), object.Float(stime)
		o.ivars["@cutime"], o.ivars["@cstime"] = object.Float(0), object.Float(0)
		return o
	})
}
