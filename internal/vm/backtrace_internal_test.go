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
	exc := &RObject{class: object.Kind[*RClass](vm.consts["RuntimeError"]), ivars: map[string]object.Value{}}
	vm.captureBacktrace(object.Wrap(exc))
	bt, ok := object.KindOK[*object.Array](getIvar(object.Wrap(exc), backtraceIvar))
	if !ok {
		t.Fatalf("expected an Array backtrace, got %#v", getIvar(object.Wrap(exc), backtraceIvar))
	}
	if len(bt.Elems) != 0 {
		t.Fatalf("expected empty backtrace, got %v", bt.Elems)
	}
}

// TestCaptureBacktraceKeepsExisting: a second capture (re-raise) leaves an
// already-stored backtrace untouched.
func TestCaptureBacktraceKeepsExisting(t *testing.T) {
	vm := New(nil)
	exc := &RObject{class: object.Kind[*RClass](vm.consts["RuntimeError"]), ivars: map[string]object.Value{}}
	orig := &object.Array{Elems: []object.Value{object.Wrap(object.NewString("orig:0:in 'x'"))}}
	setIvar(object.Wrap(exc), backtraceIvar, object.Wrap(orig))
	vm.frameNames = []string{"later"}
	vm.frameFiles = []string{"/y/other.rb"}
	vm.captureBacktrace(object.Wrap(exc))
	got := object.Kind[*object.Array](getIvar(object.Wrap(exc), backtraceIvar))
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
	exc := &RObject{class: object.Kind[*RClass](vm.consts["RuntimeError"]), ivars: map[string]object.Value{}}
	bt := vm.uncaughtBacktrace(RubyError{Obj: object.Wrap(exc)})
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
	re := RubyError{Frames: []object.Value{object.Wrap(object.NewString("a")), object.Wrap(object.NewString("b"))}}
	got := re.Backtrace()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}

// TestNormalizeBacktraceDirect exercises normalizeBacktrace's branches directly,
// including the nil-clear, single-String wrap and the two TypeError paths.
func TestNormalizeBacktraceDirect(t *testing.T) {
	if v := normalizeBacktrace(object.NilVal()); !object.IsNil(v) {
		t.Fatalf("nil: got %#v", v)
	}
	if a, ok := object.KindOK[*object.Array](normalizeBacktrace(object.Wrap(object.NewString("s")))); !ok || len(a.Elems) != 1 {
		t.Fatalf("string: got %#v", normalizeBacktrace(object.Wrap(object.NewString("s"))))
	}
	wantRaise(t, "TypeError", func() { normalizeBacktrace(object.IntValue(int64(object.Integer(1)))) })
	wantRaise(t, "TypeError", func() {
		normalizeBacktrace(object.Wrap(&object.Array{Elems: []object.Value{object.IntValue(int64(object.Integer(1)))}}))
	})
}
