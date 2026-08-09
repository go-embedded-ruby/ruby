// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerProcMethods installs the reflection / conversion surface of Proc that
// is not on the hot call path: #parameters, #to_proc, #binding, #ruby2_keywords
// and #to_s (aliased as #inspect). It runs after registerMethodReflect2 so the
// shared parameter helpers (paramPair / displayAnon / localName) already exist.
// Behaviour matches MRI 4.0.
func (vm *VM) registerProcMethods() {
	// Proc#parameters mirrors Method#parameters but distinguishes proc from
	// lambda: a non-lambda proc reports its required-looking positionals as :opt
	// (it never enforces them), while a lambda reports them as :req. The optional
	// `lambda:` keyword overrides that choice — a truthy value forces :req, a
	// non-nil falsey value forces :opt, and nil (or an absent keyword) keeps the
	// receiver's own lambda-ness.
	vm.cProc.define("parameters", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		p := self.(*Proc)
		asLambda := p.isLambda
		if h := trailingKwHash(args); h != nil {
			if v, ok := h.Get(object.SymVal("lambda")); ok && !object.IsNil(v) {
				asLambda = v.Truthy()
			}
		}
		return procParameters(p.iseq, asLambda)
	})

	// Proc#to_proc returns the receiver unchanged.
	vm.cProc.define("to_proc", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})

	// Proc#binding returns a Binding over the block's captured environment, self
	// and lexical scope, so eval("local", proc.binding) reaches the block's
	// closure locals. A synthesized (native) Proc has no Ruby frame to capture, so
	// MRI raises — "Can't create Binding from C level Proc".
	vm.cProc.define("binding", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		p := self.(*Proc)
		if p.iseq == nil {
			raise("ArgumentError", "Can't create Binding from C level Proc")
		}
		markEnvCaptured(p.env)
		return &Binding{
			env:     p.env,
			self:    p.self,
			definee: vm.blockDefinee(p),
			file:    p.iseq.File,
			names:   append([]string(nil), p.defLocals...),
		}
	})

	// Proc#ruby2_keywords marks a proc so a trailing keyword hash flows through its
	// *rest as a flagged hash. Only a proc that accepts an argument splat and
	// neither keywords, a keyword-splat nor post-splat positionals is markable;
	// any other shape prints a warning and is left unchanged. Either way it returns
	// self. (Full flag propagation needs Hash keyword-flagging, so this installs the
	// MRI-visible warning + self-return contract.)
	vm.cProc.define("ruby2_keywords", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		p := self.(*Proc)
		if !procRuby2KeywordsMarkable(p.iseq) {
			vm.send(vm.main, "warn", []object.Value{
				object.NewString("Skipping set of ruby2_keywords flag for proc (proc accepts keywords or proc does not accept argument splat)"),
			}, nil)
		}
		return self
	})

	// Proc#to_s / #inspect. MRI's form is #<Proc:0x… file:line …>; rbgo keeps its
	// established stable representation (no volatile pointer, and no file:line
	// since the VM tracks no per-instruction line), but carries the meaningful,
	// deterministic tags: " (lambda)" for a lambda and " (&:name)" for a
	// Symbol#to_proc proc. #inspect is the same method record as #to_s (MRI
	// aliases them).
	vm.cProc.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(procToS(self.(*Proc)))
	})
	aliasBuiltin(vm.cProc, "inspect", "to_s")
}

// procParameters builds a Proc's MRI #parameters array by way of the shared
// buildParamsList: a non-lambda proc reports every required positional as :opt
// (it never enforces them), while a lambda reports them as :req.
func procParameters(is *bytecode.ISeq, isLambda bool) object.Value {
	reqKind := "opt"
	if isLambda {
		reqKind = "req"
	}
	return object.NewArray(buildParamsList(is, reqKind)...)
}

// procRuby2KeywordsMarkable reports whether a proc's parameters make it eligible
// for ruby2_keywords: it must accept a *rest splat and must not accept any
// keyword, a keyword-splat or a post-splat positional.
func procRuby2KeywordsMarkable(is *bytecode.ISeq) bool {
	if is == nil || is.SplatIndex < 0 {
		return false
	}
	if len(is.KwNames) > 0 || is.KwRestSlot >= 0 {
		return false
	}
	// No positional after the splat (a post-splat required argument).
	return is.SplatIndex == len(is.Params)-1
}

// procToS renders a Proc as rbgo's stable Proc#to_s: the base "#<Proc>", tagged
// with " (&:name)" for a Symbol#to_proc proc and " (lambda)" for a lambda. (MRI
// interleaves a volatile 0x… pointer and a file:line here; rbgo omits both — the
// pointer for determinism, the location because the VM tracks no per-instruction
// line numbers.)
func procToS(p *Proc) string {
	s := "#<Proc"
	if p.symName != "" {
		s += " (&:" + p.symName + ")"
	}
	if p.isLambda {
		s += " (lambda)"
	}
	return s + ">"
}

// trailingKwHash returns the trailing keyword Hash of a native call's argument
// list, or nil when the last argument is not a Hash.
func trailingKwHash(args []object.Value) *object.Hash {
	if n := len(args); n > 0 {
		if h, ok := args[n-1].(*object.Hash); ok {
			return h
		}
	}
	return nil
}
