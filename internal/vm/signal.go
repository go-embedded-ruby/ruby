// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"syscall"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// trapKind is what the VM does when a signal is delivered, mirroring the values
// MRI stores in vm->trap_list.cmd[sig] (signal.c trap_handler):
//
//	0       (absent)         -> the signal's default Ruby action
//	Qtrue   ("IGNORE"/"")    -> discard
//	Qundef  ("EXIT")         -> raise SystemExit
//	0 + SIG_DFL ("SYSTEM_DEFAULT") -> hand the signal back to the OS
//	a Proc / command String  -> run it
type trapKind int

const (
	trapDefault       trapKind = iota // no handler installed: default_handler(sig)
	trapIgnore                        // "IGNORE" / "SIG_IGN" / ""
	trapNil                           // trap(sig, nil): ignores, and reports back nil
	trapExit                          // "EXIT"
	trapSystemDefault                 // "SYSTEM_DEFAULT": SIG_DFL, the OS acts
	trapCommand                       // a Proc (or a command String) to run
)

// trapCmd is one entry of the VM's trap list.
type trapCmd struct {
	kind trapKind
	cmd  object.Value // the Proc/String for trapCommand; the value #trap reports back
}

// signalManaged reports whether MRI installs its own sighandler for sig, which
// is signal.c default_handler's first case group (and the same set Init_signal
// passes to install_sighandler). It is exactly the set rb_signal_exec turns into
// a Ruby exception when no user handler is installed, and — because
// signal_ignored() then sees MRI's own handler rather than SIG_DFL — the set
// rb_f_kill routes through signal_enque instead of kill(2) when the target is
// this process.
func (vm *VM) signalManaged(sig int) bool {
	switch sig {
	case sigNumber("INT"), sigNumber("HUP"), sigNumber("QUIT"), sigNumber("TERM"),
		sigNumber("ALRM"), sigNumber("USR1"), sigNumber("USR2"), sigNumber("CHLD"):
		return true
	}
	return false
}

// signalFatal reports whether rb_f_kill delivers sig to this process for real
// even when the target is self: SIGSEGV, SIGBUS, SIGKILL, SIGILL, SIGFPE and
// SIGSTOP cannot be deferred to a Ruby safe point, so signal.c's rb_f_kill
// calls kill(2) directly for them ("switch (sig)" inside the pid == self arm).
func signalFatal(sig int) bool {
	switch sig {
	case sigNumber("SEGV"), sigNumber("BUS"), sigNumber("KILL"),
		sigNumber("ILL"), sigNumber("FPE"), sigNumber("STOP"):
		return true
	}
	return false
}

// signalReserved is signal.c reserved_signal_p: the signals MRI refuses to let a
// program trap at all, because it needs its own handlers for them. SEGV, BUS,
// ILL and FPE are synchronous and cannot be delivered to the main thread, and
// VTALRM is the interrupt the thread scheduler itself runs on.
func signalReserved(sig int) bool {
	switch sig {
	case sigNumber("SEGV"), sigNumber("BUS"), sigNumber("ILL"),
		sigNumber("FPE"), sigNumber("VTALRM"):
		return true
	}
	return false
}

// signalUntrappable are the two signals sigaction(2) refuses outright, so MRI's
// trap() -> ruby_signal() gets SIG_ERR and calls rb_sys_fail_str(name), raising
// Errno::EINVAL rather than ArgumentError. POSIX fixes both numbers, so this
// needs no host table.
func signalUntrappable(sig int) bool {
	return sig == sigNumber("KILL") || sig == sigNumber("STOP")
}

