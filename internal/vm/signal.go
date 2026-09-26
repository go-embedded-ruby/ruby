// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
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
	trapIgnore                        // "IGNORE" / "SIG_IGN" / "" / nil
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
		if len(args) == 0 && blk == nil {
			return raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		if len(args) == 0 {
			return raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		sig := vm.trapSignm(args[0])
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
	cSig, ok := vm.consts["SignalException"].(*RClass)
	if !ok {
		return
	}
	// esignal_init(sig) / esignal_init(signo, message = signo2signm(signo)):
	// the first argument is tried as an Integer through #to_int; if it converts,
	// a second argument may supply the message and arity is 1..2, otherwise the
	// argument is a signal NAME and arity is exactly 1. A name is stored with a
	// "SIG" prefix when it was given without one (`sig = "SIG" + sig` when the
	// matched prefix length differs from signame_prefix_len), which is why
	// SignalException.new("TERM").message is "SIGTERM".
	cSig.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		o, isObj := self.(*RObject)
		if !isObj {
			return object.NilV
		}
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
			name := args[0].ToS()
			num, found := signalNumberOf(name)
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
	if cInt, okI := vm.consts["Interrupt"].(*RClass); okI {
		cInt.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			o, isObj := self.(*RObject)
			if !isObj {
				return object.NilV
			}
			msg := ""
			if len(args) > 0 {
				msg = args[0].ToS()
			}
			o.ivars["@message"] = object.NewString(msg)
			o.ivars["@signo"] = object.IntValue(int64(sigNumber("INT")))
			return object.NilV
		})
	}
}

// trapSignm coerces Signal.trap's first argument to a signal number, applying
// signal.c trap_signm: a Fixnum is range-checked against NSIG, anything else
// goes through signm2signo with exit = TRUE, so "EXIT"/0 is accepted.
func (vm *VM) trapSignm(v object.Value) int {
	if n, isInt := vm.toIntMaybe(v); isInt {
		if n < 0 || n > int64(sigMaxNumber) {
			raise("ArgumentError", "invalid signal number (%d)", n)
		}
		return int(n)
	}
	name := v.ToS()
	if num, ok := signalNumberOf(name); ok {
		return num
	}
	raise("ArgumentError", "unsupported signal '%s'", withSIG(name))
	return 0
}

// trapHandler maps Signal.trap's second argument to a disposition, following
// signal.c trap_handler's switch on the command string's length exactly: nil and
// "" and "IGNORE"/"SIG_IGN" ignore, "DEFAULT"/"SIG_DFL" restore the signal's
// default RUBY action, "SYSTEM_DEFAULT" restores the OS default, "EXIT" exits,
// and anything else is a command to run.
func (vm *VM) trapHandler(v object.Value) trapCmd {
	if v == object.NilV {
		return trapCmd{kind: trapIgnore, cmd: object.NilV}
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
	prev, had := vm.trapList[sig]
	if !had {
		if vm.signalManaged(sig) {
			return object.NewString("DEFAULT")
		}
		return object.NewString("SYSTEM_DEFAULT")
	}
	switch prev.kind {
	case trapDefault:
		if vm.signalManaged(sig) {
			return object.NewString("DEFAULT")
		}
		return object.NewString("SYSTEM_DEFAULT")
	case trapIgnore:
		return object.NewString("IGNORE")
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
	case trapIgnore:
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
	c, ok := vm.consts[class].(*RClass)
	if !ok {
		raise(class, "%s", message)
		return
	}
	exc := vm.send(c, "new", []object.Value{object.NewString(message)}, nil)
	if o, isObj := exc.(*RObject); isObj {
		o.ivars["@signo"] = object.IntValue(int64(signo))
		o.ivars["@message"] = object.NewString(message)
	}
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

// signalNumberOf is signal.c signm2signo: it accepts "TERM", "SIGTERM" or
// :TERM and reports whether the name is one Ruby knows.
func signalNumberOf(name string) (int, bool) {
	n, ok := signalNumbers[stripSIG(name)]
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

// signalName normalises a signal designator to its bare name (no "SIG" prefix),
// accepting a Symbol, a String ("INT"/"SIGINT") or an Integer. Kept for callers
// that want a name rather than a number.
func signalName(v object.Value) string {
	switch s := v.(type) {
	case object.Symbol:
		return stripSIG(string(s))
	case *object.String:
		return stripSIG(string(s.Bytes()))
	case object.Integer:
		if name, ok := signalNameOf(int(s)); ok {
			return name
		}
		return v.ToS()
	default:
		return v.ToS()
	}
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
