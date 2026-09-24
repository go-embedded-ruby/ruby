package vm

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerVersionConstants installs the RUBY_* version/platform constants. The
// engine is reported as "ruby" (MRI) so app code that branches on RUBY_ENGINE —
// Puppet, Rails and most gems guard MRI-only paths with `RUBY_ENGINE == "ruby"`
// — takes the standard CRuby path. The version targets the project's Ruby 3.4
// compatibility goal, a real version string so guards like
// `if RUBY_VERSION >= "3.0"` pass.
func (vm *VM) registerVersionConstants() {
	const (
		version     = "3.4.1"
		releaseDate = "2024-12-25"
		revision    = "48d4efcb85000e1ebae42004e963b5d0cedddcf2"
	)
	vm.consts["RUBY_VERSION"] = object.NewString(version)
	vm.consts["RUBY_ENGINE"] = object.NewString("ruby")
	vm.consts["RUBY_ENGINE_VERSION"] = object.NewString(version)
	vm.consts["RUBY_PATCHLEVEL"] = object.IntValue(0)
	vm.consts["RUBY_PLATFORM"] = object.NewString(rubyPlatform())
	vm.consts["RUBY_DESCRIPTION"] = object.NewString("ruby " + version + " [" + rubyPlatform() + "]")
	vm.consts["RUBY_COPYRIGHT"] = object.NewString("ruby - Copyright (C) 1993-2025 Yukihiro Matsumoto")
	// RUBY_RELEASE_DATE and RUBY_REVISION describe the MRI release this runtime
	// targets, and both are frozen Strings (core/builtin_constants asserts the
	// class and the frozen-ness of each). version.h at tag v3_4_1 builds
	// RUBY_RELEASE_DATE out of RUBY_RELEASE_YEAR/MONTH/DAY and takes RUBY_REVISION
	// from the generated revision.h, i.e. the tagged commit: ruby/ruby v3_4_1 is
	// 48d4efcb85000e1ebae42004e963b5d0cedddcf2, committed 2024-12-25.
	vm.consts["RUBY_RELEASE_DATE"] = object.NewFrozenStringView(releaseDate)
	vm.consts["RUBY_REVISION"] = object.NewFrozenStringView(revision)
	vm.registerRbConfig(version)
}

// rbconfigGOOS reports the build OS used to pick RbConfig's EXEEXT (".exe" on
// Windows). It is a package var so a test can drive the Windows branch on any
// host: that branch is otherwise unreachable off Windows (and the non-Windows
// path unreachable on Windows), so the seam keeps the function at 100% on every
// platform's coverage gate.
var rbconfigGOOS = func() string { return runtime.GOOS }

// registerRbConfig installs the RbConfig module (require "rbconfig") with a
// CONFIG Hash holding the build-configuration keys app code reads — Puppet looks
// up ruby_install_name / bindir / EXEEXT, and tooling reads rubylibdir. The
// values describe this pure-Go runtime as an MRI build so MRI-shaped lookups
// resolve.
func (vm *VM) registerRbConfig(version string) {
	mod := newClass("RbConfig", nil)
	mod.isModule = true
	vm.consts["RbConfig"] = mod

	cfg := object.NewHash()
	exeext := ""
	if rbconfigGOOS() == "windows" {
		exeext = ".exe"
	}
	for k, v := range map[string]string{
		"ruby_install_name": "ruby",
		"RUBY_INSTALL_NAME": "ruby",
		"bindir":            "/usr/bin",
		"EXEEXT":            exeext,
		"ruby_version":      version,
		"host_os":           runtime.GOOS,
		"host_cpu":          runtime.GOARCH,
		"rubylibdir":        "/usr/lib/ruby",
	} {
		cfg.Set(object.NewString(k), object.NewString(v))
	}
	mod.consts["CONFIG"] = cfg
}

// rubyPlatform renders a Ruby-style platform triple from the Go build target,
// e.g. "arm64-darwin", "x86_64-linux".
func rubyPlatform() string { return rubyPlatformFor(runtime.GOARCH, runtime.GOOS) }

