// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// TestWaveGVCallMissRaisesNameError pins MRI's one call shape whose miss changes
// both the exception class and the message. rb_method_call_status turns an
// undefined entry reached with CALL_VCALL into MISSING_VCALL (vm_eval.c:854) and
// raise_method_missing's MISSING_VCALL branch (vm_eval.c:945) swaps the format
// to "undefined local variable or method '%1$s' for %3$s%4$s" AND exc to
// rb_eNameError. Every expectation below was read off ruby 4.0.5 first.
//
// The probe varies with its input by construction: only the FIRST case is a
// VCALL, and the rest are the near-miss shapes (explicit receiver, self
// receiver, a literal block, an argument) that MRI keeps on NoMethodError. A
// change that simply raised NameError everywhere fails five of the six.
func TestWaveGVCallMissRaisesNameError(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// The VCALL itself.
		{`begin; nope_xyz; rescue Exception => e; p [e.class, e.message]; end`,
			`[NameError, "undefined local variable or method 'nope_xyz' for main"]`},
		// A literal block makes it a call, not a bare name (MRI: NoMethodError).
		{`begin; nope_xyz {}; rescue Exception => e; p [e.class, e.message]; end`,
			`[NoMethodError, "undefined method 'nope_xyz' for main"]`},
		// An explicit self receiver is NODE_CALL, not NODE_VCALL.
		{`begin; self.nope_xyz; rescue Exception => e; p [e.class, e.message]; end`,
			`[NoMethodError, "undefined method 'nope_xyz' for main"]`},
		// A non-self receiver, and the receiver rendering that goes with it.
		{`begin; 42.nope_xyz; rescue Exception => e; p [e.class, e.message]; end`,
			`[NoMethodError, "undefined method 'nope_xyz' for an instance of Integer"]`},
		// An argument makes it NODE_FCALL.
		{`begin; nope_xyz 1; rescue Exception => e; p [e.class, e.message]; end`,
			`[NoMethodError, "undefined method 'nope_xyz' for main"]`},
		// Object#send names the method explicitly: never a VCALL, whatever the
		// call site that reached send looked like.
		{`begin; send(:nope_xyz); rescue Exception => e; p e.class; end`, `NoMethodError`},
	} {
		if got := strings.TrimSpace(eval(t, c.src)); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.src, got, c.want)
		}
	}
}

// TestWaveGVCallNameErrorCarriesNameAndReceiver covers what
// rb_make_no_method_exception (vm_eval.c:927) builds on the VCALL side: it takes
// the rb_name_err_new branch, so #name and #receiver are set and #args — which
// rb_nomethod_err_new alone adds — is NOT defined. Both halves measured on 4.0.5.
func TestWaveGVCallNameErrorCarriesNameAndReceiver(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`begin; nope_xyz; rescue NameError => e; p [e.name, e.receiver]; end`, `[:nope_xyz, main]`},
		{`begin; nope_xyz; rescue NameError => e; p e.respond_to?(:args); end`, `false`},
		{`begin; nope_xyz; rescue NameError => e; p e.is_a?(NoMethodError); end`, `false`},
		// The NoMethodError side keeps #args, so the change did not cost it.
		{`begin; 42.nope_xyz(1, 2); rescue NoMethodError => e; p e.args; end`, `[1, 2]`},
	} {
		if got := strings.TrimSpace(eval(t, c.src)); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.src, got, c.want)
		}
	}
}

// TestWaveGVCallVerdictDoesNotLeak is the control for VM.sendVCall's lifetime.
// The verdict is one ambient word, so the risk it carries is that a VCALL that
// SUCCEEDS leaves it standing for an unrelated miss deeper in the program — the
// exact shape the AOT lane exhibited before invokeBody's compiled arm cleared
// it. Each case reaches a NoMethodError THROUGH a successful bare call.
func TestWaveGVCallVerdictDoesNotLeak(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// Bare call -> interpreted body -> explicit-receiver miss inside it.
		{"def outer; 42.nope_xyz; end\nbegin; outer; rescue Exception => e; p e.class; end", `NoMethodError`},
		// Bare call -> interpreted body -> an FCALL miss inside it.
		{"def outer; nope_xyz(1); end\nbegin; outer; rescue Exception => e; p e.class; end", `NoMethodError`},
		// Bare call -> NATIVE body (Kernel#loop) -> a block that misses with a
		// receiver. callNative clears the verdict before the native body runs.
		{"def outer; [1].each { |x| x.nope_xyz }; end\nbegin; outer; rescue Exception => e; p e.class; end", `NoMethodError`},
		// A VCALL that succeeds, then a separate miss at the same level.
		{"def ok; 1; end\nok\nbegin; 42.nope_xyz; rescue Exception => e; p e.class; end", `NoMethodError`},
		// And the converse: a preceding explicit-receiver miss must not stop the
		// next bare one being a VCALL.
		{"begin; 42.nope_xyz; rescue Exception; end\nbegin; nope_xyz; rescue Exception => e; p e.class; end", `NameError`},
	} {
		if got := strings.TrimSpace(eval(t, c.src)); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.src, got, c.want)
		}
	}
}

