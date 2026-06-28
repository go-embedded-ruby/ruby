package vm

import (
	"path/filepath"
	"runtime"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerVersionConstants installs the RUBY_* version/platform constants. The
// engine is reported as "ruby" (MRI) so app code that branches on RUBY_ENGINE —
// Puppet, Rails and most gems guard MRI-only paths with `RUBY_ENGINE == "ruby"`
// — takes the standard CRuby path. The version targets the project's Ruby 3.4
// compatibility goal, a real version string so guards like
// `if RUBY_VERSION >= "3.0"` pass.
func (vm *VM) registerVersionConstants() {
	const version = "3.4.1"
	vm.consts["RUBY_VERSION"] = object.NewString(version)
	vm.consts["RUBY_ENGINE"] = object.NewString("ruby")
	vm.consts["RUBY_ENGINE_VERSION"] = object.NewString(version)
	vm.consts["RUBY_PATCHLEVEL"] = object.Integer(0)
	vm.consts["RUBY_PLATFORM"] = object.NewString(rubyPlatform())
	vm.consts["RUBY_DESCRIPTION"] = object.NewString("ruby " + version + " [" + rubyPlatform() + "]")
	vm.consts["RUBY_COPYRIGHT"] = object.NewString("ruby - Copyright (C) 1993-2025 Yukihiro Matsumoto")
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
	// __method__: the name of the method the call sits in, or nil at the top
	// level / in a block-only context. __method__ is itself native and pushes no
	// frame, so the top of frameNames is its caller's method name.
	vm.cObject.define("__method__", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if name := vm.currentMethodName(); name != "" {
			return object.Symbol(name)
		}
		return object.NilV
	})

	// caller: a best-effort backtrace as a String array, outermost-first omitted
	// like MRI — it excludes the frame that called caller and lists the rest from
	// nearest to the top level. Without source line tracking the line is 0.
	vm.cObject.define("caller", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{Elems: vm.callerFrames()}
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

	// at_exit: register a block to run when the program finishes normally, in
	// LIFO order. Returns the block as a Proc, as MRI does.
	vm.cObject.define("at_exit", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given")
		}
		vm.atExit = append(vm.atExit, blk)
		return blk
	})
}

// currentFile returns the path of the file currently executing: the innermost
// file being required, or the top-level script path ($0) when none is on the
// require stack. It backs Kernel#__FILE__ / #__dir__.
func (vm *VM) currentFile() string {
	if n := len(vm.fileStack); n > 0 {
		return vm.fileStack[n-1]
	}
	return vm.scriptName
}

// currentMethodName returns the name of the innermost real (named) method frame,
// or "" when none. Native frames push nothing, so the top entry is the Ruby
// method that called the native introspection method.
func (vm *VM) currentMethodName() string {
	// exec always pushes a frame name ("" for top-level/class/block bodies)
	// before any native method can run, so the stack is never empty here.
	return vm.frameNames[len(vm.frameNames)-1]
}

// callerFrames builds caller's String array. It drops the topmost frame (the one
// that invoked caller) and walks outward, formatting each as MRI does, with a
// placeholder line number since this VM does not yet track source positions.
func (vm *VM) callerFrames() []object.Value {
	return vm.backtraceFrames(1)
}

// backtraceFrames renders the current frame stack as an MRI-shaped backtrace
// (innermost-first), skipping the `skip` innermost frames. Each entry is
// "<file>:0:in '<label>'": the file is the frame's recorded ISeq file (falling
// back to the executing script name, then "(rbgo)"/"-e" when none is known), the
// label is the frame's method name (or "<main>" for top-level / block bodies).
// The line is always 0 because the parser AST carries no source positions. It
// generalises both Kernel#caller (skip 1, dropping its own native caller's
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
		out = append(out, object.NewString(vm.frameFileLabel(i)+":0:in '"+where+"'"))
	}
	return out
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