// rubyPlatformFor maps a (GOARCH, GOOS) pair to a Ruby-style platform triple. It
// is split out from rubyPlatform so the arch/os normalisations are testable on
// any host.
func rubyPlatformFor(arch, os string) string {
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "386":
		arch = "i686"
	}
	if os == "wasip1" || os == "js" {
		os = "wasm"
	}
	return arch + "-" + os
}

// registerKernelIntrospection installs Kernel#caller, #at_exit and #__method__.
func (vm *VM) registerKernelIntrospection() {
	// __method__: the definition-original name of the method the call sits in, or
	// nil at the top level / in a class body. It is native and pushes no frame, so
	// the top of frameMethods is its caller's pair; it reports orig, so an aliased
	// method still reports the name its body was defined under. Transparent through
	// blocks and eval, which inherit the enclosing method's pair.
	vm.cObject.define("__method__", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if name := vm.currentMethodCtx().orig; name != "" {
			return object.Symbol(name)
		}
		return object.NilV
	})

	// __callee__: like __method__ but the name the method was *called* by — the
	// alias for an aliased method, and otherwise identical. nil at the top level /
	// in a class body; transparent through blocks and eval.
	vm.cObject.define("__callee__", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if name := vm.currentMethodCtx().callee; name != "" {
			return object.Symbol(name)
		}
		return object.NilV
	})
	// __method__ and __callee__ are private instance methods in MRI (callable only
	// without an explicit receiver).
	vm.setInstanceVisibility(vm.cObject, "__method__", visPrivate)
	vm.setInstanceVisibility(vm.cObject, "__callee__", visPrivate)
	// MRI defines them as Kernel module functions: a private instance method of
	// Kernel and a public method on the Kernel module itself. The bodies run on
	// Object (above); mirroring the records onto Kernel makes the introspection
	// reflect that (Kernel.private_instance_methods / .public_methods).
	for _, name := range []string{"__method__", "__callee__"} {
		fn := vm.cObject.methods[name].native
		vm.cKernel.methods[name] = &Method{name: name, owner: vm.cKernel, native: fn, vis: visPrivate}
		vm.cKernel.smethods[name] = &Method{name: name, owner: vm.cKernel, native: fn}
	}

	// caller(start=1, length=nil) / caller(range): a best-effort backtrace as a
	// String array, listed nearest-first. Like MRI, `start` omits that many
	// innermost levels (caller == caller(1) drops the frame that called caller;
	// caller(0) keeps it), `length` caps the count, and a Range selects levels the
	// way Array#[] slices caller(0). A start past the top returns nil (distinct from
	// []). Without source line tracking the line is 0.
	vm.cObject.define("caller", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		frames, present := vm.callerSlice(args)
		if !present {
			return object.NilV
		}
		return object.NewArrayFromSlice(frames)
	})

	// caller_locations(start=1, length=nil) / caller_locations(range): like #caller
	// but each level is a Thread::Backtrace::Location value object (answering #path,
	// #lineno, #label, #absolute_path, #to_s) instead of a plain String. It shares
	// #caller's argument handling exactly, including the nil-for-overshoot result.
	vm.cObject.define("caller_locations", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		frames, present := vm.callerSlice(args)
		if !present {
			return object.NilV
		}
		locs := make([]object.Value, len(frames))
		for i, f := range frames {
			locs[i] = vm.backtraceLocation(f.ToS())
		}
		return object.NewArrayFromSlice(locs)
	})

	// __FILE__: the path of the file currently executing. During a require it is
	// the required file's absolute path; at the top level it is the script path
	// ($0). MRI exposes __FILE__ as a keyword the parser substitutes at compile
	// time; here it is a Kernel method returning the same value, which covers the
	// common top-level / module-body uses (File.dirname(__FILE__), __dir__).
	vm.cObject.define("__FILE__", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.currentFile())
	})
	// __dir__: the directory of the file currently executing (File.dirname of the
	// realpath of __FILE__), as MRI's Kernel#__dir__.
	vm.cObject.define("__dir__", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		f := vm.currentFile()
		if f == "" {
			return object.NilV
		}
		return object.NewString(filepath.Dir(f))
	})

	// __LINE__ is deliberately NOT defined here. It is a keyword in MRI, not a
	// method: the compiler substitutes the current source line as an Integer
	// literal (see compileCall). Defining a Kernel method of that name as well
	// would answer `self.__LINE__` and `1.send(:__LINE__)`, which raise
	// NoMethodError in ruby 4.0.5 — the method that used to stand in for the
	// missing line map made both of those silently return 0.

	// at_exit: register a block to run when the program finishes normally, in
	// LIFO order. Returns the block as a Proc, as MRI does.
	vm.cObject.define("at_exit", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			// MRI raises ArgumentError("called without a block"), not a LocalJumpError.
			raise("ArgumentError", "called without a block")
		}
		vm.atExit = append(vm.atExit, blk)
		return blk
	})

	// Kernel#exit(status = true) raises SystemExit, which unwinds to the top
	// (running at_exit handlers there). MRI's rb_f_exit (process.c) accepts 0..1
	// arguments, maps the status through exit_status_code and raises
	// SystemExit.new(status, "exit") — so the status the program asked to stop
	// with survives to the rescuer. Puppet's exit_on_fail calls exit(code) after
	// logging and then reads the code back off the SystemExit, so discarding it
	// was a real loss, not just a spec gap.
	//
	// exit!(status = false) differs only in its default (EXIT_FAILURE) and in
	// skipping at_exit handlers; the embedded host has no process to _exit() from,
	// so it unwinds the same way. Both are also reachable as Kernel.exit /
	// Kernel.exit! — registerKernelModuleFunctions mirrors the records.
	vm.cObject.define("exit", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.raiseSystemExit(vm.exitStatusArg(args, 0), "exit")
	})
	vm.cObject.define("exit!", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.raiseSystemExit(vm.exitStatusArg(args, 1), "exit")
	})
	// abort(message = nil): MRI's rb_f_abort (process.c) puts the message —
	// through StringValue, so a non-String converts with #to_str — on $stderr and
	// then raises SystemExit.new(EXIT_FAILURE, message); with no argument it
	// exits EXIT_FAILURE with the plain "exit" message. Unlike exit, the status is
	// never taken from the argument.
	vm.cObject.define("abort", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
		}
		if len(args) == 1 {
			msg := vm.coerceToString(args[0])
			fmt.Fprintln(vm.errOut, msg)
			return vm.raiseSystemExit(1, msg)
		}
		return vm.raiseSystemExit(1, "exit")
	})
}