// registerSignal installs the Signal module, Kernel#trap, and the SignalException
// / Interrupt methods that carry a signal's identity (#signo, #signm).
//
// The disposition a program installs is recorded in vm.trapList, MRI's
// vm->trap_list.cmd[] (signal.c trap/trap_handler), and consulted by
// Process.kill when the target is this process — see vm.selfSignal, which is
// signal.c rb_f_kill's pid == self arm plus rb_signal_exec. The VM deliberately
// does NOT install an OS signal handler and never calls os/signal.Notify: the
// Go runtime stays the sole owner of signal disposition. See vm.selfSignal for
// why that is the faithful reading of the C rather than a shortcut.
func (vm *VM) registerSignal() {
	mod := newClass("Signal", nil)
	mod.isModule = true
	vm.consts["Signal"] = mod
	vm.trapList = map[int]trapCmd{}

	trap := func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			return raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		sig := vm.trapSignm(args[0])
		// sig_trap refuses a reserved signal before it looks at the handler.
		if signalReserved(sig) {
			// Every signal signalReserved names has a siglist entry, so unlike
			// sig_trap (whose NSIG table can be sparser than its handler set) there
			// is no numeric spelling of this message to fall back to.
			name, _ := signalNameOf(sig)
			return raise("ArgumentError", "can't trap reserved signal: SIG%s", name)
		}
		// SIGKILL and SIGSTOP cannot be caught or ignored: sigaction(2) fails
		// EINVAL, which trap() turns into Errno::EINVAL through rb_sys_fail_str.
		if signalUntrappable(sig) {
			sysFail(syscall.EINVAL, signoToSignm(sig))
		}
		var next trapCmd
		switch {
		case blk != nil:
			next = trapCmd{kind: trapCommand, cmd: blk}
		case len(args) >= 2:
			next = vm.trapHandler(args[1])
		default:
			return raise("ArgumentError", "tried to create Proc object without a block")
		}
		prev := vm.trapPrevious(sig)
		vm.trapList[sig] = next
		return prev
	}

	mod.smethods["trap"] = &Method{name: "trap", owner: mod, native: trap}

	// Signal.list returns the {name => number} table. MRI's sig_list walks the
	// same siglist it uses everywhere else, so EXIT (0) is a member — a fact
	// core/signal/list_spec.rb asserts directly.
	mod.smethods["list"] = &Method{name: "list", owner: mod,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			h := object.NewHash()
			for _, e := range siglist {
				h.Set(object.NewString(e.name), object.IntValue(int64(e.signo)))
			}
			return h
		}}
	// Signal.signame inverts the table. MRI's sig_signame takes NUM2INT — so a
	// non-Integer goes through #to_int and a #to_int that returns a non-Integer is
	// a TypeError — and signo2signm returns NULL (nil) for a number no entry
	// carries. signo2signm returns the FIRST siglist entry with that number, which
	// is why siglist lists a canonical name ahead of its aliases.
	mod.smethods["signame"] = &Method{name: "signame", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) != 1 {
				return raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
			}
			n := vm.toIntCoerce(args[0])
			if name, ok := signalNameOf(int(n)); ok {
				return object.NewString(name)
			}
			return object.NilV
		}}

	// Kernel#trap is the same operation reachable without the Signal receiver.
	vm.cObject.define("trap", trap)

	vm.registerSignalException()
}

// registerSignalException adds the identity methods SignalException carries.
// signal.c Init_signal defines #initialize (esignal_init), #signo
// (esignal_signo) and aliases #signm to #message; Interrupt overrides
// #initialize (interrupt_init) to fix the signal at SIGINT.
func (vm *VM) registerSignalException() {
	// A hard assertion: builtins.go builds the exception hierarchy before it calls
	// registerSignal, so an absent SignalException is a broken registration order,
	// not a state to carry on from quietly.
	cSig := vm.consts["SignalException"].(*RClass)
	// esignal_init(sig) / esignal_init(signo, message = signo2signm(signo)):
	// the first argument is tried as an Integer through #to_int; if it converts,
	// a second argument may supply the message and arity is 1..2, otherwise the
	// argument is a signal NAME and arity is exactly 1. A name is stored with a
	// "SIG" prefix when it was given without one (`sig = "SIG" + sig` when the
	// matched prefix length differs from signame_prefix_len), which is why
	// SignalException.new("TERM").message is "SIGTERM".
	cSig.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o := self.(*RObject)
		if len(args) == 0 {
			return raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var signo int
		var message string
		if n, isInt := vm.toIntMaybe(args[0]); isInt {
			if len(args) > 2 {
				return raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
			}
			signo = int(n)
			if signo < 0 || signo > sigMaxNumber {
				return raise("ArgumentError", "invalid signal number (%d)", signo)
			}
			if len(args) > 1 {
				message = args[1].ToS()
			} else {
				message = signoToSignm(signo)
			}
		} else {
			if len(args) != 1 {
				return raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
			}
			// esignal_init calls signm2signo with exit = FALSE, so "EXIT" is not a
			// signal name here even though Signal.trap(:EXIT) is legal.
			name, named := vm.signalNameArg(args[0])
			if !named {
				return raise("ArgumentError", "bad signal type %s", vm.objClassName(args[0]))
			}
			num, found := signalNumberOfEx(name, false)
			if !found {
				return raise("ArgumentError", "unsupported signal '%s'", withSIG(name))
			}
			signo, message = num, withSIG(name)
		}
		o.ivars["@message"] = object.NewString(message)
		o.ivars["@signo"] = object.IntValue(int64(signo))
		return object.NilV
	})
	cSig.define("signo", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@signo")
	})
	// rb_alias(rb_eSignal, "signm", "message").
	cSig.define("signm", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.send(self, "message", nil, nil)
	})

	// interrupt_init: args[0] = SIGINT, args[1..] = the caller's arguments, so
	// Interrupt.new carries signo 2 and an EMPTY message (MRI passes no name, and
	// esignal_init's message argument defaults to "" rather than "SIGINT" here
	// because interrupt_init supplies argv[1] itself).
	cInt := vm.consts["Interrupt"].(*RClass)
	{
		cInt.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			o := self.(*RObject)
			// Interrupt.new with no argument leaves the message UNSET, so
			// Exception#message falls back to the class name and #signm reads
			// "Interrupt" (core/exception/interrupt_spec.rb). The Interrupt that
			// rb_interrupt() raises is built with an explicit empty message instead,
			// which is why a rescued one reports "" — measured on MRI 4.0.5, where
			// Interrupt.new.message is "Interrupt" and the raised one's is "".
			if len(args) > 0 {
				o.ivars["@message"] = object.NewString(args[0].ToS())
			}
			o.ivars["@signo"] = object.IntValue(int64(sigNumber("INT")))
			return object.NilV
		})
	}
}

