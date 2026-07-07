// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser/ast"
)

// TestIRBSession drives whole IRB sessions with a scripted, in-memory $stdin
// (a StringIO) and asserts the captured $stdout. There is no live terminal:
// each case sets $stdin to a fixed script, runs IRB.start (or binding.irb), and
// compares the exact prompt+result transcript — so the read→eval-through-rbgo→
// print loop, multi-line continuation, `_`, persistent locals, error display,
// exit handling and EOF are all covered deterministically.
func TestIRBSession(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			// Evaluate an expression, echo `=> 2`, then the `exit` command ends the
			// loop. :SIMPLE keeps the prompt (">> ") free of line numbers.
			name: "eval_and_exit",
			src:  `$stdin = StringIO.new("1 + 1\nexit\n"); IRB.conf[:PROMPT_MODE] = :SIMPLE; IRB.start`,
			want: ">> => 2\n>> ",
		},
		{
			// The :DEFAULT prompt expands %N/%m/%03n, so the line number advances
			// across statements and the main object renders as "main".
			name: "default_prompt",
			src:  `$stdin = StringIO.new("1 + 1\nexit\n"); IRB.start`,
			want: "irb(main):001> => 2\nirb(main):002> ",
		},
		{
			// A def spans three lines: the continuation prompt ("?> ") shows while the
			// block is open, then `=> :foo` (def returns the method name), then the
			// method is called.
			name: "multiline_continuation",
			src:  `$stdin = StringIO.new("def foo\n42\nend\nfoo\nexit\n"); IRB.conf[:PROMPT_MODE] = :SIMPLE; IRB.start`,
			want: ">> ?> ?> => :foo\n>> => 42\n>> ",
		},
		{
			// An unterminated string literal keeps the loop reading; the string-
			// continuation prompt uses the ltype character (%l => `"`).
			name: "string_continuation",
			src:  "$stdin = StringIO.new(\"\\\"ab\\nba\\\"\\nexit\\n\"); IRB.conf[:PROMPT_MODE] = :SIMPLE; IRB.start",
			want: ">> \"> => \"ab\\nba\"\n>> ",
		},
		{
			// `_` holds the last result: 2 then `_ + 1` => 3.
			name: "last_result_underscore",
			src:  `$stdin = StringIO.new("1 + 1\n_ + 1\nexit\n"); IRB.conf[:PROMPT_MODE] = :SIMPLE; IRB.start`,
			want: ">> => 2\n>> => 3\n>> ",
		},
		{
			// A local assigned on one line is visible on the next (persistent scope).
			name: "persistent_locals",
			src:  `$stdin = StringIO.new("x = 2\nx * 5\nexit\n"); IRB.conf[:PROMPT_MODE] = :SIMPLE; IRB.start`,
			want: ">> => 2\n>> => 10\n>> ",
		},
		{
			// EOF (the script ends without `exit`) closes the session after a newline.
			name: "eof_ends_session",
			src:  `$stdin = StringIO.new("1 + 1\n"); IRB.conf[:PROMPT_MODE] = :SIMPLE; IRB.start`,
			want: ">> => 2\n>> \n",
		},
		{
			// The `exit!` alias also ends the loop.
			name: "exit_bang",
			src:  `$stdin = StringIO.new("exit!\n"); IRB.conf[:PROMPT_MODE] = :SIMPLE; IRB.start`,
			want: ">> ",
		},
		{
			// A Kernel#exit call inside evaluated code raises SystemExit, which ends
			// the session cleanly rather than being reported as an error.
			name: "systemexit_in_eval",
			src:  `$stdin = StringIO.new("exit(0)\n"); IRB.conf[:PROMPT_MODE] = :SIMPLE; IRB.start`,
			want: ">> ",
		},
		{
			// binding.irb runs against the caller's binding, so the session sees the
			// local `y` captured there.
			name: "binding_irb",
			src:  `y = 99; $stdin = StringIO.new("y\nexit\n"); IRB.conf[:PROMPT_MODE] = :SIMPLE; binding.irb`,
			want: ">> => 99\n>> ",
		},
		{
			// IRB.version reports the embedded build.
			name: "version",
			src:  `$stdout.print IRB.version`,
			want: "irb (go-embedded-ruby)",
		},
		{
			// A $stdin rebound to a non-IO value has nothing to read: the session
			// returns immediately with no output.
			name: "no_stdin",
			src:  `$stdin = nil; IRB.start`,
			want: "",
		},
		{
			// require "irb" reports the feature as freshly loaded (true) the first
			// time and already-loaded (false) the second, like a normal gem.
			name: "require_feature",
			src:  `$stdout.print [require("irb"), require("irb")].inspect`,
			want: "[true, false]",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runFS(t, c.src); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

// TestIRBErrorDisplay covers the exception branch: a bare undefined name raises,
// and the REPL reports it MRI-style ("message (Class)") on $stdout and keeps
// going.
func TestIRBErrorDisplay(t *testing.T) {
	got := runFS(t, `$stdin = StringIO.new("nope\nexit\n"); IRB.conf[:PROMPT_MODE] = :SIMPLE; IRB.start`)
	if !strings.Contains(got, "(NoMethodError)") {
		t.Errorf("expected a NoMethodError report, got %q", got)
	}
	if !strings.HasPrefix(got, ">> ") || !strings.HasSuffix(got, ">> ") {
		t.Errorf("expected the loop to survive the error, got %q", got)
	}
}

// TestIrbModeFromConf covers the :PROMPT_MODE resolution: a valid mode maps to
// its table, while an absent key, a non-symbol value, and an unknown mode all
// fall back to :DEFAULT.
func TestIrbModeFromConf(t *testing.T) {
	if m := irbModeFromConf(object.NewHash()); m.Name != "DEFAULT" {
		t.Errorf("empty conf: got %q want DEFAULT", m.Name)
	}

	simple := object.NewHash()
	simple.Set(object.SymVal("PROMPT_MODE"), object.SymVal("SIMPLE"))
	if m := irbModeFromConf(simple); m.Name != "SIMPLE" {
		t.Errorf("SIMPLE: got %q", m.Name)
	}

	notSym := object.NewHash()
	notSym.Set(object.SymVal("PROMPT_MODE"), object.NewString("SIMPLE"))
	if m := irbModeFromConf(notSym); m.Name != "DEFAULT" {
		t.Errorf("non-symbol: got %q want DEFAULT", m.Name)
	}

	unknown := object.NewHash()
	unknown.Set(object.SymVal("PROMPT_MODE"), object.SymVal("BOGUS"))
	if m := irbModeFromConf(unknown); m.Name != "DEFAULT" {
		t.Errorf("unknown mode: got %q want DEFAULT", m.Name)
	}
}

// TestIRBEvalFrontEndErrors covers irbEval's parse- and compile-error branches.
// A complete statement that has already passed irb.CheckCode virtually never
// fails to parse or compile, so both failures are injected through the front-end
// seams and asserted to surface as a raised SyntaxError carrying the message.
func TestIRBEvalFrontEndErrors(t *testing.T) {
	vm := New(&bytes.Buffer{})
	newBinding := func() *Binding { return &Binding{env: &Env{}, self: vm.main, definee: vm.cObject} }

	savedParse := irbParse
	irbParse = func(string) (*ast.Program, error) { return nil, errors.New("parse boom") }
	_, cls, msg, raised := vm.irbEval(newBinding(), "whatever")
	irbParse = savedParse
	if !raised || cls != "SyntaxError" || msg != "parse boom" {
		t.Fatalf("parse error: raised=%v cls=%q msg=%q", raised, cls, msg)
	}

	savedCompile := irbCompileWithLocals
	irbCompileWithLocals = func(*ast.Program, []string) (*bytecode.ISeq, error) {
		return nil, errors.New("compile boom")
	}
	_, cls, msg, raised = vm.irbEval(newBinding(), "1 + 1")
	irbCompileWithLocals = savedCompile
	if !raised || cls != "SyntaxError" || msg != "compile boom" {
		t.Fatalf("compile error: raised=%v cls=%q msg=%q", raised, cls, msg)
	}
}

// TestIRBEvalRepropagatesSignal covers irbEval's non-exception recovery branch:
// a break/throw/return signal is not a Ruby exception the REPL can display, so
// it must re-propagate to the enclosing Run boundary (which turns an uncaught
// throw into an UncaughtThrowError) rather than being swallowed.
func TestIRBEvalRepropagatesSignal(t *testing.T) {
	vm := New(&bytes.Buffer{})
	b := &Binding{env: &Env{}, self: vm.main, definee: vm.cObject}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected the escaping throw signal to re-propagate")
		}
		if _, ok := r.(RubyError); ok {
			t.Fatalf("expected a non-RubyError signal, got RubyError %v", r)
		}
	}()
	vm.irbEval(b, "throw :zzz")
	t.Fatal("irbEval should have re-panicked")
}
