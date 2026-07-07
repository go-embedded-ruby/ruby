// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
	irb "github.com/go-ruby-irb/irb"
)

// registerIRB installs the IRB module: the interactive REPL for the embedded
// interpreter. The deterministic REPL machinery — deciding when accumulated
// input forms a complete statement (multi-line continuation), expanding the
// prompt %-specs, and shaping the `=> value` result line — lives entirely in the
// pure-Go github.com/go-ruby-irb/irb library (a faithful port of IRB's
// interpreter-independent core). The one thing that library cannot do is
// evaluate Ruby; that is exactly rbgo's job, so the read→eval→print→loop drives
// each complete statement through the front-end (parse + compile + exec) against
// a persistent Binding, on the VM goroutine under the GVL. Input is read from
// the current $stdin and output written to the current $stdout, so a host (or a
// test) can rebind them to a StringIO to script a session with no live terminal.
func (vm *VM) registerIRB() {
	mIRB := newClass("IRB", nil)
	mIRB.isModule = true
	vm.consts["IRB"] = mIRB

	// IRB.conf is the shared, mutable configuration store (a Hash keyed by
	// symbols), returned by-reference so writes persist across calls the way
	// IRB.conf[:PROMPT_MODE] = :SIMPLE does in MRI. It is captured by the start /
	// binding.irb closures so a session reads the latest configuration.
	conf := object.NewHash()
	conf.Set(object.SymVal("PROMPT_MODE"), object.SymVal("DEFAULT"))
	conf.Set(object.SymVal("ECHO"), object.Bool(true))

	mIRB.smethods["conf"] = &Method{name: "conf", owner: mIRB, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return conf
	}}
	mIRB.smethods["version"] = &Method{name: "version", owner: mIRB, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString("irb (go-embedded-ruby)")
	}}

	// IRB.start(*) opens a top-level session: the REPL runs against the main
	// object with a fresh, empty local scope, exactly like launching `irb`.
	mIRB.smethods["start"] = &Method{name: "start", owner: mIRB, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.irbSession(&Binding{env: &Env{}, self: vm.main, definee: vm.cObject}, conf)
	}}

	// binding.irb opens a session bound to the receiver binding, so the REPL sees
	// (and can mutate) that frame's self and local variables — the `binding.irb`
	// breakpoint idiom. Binding is registered before IRB in the bootstrap, but the
	// lookup is guarded so a build that omitted it degrades gracefully.
	if cBinding, ok := vm.consts["Binding"].(*RClass); ok {
		cBinding.define("irb", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return vm.irbSession(self.(*Binding), conf)
		})
	}
}

// irbSession resolves the session's I/O streams and prompt mode from the current
// globals and IRB.conf, then runs the REPL loop against b. A missing (rebound to
// a non-IO value) $stdin means there is nothing to read, so the session returns
// nil immediately, as a real IRB started on a closed input would.
func (vm *VM) irbSession(b *Binding, conf *object.Hash) object.Value {
	in, ok := vm.globals["$stdin"].(*IOObj)
	if !ok {
		return object.NilV
	}
	out := vm.curStdout()
	// `_` is the last-evaluation result; IRB seeds it to nil so referencing it
	// before any expression has been evaluated yields nil rather than a NameError.
	bindingSetLocal(b, "_", object.NilV)
	return vm.irbRun(b, in, out, irbModeFromConf(conf), vm.displayStr(b.self))
}

// irbModeFromConf reads IRB.conf[:PROMPT_MODE] and maps it to a prompt-mode
// table, falling back to :DEFAULT when the key is absent, not a symbol, or names
// an unknown mode — mirroring IRB's tolerance of a bad :PROMPT_MODE setting.
func irbModeFromConf(conf *object.Hash) irb.PromptMode {
	v, ok := conf.Get(object.SymVal("PROMPT_MODE"))
	if !ok {
		return irb.PromptDefault
	}
	s, ok := v.(object.Symbol)
	if !ok {
		return irb.PromptDefault
	}
	if m, ok := irb.PromptModes[string(s)]; ok {
		return m
	}
	return irb.PromptDefault
}

// irbRun is the read→eval→print→loop. Each turn it renders the prompt for the
// input accumulated so far (a continuation prompt while a block/literal/heredoc
// is still open), reads one line from in, and — once irb.CheckCode reports the
// buffer is no longer "needs more input" — dispatches the complete statement.
// EOF (a nil read, e.g. Ctrl-D) ends the session after a closing newline.
func (vm *VM) irbRun(b *Binding, in, out *IOObj, mode irb.PromptMode, mainStr string) object.Value {
	ctx := irb.PromptContext{IRBName: "irb", Main: mainStr}
	lineNo := 1
	var buf string
	for {
		verdict, opens := irb.CheckCode(buf)
		out.writeStr(irb.GeneratePrompt(mode, ctx, opens, verdict == irb.More, lineNo, true, mode.AutoIndent))

		line := ioGets(in, nil)
		s, ok := line.(*object.String)
		if !ok {
			out.writeStr("\n")
			return object.NilV
		}
		buf += s.Str()
		lineNo++

		if v, _ := irb.CheckCode(buf); v == irb.More {
			continue
		}
		code := buf
		buf = ""
		if vm.irbDispatch(b, code, out, mode) {
			return object.NilV
		}
	}
}

// irbDispatch handles one complete unit of input: an `exit`/`quit` command ends
// the loop (honouring IRB's rule that a local variable of that name shadows the
// command); anything else is evaluated as Ruby. A raised SystemExit (a call to
// Kernel#exit inside the evaluated code) likewise ends the loop cleanly; any
// other exception is reported MRI-style ("message (Class)") and the loop
// survives it. On success the result becomes `_` and is echoed via the mode's
// return format. It returns true when the session should end.
func (vm *VM) irbDispatch(b *Binding, code string, out *IOObj, mode irb.PromptMode) bool {
	locals := make(map[string]bool, len(b.names))
	for _, n := range b.names {
		if n != "" {
			locals[n] = true
		}
	}
	pi := irb.ParseInput(strings.TrimRight(code, "\n"), locals, false)
	if pi.IsCommand && (pi.Command == "irb_exit" || pi.Command == "irb_exit!") {
		return true
	}

	result, errClass, errMsg, raised := vm.irbEval(b, code)
	if raised {
		if vm.isSystemExit(errClass) {
			return true
		}
		out.writeStr(errMsg + " (" + errClass + ")\n")
		return false
	}
	bindingSetLocal(b, "_", result)
	out.writeStr(irb.FormatResult(mode.Return, vm.inspectStr(result)))
	return false
}

// bindingSetLocal sets a binding-local variable, extending the binding's name
// map and environment in lockstep when the name is new (so slot indices stay
// aligned for later depth-1 resolution) and overwriting the existing slot
// otherwise. It is how the REPL persists `_` across turns.
func bindingSetLocal(b *Binding, name string, v object.Value) {
	if i := b.slotOf(name); i >= 0 {
		b.env.slots[i] = v
		return
	}
	b.names = append(b.names, name)
	b.env.slots = append(b.env.slots, v)
}