// trapSignm coerces Signal.trap's first argument to a signal number, applying
// signal.c trap_signm: a Fixnum is range-checked against NSIG, anything else
// goes through signm2signo with exit = TRUE, so "EXIT" and 0 are accepted.
//
// Note what it does NOT do: trap_signm tests FIXNUM_P, so there is no #to_int
// conversion anywhere on this path — core/signal/trap_spec.rb asserts that
// explicitly with a mock that fails if #to_int is called, and a Float is a
// "bad signal type" rather than a truncated number.
func (vm *VM) trapSignm(v object.Value) int {
	if n, isInt := v.(object.Integer); isInt {
		if n < 0 || int64(n) > int64(sigMaxNumber) {
			raise("ArgumentError", "invalid signal number (%d)", int64(n))
		}
		return int(n)
	}
	return vm.signm2signo(v, true)
}

// signm2signo is signal.c signm2signo with negative = FALSE: a Symbol becomes its
// name, a String is used as is, anything else must answer #to_str (through
// rb_check_string_type, so an object that does not is a "bad signal type" naming
// its CLASS, not a conversion failure), the "SIG" prefix is optional, and a name
// no entry carries is "unsupported signal".
//
// allowExit is the C parameter `exit`: FOREACH_SIGNAL skips the first table entry
// when it is false, and that entry is EXIT — so Signal.trap accepts :EXIT while
// SignalException.new and Process.kill do not.
func (vm *VM) signm2signo(v object.Value, allowExit bool) int {
	name, ok := vm.signalNameArg(v)
	if !ok {
		raise("ArgumentError", "bad signal type %s", vm.objClassName(v))
	}
	if num, found := signalNumberOfEx(name, allowExit); found {
		return num
	}
	raise("ArgumentError", "unsupported signal '%s'", withSIG(name))
	return 0
}

// signalNameArg is signm2signo's argument coercion alone: a Symbol, a String, or
// an object whose #to_str returns one. It reports false for anything else so the
// caller can raise the "bad signal type" ArgumentError naming the class.
func (vm *VM) signalNameArg(v object.Value) (string, bool) {
	switch t := v.(type) {
	case object.Symbol:
		return string(t), true
	case *object.String:
		return string(t.Bytes()), true
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, isStr := vm.send(v, "to_str", nil, nil).(*object.String); isStr {
			return string(s.Bytes()), true
		}
	}
	return "", false
}

