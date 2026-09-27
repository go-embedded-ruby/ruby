// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// TestEvalLocationArguments is the WITNESS for the (file, line) pair every eval
// entry point takes: MRI compiles the string AT that location, so __FILE__ and
// __LINE__ inside it, the backtrace of anything it raises and the
// #source_location of anything it defines all report the given pair rather than
// the caller's. Every expectation was measured against ruby 4.0.5.
func TestEvalLocationArguments(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// Kernel#eval, all four arities.
		{`p eval("__LINE__")`, "1"},
		{`b = binding; p eval("__LINE__", b)`, "1"},
		{`b = binding; p eval("__FILE__", b, "f.rb")`, `"f.rb"`},
		{`b = binding; p eval("__LINE__", b, "f.rb", 10)`, "10"},
		{`p eval("__FILE__", nil, "z.rb")`, `"z.rb"`},
		{`p eval("[__FILE__, __LINE__]", nil, "z.rb", 4)`, `["z.rb", 4]`},
		// A later line of a multi-line string counts on from the given one.
		{`p eval("#c\n__LINE__", nil, "z.rb", 40)`, "41"},
		// instance_eval / module_eval take the same pair as argv[1]/argv[2].
		{`p Object.new.instance_eval("[__FILE__, __LINE__]", "q.rb", 5)`, `["q.rb", 5]`},
		{`p Module.new.module_eval("[__FILE__, __LINE__]", "m.rb", 7)`, `["m.rb", 7]`},
		{`p Class.new.class_eval("[__FILE__, __LINE__]", "c.rb", 9)`, `["c.rb", 9]`},
		// Binding#eval(src, file, line) is bind_eval: 1..3 arguments.
		{`b = binding; p b.eval("[__FILE__, __LINE__]", "bf.rb", 44)`, `["bf.rb", 44]`},
		{`b = binding; p b.eval("__LINE__", "bf.rb")`, "1"},
		// A NEGATIVE first line is legal and is not clamped.
		{`p eval("\n\n__LINE__", nil, "n.rb", -100)`, "-98"},
		// The lineno argument is coerced through #to_int.
		{`n = Object.new; def n.to_int; 15; end; p eval("__LINE__", nil, "t.rb", n)`, "15"},
		// An explicit nil SCOPE is an absent one: it falls back to the caller.
		{`p eval("1 + 1", nil)`, "2"},
	} {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestEvalLocationReachesDefinedThings pins the consequence that motivates
// carrying the pair into the COMPILATION rather than onto the finished ISeq: a
// proc or method the string defines reports the eval's location, because MRI's
// whole eval ISeq tree carries one path and one line origin.
func TestEvalLocationReachesDefinedThings(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p eval("-> {}", nil, "foo", 100).source_location`, `["foo", 100]`},
		{`c = Class.new { eval('def self.m; end', nil, "foo", 100) }; p c.method(:m).source_location`, `["foo", 100]`},
		{`c = Class.new { eval('def m; end', nil, "foo", 100) }; p c.instance_method(:m).source_location`, `["foo", 100]`},
		{`p Object.new.instance_eval("proc {}", "q.rb", 5).source_location`, `["q.rb", 5]`},
		// The line map reaches the BACKTRACE too, which is what
		// instance_eval("raise", "a_file", 10) is pinned on.
		{`e = (Object.new.instance_eval("raise", "a_file", 10) rescue $!); p e.backtrace.first.split(":")[0..1]`, `["a_file", "10"]`},
		{`e = (Object.new.instance_eval("\n\nraise\n", "b_file", -100) rescue $!); p e.backtrace.first.split(":")[0..1]`, `["b_file", "-98"]`},
	} {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestEvalSyntaxErrorNamesFileAndLine is the witness for the message shape: MRI
// prefixes "file:line: ", so a SyntaxError from an eval says WHERE. rbgo reported
// only what went wrong, which is why ruby/spec's
// /speccing.rb:1:.+/ could not match.
func TestEvalSyntaxErrorNamesFileAndLine(t *testing.T) {
	for _, c := range []struct{ src, wantPrefix string }{
		{`begin; eval("if true", TOPLEVEL_BINDING, "speccing.rb"); rescue SyntaxError => e; print e.message; end`, "speccing.rb:1: "},
		{`begin; eval("if true", TOPLEVEL_BINDING, "speccing.rb", -100); rescue SyntaxError => e; print e.message; end`, "speccing.rb:-100: "},
		{`begin; eval("if true", nil, "s.rb", 7); rescue SyntaxError => e; print e.message; end`, "s.rb:7: "},
		{`begin; Object.new.instance_eval("if true", "i.rb", 3); rescue SyntaxError => e; print e.message; end`, "i.rb:3: "},
		{`begin; binding.eval("if true", "b.rb", 9); rescue SyntaxError => e; print e.message; end`, "b.rb:9: "},
		// A parse error on the SECOND line of the string is reported on the second
		// line of the eval's numbering, not on its first.
		{`begin; eval("1\nif true", nil, "s.rb", 7); rescue SyntaxError => e; print e.message; end`, "s.rb:8: "},
	} {
		got := eval(t, c.src)
		if !strings.HasPrefix(got, c.wantPrefix) {
			t.Errorf("src=%q message %q does not start with %q", c.src, got, c.wantPrefix)
		}
	}
}

// TestEvalDefaultPathIsEvalAt covers get_eval_default_path: with no filename MRI
// does not borrow the caller's path but builds "(eval at FILE:LINE)". Compiled-in
// code (this harness) has no path at all, which is MRI's Qnil arm — the bare
// "(eval)".
func TestEvalDefaultPathIsEvalAt(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p eval("__FILE__")`, `"(eval)"`},
		{`p Object.new.instance_eval("__FILE__")`, `"(eval)"`},
		{`p Module.new.module_eval("__FILE__")`, `"(eval)"`},
		{`p binding.eval("__FILE__")`, `"(eval)"`},
		// An explicitly given filename wins over the default.
		{`p eval("__FILE__", nil, "given.rb")`, `"given.rb"`},
	} {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// And the synthetic path names a real caller location when there is one: the
	// pair comes from sourceLocation, the innermost Ruby frame.
	var buf bytes.Buffer
	vmv := New(&buf)
	vmv.frameNames = []string{"<main>"}
	vmv.frameCode = []frameCode{{
		iseq: &bytecode.ISeq{File: "prog.rb", Lines: []bytecode.LineEntry{{PC: 0, Line: 12}}},
	}}
	if got := vmv.evalDefaultPath(); got != "(eval at prog.rb:12)" {
		t.Errorf("evalDefaultPath() = %q, want %q", got, "(eval at prog.rb:12)")
	}
}