// TestWaveGVCallUserMethodMissingWins pins that the verdict changes only the
// DEFAULT hook's raise. MRI reaches raise_method_missing solely through
// BasicObject#method_missing, so a user hook sees the call and nothing is
// raised at all.
func TestWaveGVCallUserMethodMissingWins(t *testing.T) {
	src := "def method_missing(n, *a); [n, a]; end\np nope_xyz\np nope_xyz(1)\n"
	if got := strings.TrimSpace(eval(t, src)); got != "[:nope_xyz, []]\n[:nope_xyz, [1]]" {
		t.Errorf("user method_missing: got %q", got)
	}
}

// TestWaveGAOTSendStatesTheVCallVerdict is the AOT half (G2) at unit level: the
// lane must reach the interpreter's verdict from the same flags, or an
// AOT-compiled call site and its interpreted twin disagree silently. aotSend
// takes the raw flags, so the two directions are checked on one call site each.
//
// The end-to-end witness is `rbgo build` over a program whose <main> level-2
// lowers: `nope_xyz` alone answers NameError from the built binary and
// `42.nope_xyz` answers NoMethodError, both matching the interpreter. Removing
// the one line in aotSend makes the first answer NoMethodError — measured.
func TestWaveGAOTSendStatesTheVCallVerdict(t *testing.T) {
	for _, c := range []struct {
		name  string
		flags int
		recv  object.Value
		want  string
	}{
		{"vcall", bytecode.FlagSendVCall, nil, "NameError"},
		{"explicit", bytecode.FlagSendExplicit, object.Integer(42), "NoMethodError"},
		{"plain fcall", 0, nil, "NoMethodError"},
	} {
		vm := New(io.Discard)
		var ic inlineCache
		recv := c.recv
		if recv == nil {
			recv = vm.main
		}
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("%s: aotSend did not raise", c.name)
					return
				}
				re, ok := r.(RubyError)
				if !ok {
					panic(r)
				}
				if re.Class != c.want {
					t.Errorf("%s: aotSend raised %s, want %s (%q)", c.name, re.Class, c.want, re.Message)
				}
			}()
			vm.aotSend(&ic, recv, "nope_xyz", nil, c.flags, vm.main, nil)
		}()
	}
}

// evalOn is eval with a VM the caller has already prepared, so a test can plant
// a probe method the Ruby source then calls.
func evalOn(t *testing.T, vm *VM, buf *bytes.Buffer, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := vm.Run(iseq); err != nil {
		t.Fatalf("run %q: %v", src, err)
	}
	return strings.TrimSpace(buf.String())
}

// TestWaveGVCallVerdictIsClearedForAnOperatorMiss witnesses exec's clear. An
// operator is a compiler fast path, not an OpSend, so it is the one miss inside
// an interpreted body that no send site has already overwritten the verdict for:
// with the clear removed, `K + 1` reached from a bare `outer` answers "undefined
// local variable or method '+'" — measured. MRI: NoMethodError, because the
// operator call is not CALL_VCALL.
func TestWaveGVCallVerdictIsClearedForAnOperatorMiss(t *testing.T) {
	src := "class C; end\nK = C.new\ndef outer; K + 1; end\n" +
		"begin; outer; rescue Exception => e; p [e.class, e.message]; end"
	const want = `[NoMethodError, "undefined method '+' for an instance of C"]`
	if got := strings.TrimSpace(eval(t, src)); got != want {
		t.Errorf("operator miss under a VCALL frame:\n got %s\nwant %s", got, want)
	}
}

// TestWaveGVCallVerdictIsClearedForANativeBody witnesses callNative's clear. A
// native body reached BY a VCALL that resolved must not hand its own internal
// dispatch the caller's verdict: MRI's C functions call through rb_funcall,
// whose call_status is CALL_FCALL, so an undefined name there is MISSING_NOENTRY
// and NoMethodError. The probe is planted rather than found because every native
// rbgo has that both takes no arguments and dispatches internally is reached
// through some other shape; the mechanism, not the caller, is what is pinned.
func TestWaveGVCallVerdictIsClearedForANativeBody(t *testing.T) {
	var buf bytes.Buffer
	vm := New(&buf)
	vm.cKernel.define("wave_g_probe_native", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.send(self, "nope_xyz", nil, nil)
	})
	// `wave_g_probe_native` is bare, zero-argument and receiver-less: a VCALL.
	got := evalOn(t, vm, &buf, "begin; wave_g_probe_native; rescue Exception => e; p e.class; end")
	if got != "NoMethodError" {
		t.Errorf("a native body inherited its caller's VCALL verdict: got %s, want NoMethodError", got)
	}
}