// exitStatusArg reads Kernel#exit / #exit!'s optional status argument, applying
// MRI's arity check (0..1) and exit_status_code mapping (process.c): true is
// EXIT_SUCCESS (0), false is EXIT_FAILURE (1), and anything else goes through
// NUM2INT — an Integer passes, a Float truncates through #to_int, and a String,
// nil or Array (none of which define #to_int) is a TypeError. dflt is the code
// for a bare call: 0 for exit, 1 for exit!.
func (vm *VM) exitStatusArg(args []object.Value, dflt int64) int64 {
	if len(args) > 1 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
	}
	if len(args) == 0 {
		return dflt
	}
	switch args[0] {
	case object.Value(object.True):
		return 0
	case object.Value(object.False):
		return 1
	}
	return vm.toIntCoerce(args[0])
}

// raiseSystemExit raises the SystemExit that Kernel#exit / #exit! / #abort stop
// the program with, built as MRI's rb_exit does it —
// SystemExit.new(status, message) — so #status reports the requested code and
// #message the requested text. It never returns; the object.Value result type
// only lets it be the tail expression of a native method body.
func (vm *VM) raiseSystemExit(status int64, message string) object.Value {
	exc := vm.send(vm.consts["SystemExit"].(*RClass), "new",
		[]object.Value{object.IntValue(status), object.NewString(message)}, nil)
	panic(vm.excError(vm.captureBacktrace(exc)))
}

