// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// runHost compiles and runs src in one VM after letting seed configure it the way
// a host does before Run, and returns everything the program wrote.
func runHost(t *testing.T, src string, seed func(*VM)) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	vm := New(&buf)
	if seed != nil {
		seed(vm)
	}
	if _, err := vm.Run(iseq); err != nil {
		t.Fatalf("run: %v", err)
	}
	return buf.String()
}

// TestSetARGVIsRubySetArgv covers the host half of #708. Every "want" was measured
// against MRI ruby 4.0.5 with the equivalent command line.
//
// The identity rows are the point: ruby_set_argv clears and refills the ONE Array
// that ARGV and $* both name, so a fresh Array installed over the constant would
// leave $* empty and ARGF (argf.go, which shifts filenames off the live ARGV)
// drawing from whichever of the two it happened to hold.
//
// A/B: replace the in-place refill with `vm.consts["ARGV"] = object.NewArray(...)`
// — it compiles, and the `$*` rows fail while the plain `p ARGV` row still passes.
func TestSetARGVIsRubySetArgv(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		src  string
		want string
	}{
		{"arguments arrive in order", []string{"a", "b"}, "p ARGV", "[\"a\", \"b\"]\n"},
		{"no arguments is an empty Array, not nil", nil, "p ARGV", "[]\n"},
		{"$* is the same object", []string{"a"}, "p ARGV.equal?($*)", "true\n"},
		{"a mutation through ARGV shows in $*", []string{"a", "b"}, "ARGV.shift; p $*", "[\"b\"]\n"},
		{"a mutation through $* shows in ARGV", []string{"a", "b"}, "$*.pop; p ARGV", "[\"a\"]\n"},
		// ruby_set_argv does OBJ_FREEZE(arg) on every element.
		{"elements are frozen", []string{"a", "b"}, "p ARGV.map(&:frozen?)", "[true, true]\n"},
		{"a frozen element raises on mutation", []string{"a"},
			"begin; ARGV[0] << \"x\"; rescue => e; p e.class; end", "FrozenError\n"},
		{"the empty string survives as an argument", []string{"", "b"}, "p ARGV", "[\"\", \"b\"]\n"},
		{"a second call replaces rather than appends", []string{"a"}, "p ARGV", "[\"a\"]\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := runHost(t, c.src, func(vm *VM) {
				if c.name == "a second call replaces rather than appends" {
					vm.SetARGV([]string{"stale", "values"})
				}
				vm.SetARGV(c.args)
			})
			if got != c.want {
				t.Errorf("%s = %q, want %q", c.src, got, c.want)
			}
		})
	}
}

// TestSetARGVRepairsAReplacedConstant covers the branch where a host put
// something other than an Array under ARGV: the Array is rebuilt and $* is
// re-pointed at it, so the two cannot end up naming different objects.
func TestSetARGVRepairsAReplacedConstant(t *testing.T) {
	got := runHost(t, "p [ARGV, ARGV.equal?($*)]", func(vm *VM) {
		vm.SetConst("ARGV", object.NewString("not an array"))
		vm.SetARGV([]string{"a"})
	})
	if want := "[[\"a\"], true]\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSetWarningLevelIsProcWOption covers the host half of #709: the three states
// -W<level> selects. Measured with `ruby -W0 / -W1 / -W2 / -w / -W3` on a program
// that redefines a constant (an rb_warn) and prints $VERBOSE and $-W.
//
// The redefinition warning is the discriminator, so each row asserts BOTH the
// value a program reads and whether the warning was emitted — a row that only
// read $VERBOSE would pass even if nothing were gated on it.
//
// A/B: make the `level == 1` arm write object.NilVal() — it compiles, and the
// level-1 row fails on both the value and the warning.
func TestSetWarningLevelIsProcWOption(t *testing.T) {
	const src = "X = 1\nX = 2\np [$VERBOSE, $-W]\n"
	for _, c := range []struct {
		name      string
		level     int
		wantState string
		wantWarn  bool
	}{
		{"-W0 is nil and silences rb_warn", 0, "[nil, 0]\n", false},
		{"-W1 is false and lets rb_warn speak", 1, "[false, 1]\n", true},
		{"-W2 is true", 2, "[true, 2]\n", true},
		{"-W3 saturates at true", 3, "[true, 2]\n", true},
		// scan_oct cannot produce a negative level; treating one as the silent end
		// of the scale is the only defined answer, and it is asserted rather than
		// left to whichever arm happens to catch it.
		{"a negative level is the silent end", -1, "[nil, 0]\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			level := c.level
			got := runHost(t, src, func(vm *VM) { vm.SetWarningLevel(level) })
			warned := bytes.Contains([]byte(got), []byte("already initialized constant X"))
			if warned != c.wantWarn {
				t.Errorf("warned = %v, want %v (output %q)", warned, c.wantWarn, got)
			}
			// The state line is the last line; the warnings (when any) precede it.
			lines := bytes.Split([]byte(got), []byte("\n"))
			last := string(lines[len(lines)-2]) + "\n"
			if last != c.wantState {
				t.Errorf("[$VERBOSE, $-W] = %q, want %q", last, c.wantState)
			}
		})
	}
}

// TestSetWarningLevelLeavesOneRepresentation guards the defect globals.go warns
// about: $VERBOSE must have exactly one representation. SetWarningLevel writes the
// slot, so what a Ruby program reads and what warnEnabled/warningEnabled read have
// to agree for all three states.
func TestSetWarningLevelLeavesOneRepresentation(t *testing.T) {
	for _, c := range []struct {
		level               int
		wantWarn, wantWarn2 bool
	}{
		{0, false, false},
		{1, true, false},
		{2, true, true},
	} {
		vm := New(&bytes.Buffer{})
		vm.SetWarningLevel(c.level)
		if got := vm.warnEnabled(); got != c.wantWarn {
			t.Errorf("level %d: warnEnabled = %v, want %v", c.level, got, c.wantWarn)
		}
		if got := vm.warningEnabled(); got != c.wantWarn2 {
			t.Errorf("level %d: warningEnabled = %v, want %v", c.level, got, c.wantWarn2)
		}
	}
}