// trapHandler maps Signal.trap's second argument to a disposition, following
// signal.c trap_handler's switch on the command string's length exactly: nil and
// "" and "IGNORE"/"SIG_IGN" ignore, "DEFAULT"/"SIG_DFL" restore the signal's
// default RUBY action, "SYSTEM_DEFAULT" restores the OS default, "EXIT" exits,
// and anything else is a command to run.
func (vm *VM) trapHandler(v object.Value) trapCmd {
	if v == object.NilV {
		// trap_handler leaves *cmd as Qnil for a nil handler, and trap()'s oldcmd
		// switch has an explicit `case Qnil: break` that does NOT rewrite it — so a
		// signal whose handler was set to nil reports back nil, not "IGNORE".
		return trapCmd{kind: trapNil, cmd: object.NilV}
	}
	if p, isProc := v.(*Proc); isProc {
		return trapCmd{kind: trapCommand, cmd: p}
	}
	switch name := v.ToS(); name {
	case "", "IGNORE", "SIG_IGN":
		return trapCmd{kind: trapIgnore, cmd: object.NewString("IGNORE")}
	case "DEFAULT", "SIG_DFL":
		return trapCmd{kind: trapDefault, cmd: object.NewString("DEFAULT")}
	case "SYSTEM_DEFAULT":
		return trapCmd{kind: trapSystemDefault, cmd: object.NewString("SYSTEM_DEFAULT")}
	case "EXIT":
		return trapCmd{kind: trapExit, cmd: object.NewString("EXIT")}
	default:
		return trapCmd{kind: trapCommand, cmd: v}
	}
}

// trapPrevious is what Signal.trap returns: signal.c trap()'s oldcmd switch. A
// signal with nothing installed reports the name of the disposition it had —
// "DEFAULT" for a signal MRI handles itself, "SYSTEM_DEFAULT" for one left to
// the OS — rather than nil, and an installed Proc or command comes back as
// itself.
func (vm *VM) trapPrevious(sig int) object.Value {
	// Signal 0 (Signal.trap(:EXIT)) reports nil, not a disposition name: trap()
	// starts with `if (sig == 0) oldfunc = SIG_ERR;`, and SIG_ERR matches none of
	// the three arms of the oldcmd switch's `case 0`, so it falls to Qnil.
	// Measured on MRI 4.0.5: Signal.trap(:EXIT) {} => nil.
	if sig == 0 {
		if prev, had := vm.trapList[0]; had && prev.kind == trapCommand {
			return prev.cmd
		}
		return object.NilV
	}
	// An absent entry IS trapDefault (MRI's trap_list.cmd[sig] == 0), so the
	// zero value of trapCmd already says what to report.
	prev := vm.trapList[sig]
	switch prev.kind {
	case trapDefault:
		if vm.signalManaged(sig) {
			return object.NewString("DEFAULT")
		}
		return object.NewString("SYSTEM_DEFAULT")
	case trapIgnore:
		return object.NewString("IGNORE")
	case trapNil:
		return object.NilV
	case trapSystemDefault:
		return object.NewString("SYSTEM_DEFAULT")
	case trapExit:
		return object.NewString("EXIT")
	}
	return prev.cmd
}

// selfSignal is signal.c rb_f_kill's "target pid is self" arm followed by
// rb_signal_exec, and it is the whole of issue #691.
//
// MRI does NOT send the signal to itself for the ordinary cases. rb_f_kill's
// comment says why — "When target pid is self, many caller assume signal will be
// delivered immediately and synchronously" — so it consults the installed
// disposition and, for a signal it handles itself, calls signal_enque(sig) and
// then rb_thread_execute_interrupts() before returning. The Ruby-level effect
// therefore happens INSIDE Process.kill, on the calling thread, and no OS signal
// is ever raised.
//
// That is what reconciles MRI's model with Go's. MRI installs a C handler
// (Init_signal -> install_sighandler) purely so an ASYNCHRONOUS signal can be
// parked in signal_buff and re-emerge at the next Ruby safe point; the safe point
// for a self-directed kill is the next statement, and signal_enque plus an
// immediate interrupt check expresses it without the handler. Go's runtime owns
// signal disposition and os/signal.Notify changes it process-wide, so
// reproducing MRI by installing a Go handler would buy nothing here and cost the
// asynchronous delivery race that IS the observed defect: the old code called
// kill(2) and then raced the Go runtime's default action against the program's
// own exit, which is why the old behaviour was 15-20% fatal rather than always
// fatal. Executing the disposition synchronously removes the race by
// construction, needs no build tag on a target without signals, and leaves Go's
// disposition untouched.
//
// What it does NOT do, and MRI does: an EXTERNAL signal (one another process
// sends us) still meets Go's default disposition, so a trapped SIGTERM arriving
// from outside kills us where MRI would run the trap. Handling that needs
// os/signal.Notify plus a delivery point in the interpreter loop; it is recorded
// as follow-up work rather than smuggled in here. Note what MRI itself does with
// a signal that arrives while no Ruby frame is executing: sighandler only fills
// signal_buff, and if the VM has already reached rb_ec_cleanup nothing runs it —
// the process then dies through ruby_default_signal. So "no Ruby frame" is not a
// case MRI serves either.
//
// handled reports whether the signal was consumed here; false means the caller
// (Process.kill) must fall through to a real kill(2).
func (vm *VM) selfSignal(sig int) (handled bool) {
	if sig == 0 || signalFatal(sig) {
		return false // kill(pid, 0) probes; a fatal signal cannot be deferred
	}
	cmd, had := vm.trapList[sig]
	if !had {
		cmd = trapCmd{kind: trapDefault}
	}
	switch cmd.kind {
	case trapIgnore, trapNil:
		// signal_ignored() == 1: MRI neither sends nor runs anything.
		return true
	case trapSystemDefault:
		// signal_ignored() == -1 (SIG_DFL): MRI really does kill(pid, sig).
		return false
	case trapExit:
		// rb_threadptr_signal_exit: SystemExit with EXIT_FAILURE.
		vm.raiseSystemExit(1, "exit")
		return true
	case trapCommand:
		// signal_exec: the handler runs with the signal number as its argument.
		vm.runTrapCommand(cmd.cmd, sig)
		return true
	}
	// trapDefault. rb_signal_exec's cmd == 0 arm: SIGINT becomes Interrupt,
	// HUP/QUIT/TERM/ALRM/USR1/USR2 become SignalException, and any other signal
	// MRI does not handle is left to the OS.
	if !vm.signalManaged(sig) {
		return false
	}
	switch sig {
	case sigNumber("INT"):
		vm.raiseSignalException("Interrupt", sig, "")
	case sigNumber("CHLD"):
		// Init_signal installs sighandler for SIGCHLD to reap children; there is
		// no cmd == 0 case for it in rb_signal_exec, so it raises nothing.
	default:
		vm.raiseSignalException("SignalException", sig, signoToSignm(sig))
	}
	return true
}