// registerKernelDelegates installs the Kernel global functions whose behaviour
// CRuby defines by forwarding to another object. MRI declares each with
// rb_define_global_function (io.c:15615-15628, process.c), which makes it a
// private instance method reachable without a receiver AND — through the
// Kernel module_function split registerKernelModuleFunctions applies — a public
// method on the Kernel module; the names are listed there so the split covers
// them.
//
// Every body here forwards through vm.send to the object CRuby forwards to, so
// a spec that stubs ARGF.gets (core/kernel/gets_spec) or replaces IO.select
// sees its replacement run, exactly as MRI's `forward(argf, idGets, …)` does.
func (vm *VM) registerKernelDelegates() {
	// Kernel#gets / #readline / #readlines read the ARGV-concatenated input
	// stream: rb_f_gets, rb_f_readline and rb_f_readlines (io.c) all
	// `forward(argf, …)` when the receiver is not ARGF itself.
	for _, name := range []string{"gets", "readline", "readlines"} {
		meth := name
		vm.cObject.define(meth, func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.send(vm.consts["ARGF"], meth, args, blk)
		})
	}

	// Kernel#select is IO.select under another name (rb_f_select, io.c, shares
	// select_call with rb_io_s_select).
	vm.cObject.define("select", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.send(vm.consts["IO"], "select", args, blk)
	})

	// Kernel#spawn is Process.spawn (rb_f_spawn, process.c).
	vm.cObject.define("spawn", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.send(vm.consts["Process"], "spawn", args, blk)
	})

	// Kernel#syscall invokes a system call by number. A pure-Go, CGO-free runtime
	// has no portable way to do that, which is the situation CRuby itself is in
	// on a platform without syscall(2): io.c ends with
	// `#define rb_f_syscall rb_f_notimplement`, and rb_f_notimplement raises
	// NotImplementedError with this message shape (error.c). MRI 4.0.5 on darwin
	// answers exactly this.
	vm.cObject.define("syscall", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return raise("NotImplementedError", "syscall() function is unimplemented on this machine")
	})

	// Kernel#` (backquote): rb_f_backquote (io.c) takes exactly one argument,
	// puts it through StringValue (so a non-String is converted with #to_str, and
	// anything else is a TypeError) and returns the command's output. The command
	// runs through the same runShellCommand the `%x{…}`/backtick *literal* uses
	// (OpXStr), so the two spellings cannot drift apart.
	vm.cObject.define("`", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		return object.NewString(vm.runShellCommand(vm.coerceToString(args[0])))
	})

	// Kernel#global_variables lists every defined global as a Symbol
	// (rb_f_global_variables, variable.c, walks rb_global_tbl). The stored
	// globals are this VM's table; the process/exception specials specialGvar
	// answers out of VM state rather than the table ($!, $0, $$) are always
	// defined, so they are listed too when the table has no slot for them. MRI's
	// order is the global table's internal one and unspecified; sorting keeps the
	// answer stable across calls (Go map iteration is randomised).
	vm.cObject.define("global_variables", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		names := make([]string, 0, len(vm.globals)+len(alwaysDefinedGvars))
		for n := range vm.globals {
			names = append(names, n)
		}
		for _, n := range alwaysDefinedGvars {
			if _, ok := vm.globals[n]; !ok {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		out := make([]object.Value, len(names))
		for i, n := range names {
			out[i] = object.Symbol(n)
		}
		return object.NewArrayFromSlice(out)
	})
}

// alwaysDefinedGvars are the globals specialGvar answers from VM state instead
// of the vm.globals table, so they are defined even with no slot in it. Kernel#
// global_variables adds them to the table's names.
var alwaysDefinedGvars = []string{"$!", "$0", "$$"}

