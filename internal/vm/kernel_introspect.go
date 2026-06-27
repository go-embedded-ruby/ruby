package vm

import (
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
	if len(vm.frameNames) < 2 {
		return nil // only the calling frame (or none): no outer frames to report
	}
	var out []object.Value
	for i := len(vm.frameNames) - 2; i >= 0; i-- {
		where := "<main>"
		if name := vm.frameNames[i]; name != "" {
			where = name
		}
		out = append(out, object.NewString("(rbgo):0:in '"+where+"'"))
	}
	return out
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