// TestEvalRejectsANonBindingScope: rb_f_eval routes a non-nil scope through
// Check_TypedStruct, so anything that is not a Binding is a TypeError naming the
// type it got. rbgo used to IGNORE the argument and run against the caller, so
// eval(str, proc {}) quietly did something else.
func TestEvalRejectsANonBindingScope(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`begin; eval("1", proc {}); rescue TypeError => e; print e.message; end`, "wrong argument type proc (expected binding)"},
		{`begin; eval("1", method(:puts)); rescue TypeError => e; print e.message; end`, "wrong argument type method (expected binding)"},
		{`begin; eval("1", 42); rescue TypeError => e; print e.message; end`, "wrong argument type Integer (expected binding)"},
		{`begin; eval("1", Object.new); rescue TypeError => e; print e.message; end`, "wrong argument type Object (expected binding)"},
		{`begin; eval("1", Object); rescue TypeError => e; print e.message; end`, "wrong argument type Class (expected binding)"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestEvalNilFileAndLineArguments: an explicit Ruby nil is where rb_f_eval and
// specific_eval part. rb_f_eval runs StringValue(vfile) whenever argc >= 3 and
// only then tests NIL_P, so eval(src, nil, nil) raises; specific_eval tests
// NIL_P first, so instance_eval(src, nil) takes the default path. Binding#eval
// splices the binding into rb_f_eval's argv, so it raises like eval. A nil LINE
// is NUM2INT on nil in both families, with rb_to_int's own wording. All nine
// measured against ruby 4.0.5.
func TestEvalNilFileAndLineArguments(t *testing.T) {
	const nilStr = "no implicit conversion of nil into String"
	const nilInt = "no implicit conversion from nil to integer"
	for _, c := range []struct{ src, want string }{
		{`begin; eval("1", nil, nil); rescue TypeError => e; print e.message; end`, nilStr},
		{`begin; eval("1", nil, nil, 5); rescue TypeError => e; print e.message; end`, nilStr},
		{`begin; eval("1", nil, "f", nil); rescue TypeError => e; print e.message; end`, nilInt},
		{`begin; binding.eval("1", nil); rescue TypeError => e; print e.message; end`, nilStr},
		{`begin; binding.eval("1", nil, 7); rescue TypeError => e; print e.message; end`, nilStr},
		{`begin; binding.eval("1", "f", nil); rescue TypeError => e; print e.message; end`, nilInt},
		// specific_eval's family takes the default path for a nil filename.
		{`print Object.new.instance_eval("__FILE__", nil)`, "(eval)"},
		{`print Object.new.instance_eval("__FILE__", nil, 5)`, "(eval)"},
		{`print Module.new.module_eval("__FILE__", nil)`, "(eval)"},
		{`print Class.new.class_eval("__FILE__", nil)`, "(eval)"},
		// but still raises for a nil line.
		{`begin; Object.new.instance_eval("1", "f", nil); rescue TypeError => e; print e.message; end`, nilInt},
		{`begin; Module.new.module_eval("1", "f", nil); rescue TypeError => e; print e.message; end`, nilInt},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestEvalLinenoCoercionErrors: NUM2INT on the lineno argument reports
// rb_to_integer's message when #to_int answers with a non-Integer, and the plain
// implicit-conversion TypeError when there is no #to_int at all.
func TestEvalLinenoCoercionErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`n = Object.new; def n.to_int; :sym; end; begin; Object.new.instance_eval("1", "f", n); rescue TypeError => e; print e.message; end`,
			"can't convert Object to Integer (Object#to_int gives Symbol)"},
		{`begin; Object.new.instance_eval("1", "f", Object.new); rescue TypeError => e; print e.message; end`,
			"no implicit conversion of Object into Integer"},
		{`begin; eval("1", nil, "f", Object.new); rescue TypeError => e; print e.message; end`,
			"no implicit conversion of Object into Integer"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestEvalArity: rb_f_eval scans "13" — one required argument and three optional
// ones — so five is an ArgumentError, and Binding#eval scans "12".
func TestEvalArity(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`begin; eval("1", nil, "f", 1, 2); rescue ArgumentError => e; print e.message; end`,
			"wrong number of arguments (given 5, expected 1..4)"},
		{`begin; binding.eval("1", "f", 1, 2); rescue ArgumentError => e; print e.message; end`,
			"wrong number of arguments (given 4, expected 1..3)"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestEvalDefineeIsTheModuleItself: a `def` inside eval(str) lands in the
// caller's cref, which for a Module or Class self is that module — not its class.
// classOf(self) answered Class, so Class.new { eval("def m; end") } put m on
// Class and the new class reported none.
func TestEvalDefineeIsTheModuleItself(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// The ONE-argument form is rewritten by the compiler into eval(src, binding)
		// (compiler.go, the `eval` intrinsic), so its definee comes from the binding
		// and this arm does not exercise evalDefinee. Every other arity — and a
		// dispatch that the rewrite cannot see, such as #send — reaches it, so each
		// case below is stated in one of those forms.
		{`c = Class.new { eval("def m; 1; end", nil, "f", 1) }; p c.new.m`, "1"},
		{`c = Class.new { eval("def m; end", nil, "f", 1) }; p c.instance_method(:m).owner.equal?(c)`, "true"},
		{`c = Class.new { send(:eval, "def m; end") }; p c.instance_method(:m).owner.equal?(c)`, "true"},
		{`c = Class.new { eval("def m; 1; end") }; p c.new.m`, "1"},
		{`m = Module.new { eval("def mm; 2; end", nil, "f", 1) }; k = Class.new { include m }; p k.new.mm`, "2"},
		// An ordinary object's cref is still its class.
		{`class EvD; def go; send(:eval, "def later; 3; end"); end; end; o = EvD.new; o.go; p o.later`, "3"},
	} {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestModuleEvalBlockYieldsTheModule: specific_eval's block arm is
// yield_under(self, …, 1, &self, …) — one argument, the receiver. rbgo passed
// none, so `Foo.class_eval { |m| m }` answered nil.
func TestModuleEvalBlockYieldsTheModule(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`module EvMB; end; p EvMB.module_eval { |m| m }`, "EvMB"},
		{`module EvMC; end; p EvMC.class_eval { |m| m.equal?(EvMC) }`, "true"},
		// A block that takes no parameter is unaffected.
		{`module EvMD; end; p EvMD.module_eval { 7 }`, "7"},
	} {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestParseErrorLine covers the message split raiseEvalSyntaxError depends on,
// including the shapes it must NOT claim to understand — a message it cannot
// split reports the eval's first line rather than a wrong one.
func TestParseErrorLine(t *testing.T) {
	for _, c := range []struct {
		msg  string
		n    int
		rest string
		ok   bool
	}{
		{"parse error at line 3: bad", 3, "bad", true},
		{"parse error at line 1: a: b", 1, "a: b", true},
		{"parse error at line -4: bad", -4, "bad", true},
		{"compile error: Invalid break", 0, "compile error: Invalid break", false},
		{"parse error at line x: bad", 0, "parse error at line x: bad", false},
		{"parse error at line 3", 0, "parse error at line 3", false},
	} {
		n, rest, ok := parseErrorLine(c.msg)
		if n != c.n || rest != c.rest || ok != c.ok {
			t.Errorf("parseErrorLine(%q) = (%d, %q, %v), want (%d, %q, %v)", c.msg, n, rest, ok, c.n, c.rest, c.ok)
		}
	}
}