// currentFile returns the path of the file currently executing: the innermost
// file being required, or the top-level script path ($0) when none is on the
// require stack. It backs Kernel#__FILE__ / #__dir__.
func (vm *VM) currentFile() string {
	if n := len(vm.fileStack); n > 0 {
		return vm.fileStack[n-1]
	}
	return vm.scriptName
}

// currentMethodCtx returns the __method__/__callee__ pair of the frame on top of
// the stack — the Ruby frame that called the native introspection method (a
// native pushes no frame). exec always pushes a frame (even for top-level / class
// bodies, whose pair is empty and reports nil) before any native can run, so the
// stack is never empty here; this backs Kernel#__method__ / #__callee__ directly.
func (vm *VM) currentMethodCtx() frameMethod {
	return vm.frameMethods[len(vm.frameMethods)-1]
}

// currentMethodCtxPtr returns a heap copy of currentMethodCtx for handing to the
// next exec frame via pendingMethodCtx (Kernel#eval, so eval'd code inherits the
// caller's method context).
func (vm *VM) currentMethodCtxPtr() *frameMethod {
	ctx := vm.currentMethodCtx()
	return &ctx
}

// callerSlice selects the levels Kernel#caller / #caller_locations return from the
// argument list, applying MRI's semantics over caller(0) — backtraceFrames(0),
// the current frame stack nearest-first. A Range slices that list exactly like
// Array#[] (endless, beginless and negative bounds included); an Integer start
// (default 1) omits that many innermost levels, with an optional non-negative
// length cap. A negative start/length is an ArgumentError, as MRI; a start past
// the top of the stack yields (nil, false) so the caller returns Ruby nil,
// distinct from an empty array (an exactly-past-the-end start yields []). The
// start/length are coerced through #to_int, so a Float level truncates.
func (vm *VM) callerSlice(args []object.Value) ([]object.Value, bool) {
	full := vm.backtraceFrames(0)
	fullArr := object.NewArrayFromSlice(full)
	// Range form: forward to Array#[], which already handles every bound shape.
	if len(args) >= 1 {
		if _, isRange := args[0].(*object.Range); isRange {
			res := vm.send(fullArr, "[]", []object.Value{args[0]}, nil)
			if object.IsNil(res) {
				return nil, false
			}
			return res.(*object.Array).Elems, true
		}
	}
	start := int64(1)
	if len(args) >= 1 {
		start = vm.toIntCoerce(args[0])
	}
	if start < 0 {
		raise("ArgumentError", "negative level (%d)", start)
	}
	// A start-only call is caller(0)[start..]: at most len(full) elements from
	// start, which Array#[](start, len(full)+1) yields with the right nil/[] edges.
	length := int64(len(full)) + 1
	if len(args) >= 2 && !object.IsNil(args[1]) {
		length = vm.toIntCoerce(args[1])
		if length < 0 {
			raise("ArgumentError", "negative size (%d)", length)
		}
	}
	res := vm.send(fullArr, "[]", []object.Value{object.IntValue(start), object.IntValue(length)}, nil)
	if object.IsNil(res) {
		return nil, false
	}
	return res.(*object.Array).Elems, true
}

// backtraceFrames renders the current frame stack as an MRI-shaped backtrace
// (innermost-first), skipping the `skip` innermost frames. Each entry is
// "<file>:<line>:in '<label>'": the file is the frame's recorded ISeq file
// (falling back to the executing script name, then "(rbgo)"/"-e" when none is
// known), the line is where that frame has got to, and the label is the frame's
// method name (or "<main>" for top-level / block bodies).
//
// The line used to be the literal 0, because the parser AST carried no source
// positions; it is now frameLine(i), which is MRI's calc_lineno over the
// frame's (iseq, pc) pair. A frame that still cannot be placed renders 0, and
// location_format (vm_backtrace.c v3_4_0:446) omits the ":0" entirely in that
// case — so the line is only in the string when there is one to report.
//
// It generalises both Kernel#caller (skip 1, dropping its own native caller's
// frame) and exception capture at raise time (skip 0). GVL-guarded via the VM.
func (vm *VM) backtraceFrames(skip int) []object.Value {
	top := len(vm.frameNames) - 1 - skip
	if top < 0 {
		return nil // nothing left to report after the skip
	}
	out := make([]object.Value, 0, top+1)
	for i := top; i >= 0; i-- {
		where := "<main>"
		if name := vm.frameNames[i]; name != "" {
			where = name
		}
		out = append(out, object.NewString(formatBacktraceEntry(vm.frameFileLabel(i), vm.frameLine(i), where)))
	}
	return out
}