// TestWaveGVCallVerdictIsClearedForACompiledBody witnesses invokeBody's compiled
// arm. The body planted here is exactly what internal/aot/codegen.go emits for
// `42.nope_xyz` — setSendNoKW plus a bare dispatchSend, with no site verdict of
// its own — so without the clear it inherits the bare call that reached it and
// answers NameError. That is not hypothetical: `rbgo build` over
// `def probe_recv; 42.nope_xyz; end` did exactly that before the clear landed.
func TestWaveGVCallVerdictIsClearedForACompiledBody(t *testing.T) {
	var buf bytes.Buffer
	vm := New(&buf)
	vm.cKernel.methods["wave_g_probe_compiled"] = &Method{
		name:  "wave_g_probe_compiled",
		owner: vm.cKernel,
		compiled: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			vm.setSendNoKW(false)
			return vm.dispatchSend(object.Integer(42), "nope_xyz", []object.Value{}, nil)
		},
	}
	bumpMethodSerial()
	got := evalOn(t, vm, &buf, "begin; wave_g_probe_compiled; rescue Exception => e; p e.class; end")
	if got != "NoMethodError" {
		t.Errorf("an AOT-compiled body inherited its caller's VCALL verdict: got %s, want NoMethodError", got)
	}
}

// TestWaveGVCallNameShape pins, end to end, the half of MRI's rule that the
// call-site SHAPE cannot express. `nope!` and `nope?` are written with no
// receiver, no parentheses, no arguments and no block, yet they lex as tFID and
// `primary : tFID` builds NEW_FCALL (parse.y-ruby_4_0:4370), while gettable
// reaches NEW_VCALL only for a plain tIDENTIFIER (parse.y-ruby_4_0:13086).
//
// The rule now lives once, in the compiler (compiler.vcallName), so
// bytecode.FlagSendVCall is NOT set for these two and the VM reads the flag
// alone; the predicate that used to re-check the name here is gone. This test
// keeps the observable half, which is what MRI actually promises. Both MRI
// 4.0.5 parsers were run:
//
//	                       --parser=parse.y   --parser=prism (the default)
//	nope_xyz               NameError          NameError
//	nope_xyz!              NoMethodError      NoMethodError
//	nope_xyz?              NoMethodError      NoMethodError
//
// The two instruments agree here, so this is not one of the places where the
// grammar oracle and the stock binary diverge. (They DO diverge on the
// value-omitted hash shorthand `{x:}` — parse.y makes it a VCALL, Prism an
// FCALL — which is recorded in TestWaveGVCallHashShorthandFollowsParseY.)
func TestWaveGVCallNameShape(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`begin; nope_xyz!; rescue Exception => e; p [e.class, e.message]; end`,
			`[NoMethodError, "undefined method 'nope_xyz!' for main"]`},
		{`begin; nope_xyz?; rescue Exception => e; p [e.class, e.message]; end`,
			`[NoMethodError, "undefined method 'nope_xyz?' for main"]`},
		// And the plain identifier beside them, so the probe varies with its input.
		{`begin; nope_xyz; rescue Exception => e; p [e.class, e.message]; end`,
			`[NameError, "undefined local variable or method 'nope_xyz' for main"]`},
	} {
		if got := strings.TrimSpace(eval(t, c.src)); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.src, got, c.want)
		}
	}
}

// TestWaveGVCallHashShorthandFollowsParseY records, rather than asserts as
// correct, the one witness on which MRI 4.0.5's two parsers disagree. In
// parse.y the value-omitted shorthand `{x:}` goes through gettable and becomes
// a VCALL; Prism — which the stock `ruby` binary defaults to (+PRISM) — makes it
// an FCALL:
//
//	p({nope_xyz:})   --parser=parse.y  NameError
//	                 --parser=prism    NoMethodError   <- stock `ruby`
//
// rbgo answers parse.y's NameError, and NOT by choice: go-ruby-parser v0.8.0
// gives the shorthand value the same receiver-less, argument-less *ast.Call a
// bare identifier gets, so the compiler's shape test cannot tell them apart and
// the VM has no name to distinguish either. Matching Prism needs the parser to
// mark the shorthand and the compiler to withhold the flag for it — both
// outside this change's cluster. This test exists so the divergence is on the
// record and a later change has to say it moved it deliberately.
func TestWaveGVCallHashShorthandFollowsParseY(t *testing.T) {
	src := `begin; p({nope_xyz:}); rescue Exception => e; p e.class; end`
	if got := strings.TrimSpace(eval(t, src)); got != "NameError" {
		t.Errorf("hash shorthand: got %s, want NameError (parse.y's answer; "+
			"stock ruby/Prism says NoMethodError — if this moved, say which "+
			"parser rbgo now follows)", got)
	}
}