// runTrapCommand runs an installed trap. A Proc (or anything that responds to
// #call) is called with the signal number, as signal.c signal_exec does through
// rb_eval_cmd_kw(cmd, [INT2NUM(sig)]).
func (vm *VM) runTrapCommand(cmd object.Value, sig int) {
	arg := []object.Value{object.IntValue(int64(sig))}
	if p, isProc := cmd.(*Proc); isProc {
		vm.callProcWithBlock(p, arg, nil)
		return
	}
	vm.send(cmd, "call", arg, nil)
}

// raiseSignalException raises SignalException (or Interrupt) already carrying
// @signo, so a rescuer sees #signo and #signm without the class having to
// re-derive them.
func (vm *VM) raiseSignalException(class string, signo int, message string) {
	c := vm.consts[class].(*RClass)
	exc := vm.send(c, "new", []object.Value{object.NewString(message)}, nil)
	o := exc.(*RObject)
	o.ivars["@signo"] = object.IntValue(int64(signo))
	o.ivars["@message"] = object.NewString(message)
	panic(vm.excError(vm.captureBacktrace(exc)))
}

// signalEntry is one row of the signal name table.
type signalEntry struct {
	name  string
	signo int
}

// siglist is signal.c's siglist: the signal names Ruby knows, with a CANONICAL
// name ahead of each of its aliases, because signo2signm returns the first entry
// matching a number (core/signal/signame_spec.rb asserts ABRT wins over IOT and
// CHLD over CLD). EXIT is a member with number 0 — Signal.trap(:EXIT) is
// at_exit spelt as a signal, and core/signal/list_spec.rb reads it back.
//
// The numbers are this table's own, not the host's. They are correct on Darwin
// and correct everywhere for the signals whose numbers POSIX fixes (HUP 1,
// INT 2, QUIT 3, KILL 9, PIPE 13, ALRM 14, TERM 15); USR1/USR2/CHLD/STOP/TSTP
// and the BSD extras differ on Linux, which is a pre-existing gap recorded with
// this change rather than fixed by it (a per-GOOS table needs a build tag on a
// target, wasip1, that has no signal constants at all).
var siglist = []signalEntry{
	{"EXIT", 0},
	{"HUP", 1}, {"INT", 2}, {"QUIT", 3}, {"ILL", 4}, {"TRAP", 5},
	{"ABRT", 6}, {"IOT", 6},
	{"FPE", 8}, {"KILL", 9}, {"BUS", 10}, {"SEGV", 11}, {"SYS", 12},
	{"PIPE", 13}, {"ALRM", 14}, {"TERM", 15}, {"URG", 16},
	{"STOP", 17}, {"TSTP", 18}, {"CONT", 19},
	{"CHLD", 20}, {"CLD", 20},
	{"TTIN", 21}, {"TTOU", 22}, {"IO", 23}, {"XCPU", 24}, {"XFSZ", 25},
	{"VTALRM", 26}, {"PROF", 27}, {"WINCH", 28}, {"INFO", 29},
	{"USR1", 30}, {"USR2", 31},
}