// formatBacktraceEntry renders one backtrace line.
//
// It is MRI's location_format (vm_backtrace.c v3_4_0:446), including the part
// that is easy to miss: the ":%d" is appended only `if (lineno != 0)`. A frame
// with no line reports "path:in 'label'", not "path:0:in 'label'" — so an
// unplaceable frame says nothing about its line rather than claiming line zero.
func formatBacktraceEntry(file string, line int, label string) string {
	if line == 0 {
		return file + ":in '" + label + "'"
	}
	return file + ":" + strconv.Itoa(line) + ":in '" + label + "'"
}

// frameFileLabel returns the file portion of a backtrace entry for frame i: the
// frame's own ISeq file when stamped, otherwise the running script name, and
// finally "-e" for a `-e` one-liner or "(rbgo)" when no file is known at all.
func (vm *VM) frameFileLabel(i int) string {
	if f := vm.frameFiles[i]; f != "" {
		return f
	}
	switch vm.scriptName {
	case "":
		return "(rbgo)"
	case "-e":
		return "-e"
	default:
		return vm.scriptName
	}
}

// runAtExit runs the registered at_exit blocks in LIFO order. A raise inside one
// is swallowed so the remaining handlers still run (MRI reports it but keeps
// going); other control-flow signals propagate.
func (vm *VM) runAtExit() {
	for i := len(vm.atExit) - 1; i >= 0; i-- {
		vm.runAtExitOne(vm.atExit[i])
	}
	vm.atExit = nil
}

func (vm *VM) runAtExitOne(blk *Proc) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(RubyError); ok {
				return // swallow: a failing at_exit hook must not abort the others
			}
			panic(r)
		}
	}()
	vm.callBlock(blk, nil)
}

// gvarTraceIvar names the Kernel-module ivar holding the Kernel#trace_var hook
// table (a Hash of Symbol global name -> Array of commands, most recent first).
// Parking it on the module rather than in a VM field keeps the cost at zero for
// the overwhelming majority of programs: the ivar is created by the first
// trace_var call and never exists otherwise.
const gvarTraceIvar = "@__gvar_traces"

// gvarTraces returns the trace table, creating it when create is set. A read
// with no table yet reports nil, which is the fast path every ordinary global
// assignment takes.
func (vm *VM) gvarTraces(create bool) *object.Hash {
	if h, ok := vm.cKernel.ivars[gvarTraceIvar].(*object.Hash); ok {
		return h
	}
	if !create {
		return nil
	}
	// newClass always allocates the ivar table, so cKernel's is never nil.
	h := object.NewHash()
	vm.cKernel.ivars[gvarTraceIvar] = h
	return h
}

// fireGvarTraces runs the hooks registered for name, most recently added first,
// passing the newly assigned value. variable.c v3_4_0 rb_gvar_set_entry runs the
// trace list AFTER the variable's setter has stored the value, and trace_ev
// walks entry->var->trace from the head — which rb_f_trace_var pushes onto — so
// the last hook registered is the first to run. A command that is not callable
// is a String of Ruby source, which rb_trace_eval hands to rb_eval_cmd_kw.
func (vm *VM) fireGvarTraces(name string, v object.Value) {
	h := vm.gvarTraces(false)
	if h == nil {
		return
	}
	cmds, ok := h.Get(object.Symbol(name))
	if !ok {
		return
	}
	arr, ok := cmds.(*object.Array)
	if !ok {
		return
	}
	for _, cmd := range append([]object.Value(nil), arr.Elems...) {
		if s, isStr := cmd.(*object.String); isStr {
			vm.send(vm.main, "eval", []object.Value{s}, nil)
			continue
		}
		vm.send(cmd, "call", []object.Value{v}, nil)
	}
}

