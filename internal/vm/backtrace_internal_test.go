// Copyright (c) the go-embedded-ruby/ruby authors
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestFrameFileLabelFallbacks drives frameFileLabel's three fallbacks directly:
// the frame's own file when set, the script name when the frame has none, and
// "(rbgo)" when neither is known.
func TestFrameFileLabelFallbacks(t *testing.T) {
	vm := New(nil)

	// Frame carries its own file: that wins regardless of scriptName.
	vm.frameFiles = []string{"/x/own.rb"}
	vm.scriptName = "ignored.rb"
	if got := vm.frameFileLabel(0); got != "/x/own.rb" {
		t.Fatalf("own-file: got %q", got)
	}

	// No frame file, a script name set: fall back to the script name.
	vm.frameFiles = []string{""}
	vm.scriptName = "main.rb"
	if got := vm.frameFileLabel(0); got != "main.rb" {
		t.Fatalf("script-name: got %q", got)
	}

	// No frame file, scriptName "-e": label "-e".
	vm.scriptName = "-e"
	if got := vm.frameFileLabel(0); got != "-e" {
		t.Fatalf("-e: got %q", got)
	}

	// No frame file, no script name: "(rbgo)".
	vm.scriptName = ""
	if got := vm.frameFileLabel(0); got != "(rbgo)" {
		t.Fatalf("rbgo: got %q", got)
	}
}

// TestBacktraceFramesSkipPastEnd: skipping more frames than exist yields nil.
func TestBacktraceFramesSkipPastEnd(t *testing.T) {
	vm := New(nil)
	vm.frameNames = []string{"a"}
	vm.frameFiles = []string{""}
	if got := vm.backtraceFrames(5); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// TestCaptureBacktraceEmptyFrames: with no live frames, captureBacktrace stores
// an empty Array (a raised-but-frameless backtrace), distinct from never-raised
// nil.
func TestCaptureBacktraceEmptyFrames(t *testing.T) {
	vm := New(nil)
	vm.frameNames = nil
	vm.frameFiles = nil
	exc := &RObject{class: vm.consts["RuntimeError"].(*RClass), ivars: map[string]object.Value{}}
	vm.captureBacktrace(exc)
	bt, ok := getIvar(exc, backtraceIvar).(*object.Array)
	if !ok {
		t.Fatalf("expected an Array backtrace, got %#v", getIvar(exc, backtraceIvar))
	}
	if len(bt.Elems) != 0 {
		t.Fatalf("expected empty backtrace, got %v", bt.Elems)
	}
}

// TestCaptureBacktraceKeepsExisting: a second capture (re-raise) leaves an
// already-stored backtrace untouched.
func TestCaptureBacktraceKeepsExisting(t *testing.T) {
	vm := New(nil)
	exc := &RObject{class: vm.consts["RuntimeError"].(*RClass), ivars: map[string]object.Value{}}
	orig := &object.Array{Elems: []object.Value{object.NewString("orig:0:in 'x'")}}
	setIvar(exc, backtraceIvar, orig)
	vm.frameNames = []string{"later"}
	vm.frameFiles = []string{"/y/other.rb"}
	vm.captureBacktrace(exc)
	got := getIvar(exc, backtraceIvar).(*object.Array)
	if got != orig {
		t.Fatalf("re-raise overwrote the backtrace: %v", got.Elems)
	}
}

// TestUncaughtBacktraceFallsBackToLiveStack: when the escaping RubyError carries
// an object without a stored backtrace, uncaughtBacktrace snapshots the live
// frame stack.
func TestUncaughtBacktraceFallsBackToLiveStack(t *testing.T) {
	vm := New(nil)
	vm.frameNames = []string{"top", "inner"}
	vm.frameFiles = []string{"/p.rb", "/p.rb"}
	exc := &RObject{class: vm.consts["RuntimeError"].(*RClass), ivars: map[string]object.Value{}}
	bt := vm.uncaughtBacktrace(RubyError{Obj: exc})
	if len(bt) != 2 {
		t.Fatalf("expected 2 live frames, got %v", bt)
	}
	if bt[0].ToS() != "/p.rb:0:in 'inner'" {
		t.Fatalf("got %q", bt[0].ToS())
	}
}

// TestRubyErrorBacktraceStrings checks RubyError.Backtrace stringifies its
// captured frames (the slice the CLI iterates), including the empty case.
func TestRubyErrorBacktraceStrings(t *testing.T) {
	if got := (RubyError{}).Backtrace(); len(got) != 0 {
		t.Fatalf("empty: got %v", got)
	}
	re := RubyError{Frames: []object.Value{object.NewString("a"), object.NewString("b")}}
	got := re.Backtrace()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}

// TestNormalizeBacktraceDirect exercises normalizeBacktrace's branches directly,
// including the nil-clear, single-String wrap and the two TypeError paths.
func TestNormalizeBacktraceDirect(t *testing.T) {
	if v := normalizeBacktrace(object.NilV); v != object.NilV {
		t.Fatalf("nil: got %#v", v)
	}
	if a, ok := normalizeBacktrace(object.NewString("s")).(*object.Array); !ok || len(a.Elems) != 1 {
		t.Fatalf("string: got %#v", normalizeBacktrace(object.NewString("s")))
	}
	wantRaise(t, "TypeError", func() { normalizeBacktrace(object.Integer(1)) })
	wantRaise(t, "TypeError", func() {
		normalizeBacktrace(&object.Array{Elems: []object.Value{object.Integer(1)}})
	})
}