// sigMaxNumber is NSIG's role in esignal_init and trap_signm: the inclusive
// upper bound a signal number may take before ArgumentError.
const sigMaxNumber = 64

// signalNumbers indexes siglist by name for lookup. signalNames indexes it by
// number, keeping the FIRST (canonical) name for each.
var signalNumbers, signalNames = func() (map[string]int, map[int]string) {
	byName := make(map[string]int, len(siglist))
	byNo := make(map[int]string, len(siglist))
	for _, e := range siglist {
		byName[e.name] = e.signo
		if _, seen := byNo[e.signo]; !seen {
			byNo[e.signo] = e.name
		}
	}
	return byName, byNo
}()

// sigNumber is siglist's number for a canonical name. It panics on an unknown
// name, which can only be a typo in this package.
func sigNumber(name string) int {
	n, ok := signalNumbers[name]
	if !ok {
		panic("vm: unknown signal name " + name)
	}
	return n
}

// signalNumberOf is signalNumberOfEx allowing EXIT, for callers that only need
// the lookup.
func signalNumberOf(name string) (int, bool) {
	return signalNumberOfEx(name, true)
}

// signalNumberOfEx looks a name up in siglist, accepting "TERM", "SIGTERM" or
// :TERM. allowExit is signm2signo's FOREACH_SIGNAL(sigs, !exit): with it false
// the EXIT entry is skipped, so "EXIT" is unsupported.
func signalNumberOfEx(name string, allowExit bool) (int, bool) {
	bare := stripSIG(name)
	if !allowExit && bare == "EXIT" {
		return 0, false
	}
	n, ok := signalNumbers[bare]
	return n, ok
}

// signalNameOf is signal.c signo2signm.
func signalNameOf(signo int) (string, bool) {
	n, ok := signalNames[signo]
	return n, ok
}

// signoToSignm is rb_signo2signm: the "SIG"-prefixed name, or "SIG<n>" for a
// number no entry carries (rb_sprintf("SIG%u", signo)).
func signoToSignm(signo int) string {
	if name, ok := signalNameOf(signo); ok {
		return "SIG" + name
	}
	return "SIG" + object.IntValue(int64(signo)).ToS()
}

// withSIG prefixes a bare signal name, as esignal_init does when the name it was
// given carried no prefix.
func withSIG(name string) string {
	if stripSIG(name) == name {
		return "SIG" + name
	}
	return name
}

func stripSIG(name string) string {
	if len(name) > 3 && name[:3] == "SIG" {
		return name[3:]
	}
	return name
}

// objClassName is MRI's rb_obj_classname: the name of the object's CLASS, which
// for nil is "NilClass" and not "nil". classNameOf answers the type name a format
// string wants, which differs for exactly the values signm2signo's error message
// is most likely to be handed (nil, true, false) — core/signal/trap_spec.rb reads
// the difference.
func (vm *VM) objClassName(v object.Value) string {
	if c := vm.classOf(v); c != nil {
		return c.name
	}
	// classOf ends in classOfPlatform, which answers nil for a value type this
	// platform does not place (it is a plain `return nil` on Windows). Nothing a
	// Ruby expression can produce reaches that, but a nil dereference in an error
	// path is a poor way to find out, so fall back to the type name.
	return classNameOf(v)
}

// toIntMaybe is signal.c's rb_check_to_integer(v, "to_int"): it reports whether
// v converts to an Integer WITHOUT raising, which is how esignal_init decides
// between "first argument is a signal number" and "first argument is a signal
// name". A String responds to neither #to_int nor an implicit conversion, so
// SignalException.new("TERM") takes the name branch.
func (vm *VM) toIntMaybe(v object.Value) (int64, bool) {
	if i, ok := v.(object.Integer); ok {
		return int64(i), true
	}
	if _, isStr := v.(*object.String); isStr {
		return 0, false
	}
	if _, isSym := v.(object.Symbol); isSym {
		return 0, false
	}
	if vm.respondsToDynamic(v, "to_int") {
		if i, ok := vm.send(v, "to_int", nil, nil).(object.Integer); ok {
			return int64(i), true
		}
	}
	return 0, false
}