// registerGvarTracing installs Kernel#trace_var and Kernel#untrace_var, the
// hooks that fire when a global is assigned. Reference: ruby/ruby v3_4_0
// variable.c rb_f_trace_var / rb_f_untrace_var / rb_gvar_set_entry.
func (vm *VM) registerGvarTracing() {
	// trace_var(name, cmd = nil, &block): register cmd (or the block) to run on
	// every assignment to the named global, and return nil. rb_f_trace_var takes
	// the block through rb_block_proc() when no command is given, so a call with
	// neither raises ArgumentError "tried to create Proc object without a block";
	// an explicit nil command means untrace_var instead. The name is not
	// validated (MRI runs it through rb_to_id, which accepts any symbol/string).
	vm.cObject.define("trace_var", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) < 1 || len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		name := nameArg(args[0])
		var cmd object.Value = object.NilV
		if len(args) == 2 {
			cmd = args[1]
		}
		if object.IsNil(cmd) {
			if blk == nil {
				if len(args) == 2 {
					// An explicit nil command: rb_f_trace_var delegates to
					// rb_f_untrace_var with the same arguments.
					return vm.send(vm.main, "untrace_var", args[:1], nil)
				}
				raise("ArgumentError", "tried to create Proc object without a block")
			}
			cmd = blk
		}
		h := vm.gvarTraces(true)
		key := object.Symbol(name)
		var elems []object.Value
		if prev, ok := h.Get(key); ok {
			if a, isArr := prev.(*object.Array); isArr {
				elems = a.Elems
			}
		}
		h.Set(key, object.NewArrayFromSlice(append([]object.Value{cmd}, elems...)))
		return object.NilV
	})

	// untrace_var(name, cmd = nil): remove the hooks registered for the global and
	// return them as an Array — all of them, or just the one equal to cmd. A name
	// that is neither traced nor a defined global is a NameError, as
	// rb_f_untrace_var's rb_find_global_entry failure is. Removing a command that
	// was never registered returns nil.
	vm.cObject.define("untrace_var", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 || len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		name := nameArg(args[0])
		key := object.Symbol(name)
		h := vm.gvarTraces(false)
		var elems []object.Value
		if h != nil {
			if prev, ok := h.Get(key); ok {
				if a, isArr := prev.(*object.Array); isArr {
					elems = a.Elems
				}
			}
		}
		if elems == nil {
			// No hooks: a global with no entry at all is rb_find_global_entry's
			// NameError, while a defined-but-untraced one yields the empty Array
			// rb_f_untrace_var builds from an empty trace list (witnessed:
			// `$tv = nil; p untrace_var(:$tv)` prints [] on ruby 4.0.5). With a
			// command to remove, the walk finds nothing and the answer is nil.
			if _, defined := vm.globals[canonicalGvar(name)]; !defined {
				vm.raiseNameError("undefined global variable "+name, name)
			}
			if len(args) == 2 && !object.IsNil(args[1]) {
				return object.NilV
			}
			return object.NewArray()
		}
		if len(args) == 2 && !object.IsNil(args[1]) {
			for i, cmd := range elems {
				if cmd == args[1] {
					h.Set(key, object.NewArrayFromSlice(append(append([]object.Value(nil), elems[:i]...), elems[i+1:]...)))
					return object.NewArray(cmd)
				}
			}
			return object.NilV
		}
		h.Delete(key)
		return object.NewArrayFromSlice(append([]object.Value(nil), elems...))
	})
}
