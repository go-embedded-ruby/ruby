// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// warnProbeFile is the path runWarn stamps onto its ISeq. It has to be stamped:
// a warning's position comes from the frame's ISeq, and so does the location
// recordConstLoc stores for the "previous definition" line. A path-less ISeq —
// which is what compiling a bare source string gives — records no location at
// all, so the second line is correctly suppressed and the case cannot witness
// it. Stamping a path is what `rbgo script.rb` and `rbgo -e` both do.
const warnProbeFile = "wt45probe.rb"

// runWarn compiles and runs src in one VM and returns everything it wrote,
// newlines and all — warnings are the SUBJECT here, so nothing is trimmed and
// nothing is filtered.
func runWarn(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	setISeqFile(iseq, warnProbeFile)
	var buf bytes.Buffer
	vm := New(&buf)
	vm.SetScriptPath(warnProbeFile)
	if _, err := vm.Run(iseq); err != nil {
		t.Fatalf("run: %v", err)
	}
	return buf.String()
}

// $VERBOSE has three states and the two warning families split on them. This is
// a WITNESS for the defect in #641: the UNSET rows. A fresh VM assigns $VERBOSE
// nothing, MRI reads false there, and rb_warn must therefore speak. The gate used
// to read vm.globals["$VERBOSE"] straight out of the map, where an unset slot is a
// Go nil that object.IsNil accepts, so the unset rows were silent while the
// $VERBOSE = false rows were not — one value with two representations, and the
// gate read the one nobody writes.
//
// Verified against MRI 4.0.5: `ruby -e 'X=1; X=2'` warns, `ruby -W0 -e …` does
// not, and `ruby -e 'p [1,2].inject(0, :+) { }'` warns only under -w.
func TestVerboseGateThreeStates(t *testing.T) {
	cases := []struct {
		name        string
		prologue    string
		wantWarn    bool // rb_warn family (already initialized constant)
		wantWarning bool // rb_warning family (Array.new with a block and no args)
	}{
		{"unset — MRI's default, reads false", "", true, false},
		{"$VERBOSE = false", "$VERBOSE = false\n", true, false},
		{"$VERBOSE = nil (-W0)", "$VERBOSE = nil\n", false, false},
		{"$VERBOSE = true (-w)", "$VERBOSE = true\n", true, true},
		{"$VERBOSE = 1 coerces to true", "$VERBOSE = 1\n", true, true},
	}
	for _, c := range cases {
		gotWarn := strings.Contains(runWarn(t, c.prologue+"X = 1\nX = 2\n"), "already initialized constant X")
		if gotWarn != c.wantWarn {
			t.Errorf("%s: rb_warn fired=%v, want %v", c.name, gotWarn, c.wantWarn)
		}
		gotWarning := strings.Contains(runWarn(t, c.prologue+"Array.new { :blk }\n"), "given block not used")
		if gotWarning != c.wantWarning {
			t.Errorf("%s: rb_warning fired=%v, want %v", c.name, gotWarning, c.wantWarning)
		}
	}
}

// A redefined constant emits TWO lines, each with its own source position, and
// the second one names the constant WITHOUT its module qualification. All four
// expectations are MRI 4.0.5's, taken from `ruby -e` runs:
//
//	-e:1: warning: already initialized constant MM::K
//	-e:1: warning: previous definition of K was here
//
// WITNESS for all three parts of #699 at once: the prefix, the second line, and
// the gate that let neither be seen. Against the old code every row fails.
func TestConstantRedefinitionEmitsBothLinesWithPositions(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"top level", "X = 1\nX = 2\n",
			"wt45probe.rb:2: warning: already initialized constant X\n" +
				"wt45probe.rb:1: warning: previous definition of X was here\n"},
		{"qualified by the module, second line bare", "module MM\n  K = 1\n  K = 2\nend\n",
			"wt45probe.rb:3: warning: already initialized constant MM::K\n" +
				"wt45probe.rb:2: warning: previous definition of K was here\n"},
		{"scoped assignment", "module MM\nend\nMM::K = 1\nMM::K = 2\n",
			"wt45probe.rb:4: warning: already initialized constant MM::K\n" +
				"wt45probe.rb:3: warning: previous definition of K was here\n"},
		{"Module#const_set takes the same path", "Object.const_set(:Q, 1)\nObject.const_set(:Q, 2)\n",
			"wt45probe.rb:2: warning: already initialized constant Q\n" +
				"wt45probe.rb:1: warning: previous definition of Q was here\n"},
		{"$VERBOSE = nil silences both", "$VERBOSE = nil\nX = 1\nX = 2\n", ""},
	}
	for _, c := range cases {
		if got := runWarn(t, c.src); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// The second line's position is the OLD entry's, so a constant first defined in
// one place and overwritten in another produces two DIFFERENT positions. The
// in-process harness compiles one ISeq, so the two lines share a path here and
// the LINES are what differ; the cross-file case is the same code with a
// different recorded file, which constLocs already stores per constant.
//
// This also pins that the location is read BEFORE the store: recordConstLoc
// stamps the new site during the assignment, so reading it afterwards would
// report line 2 twice.
func TestPreviousDefinitionPointsAtTheOriginalSite(t *testing.T) {
	got := runWarn(t, "Z = 1\n\n\n\nZ = 2\n")
	want := "wt45probe.rb:5: warning: already initialized constant Z\n" +
		"wt45probe.rb:1: warning: previous definition of Z was here\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A constant this VM defined natively has no recorded location, and MRI's
// `if (!NIL_P(ce->file) && ce->line)` means the first line goes out ALONE. Object
// is full of such constants; ARGV is one.
func TestNativeConstantRedefinitionHasNoSecondLine(t *testing.T) {
	got := runWarn(t, "ARGV = []\n")
	if !strings.Contains(got, "already initialized constant ARGV") {
		t.Fatalf("got %q, want the already-initialized line", got)
	}
	if strings.Contains(got, "previous definition") {
		t.Fatalf("got %q, want NO second line for a natively defined constant", got)
	}
}

// warningStringAt is err_vcatf's shape (error.c:125). The single caller today —
// warnAlreadyInitialized — filters both degenerate shapes out at the call site,
// exactly as MRI's `if (!NIL_P(ce->file) && ce->line)` does, so these two rows are
// NOT reachable through it. This is a GUARD, not a witness: it pins the shape
// err_vcatf gives the next caller that does not filter, and it is why the branches
// exist at all rather than being asserted away.
func TestWarningStringAtShapes(t *testing.T) {
	cases := []struct {
		file string
		line int
		want string
	}{
		{"a.rb", 7, "a.rb:7: warning: m\n"},
		{"a.rb", 0, "a.rb: warning: m\n"}, // err_vcatf: `if (line)` guards the ":%d"
		{"", 7, "warning: m\n"},           // err_vcatf: `if (file)` guards the whole location
	}
	for _, c := range cases {
		if got := warningStringAt(c.file, c.line, "m"); got != c.want {
			t.Errorf("warningStringAt(%q, %d): got %q, want %q", c.file, c.line, got, c.want)
		}
	}
}

// Struct.new(name, …) is the ONE place MRI words this differently: struct.c:273
// warns "redefining constant Struct::Foo", with no companion line, because
// new_struct removes the old constant itself before defining the new one.
// Measured against MRI 4.0.5:
//
//	$ ruby -e 'Struct.new("Dup1", :a); Struct.new("Dup1", :b)'
//	-e:1: warning: redefining constant Struct::Dup1
//
// WITNESS: the old code wrote "already initialized constant Struct::Dup1" through
// Kernel#warn, with no prefix and no $VERBOSE gate, and a test asserted that text
// — pinning our own divergence.
func TestStructNamedRedefineUsesStructsOwnWording(t *testing.T) {
	got := runWarn(t, "Struct.new(\"Dup1\", :a)\nStruct.new(\"Dup1\", :b)\n")
	want := "wt45probe.rb:2: warning: redefining constant Struct::Dup1\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if out := runWarn(t, "$VERBOSE = nil\nStruct.new(\"Dup2\", :a)\nStruct.new(\"Dup2\", :b)\n"); out != "" {
		t.Fatalf("$VERBOSE = nil must silence it too: got %q", out)
	}
}

// "given block not used" reaches every method MRI warns from, and only those.
// enum.c and array.c use rb_warn for the value/pattern forms of count, index,
// find_index, any?, all?, none? and one?, and rb_warning for Array#initialize
// with no arguments and for inject's two-argument arm. Every row was measured on
// MRI 4.0.5.
//
// WITNESS for the helper inconsistency: before this wave six of these were silent
// at the default verbosity because rbgo did not emit them at all, and the one
// helper that required $VERBOSE == true was chosen per call site rather than per
// MRI function.
func TestGivenBlockNotUsedMatchesMRIPerMethod(t *testing.T) {
	const enumClass = "class WCE\n  include Enumerable\n  def each; yield 1; yield 2; end\nend\n"
	cases := []struct {
		name      string
		src       string
		wantWarn  bool // at the DEFAULT verbosity
		wantUnder bool // under $VERBOSE = true
	}{
		{"Array#count(item) with a block", "[1, 2].count(1) { |x| x }\n", true, true},
		{"Array#count with a block alone", "[1, 2].count { |x| x }\n", false, false},
		{"Array#any?(pattern) with a block", "[1, 2].any?(Integer) { |x| x }\n", true, true},
		{"Array#all?(pattern) with a block", "[1, 2].all?(Integer) { |x| x }\n", true, true},
		{"Array#none?(pattern) with a block", "[1, 2].none?(Integer) { |x| x }\n", true, true},
		{"Array#one?(pattern) with a block", "[1, 2].one?(Integer) { |x| x }\n", true, true},
		{"Array#any? with a block alone", "[1, 2].any? { |x| x }\n", false, false},
		{"Array#index(value) with a block", "[1].index(1) { }\n", true, true},
		{"Array#rindex(value) with a block", "[1].rindex(1) { }\n", true, true},
		{"Array#find_index(value) with a block", "[1].find_index(1) { }\n", true, true},
		{"Hash#any?(pattern) with a block", "{ a: 1 }.any?(Array) { |x| x }\n", true, true},
		{"Enumerable#count(item) with a block", enumClass + "WCE.new.count(1) { |x| x }\n", true, true},
		{"Enumerable#find_index(value) with a block", enumClass + "WCE.new.find_index(1) { |x| x }\n", true, true},
		{"Enumerable#one?(pattern) with a block", enumClass + "WCE.new.one?(Integer) { |x| x }\n", true, true},
		// rb_warning, not rb_warn: the default verbosity stays quiet.
		{"Array.new with no arguments and a block", "Array.new { :blk }\n", false, true},
		{"Enumerable#inject(init, op) with a block", enumClass + "WCE.new.inject(0, :+) { |a, b| a }\n", false, true},
		{"Enumerable#inject(init) with a block is the ordinary form", enumClass + "WCE.new.inject(0) { |a, b| a + b }\n", false, false},
	}
	for _, c := range cases {
		got := strings.Contains(runWarn(t, c.src), "given block not used")
		if got != c.wantWarn {
			t.Errorf("%s at the default $VERBOSE: warned=%v, want %v", c.name, got, c.wantWarn)
		}
		gotV := strings.Contains(runWarn(t, "$VERBOSE = true\n"+c.src), "given block not used")
		if gotV != c.wantUnder {
			t.Errorf("%s under $VERBOSE = true: warned=%v, want %v", c.name, gotV, c.wantUnder)
		}
	}
}

// Every one of these lines carries the position of the frame that CALLED the
// warning method, not of the prelude or of the native helper — error.c composes
// it from rb_source_location, which is the current Ruby frame.
func TestGivenBlockNotUsedCarriesTheCallersPosition(t *testing.T) {
	got := runWarn(t, "\n\n[1, 2].count(1) { |x| x }\n")
	want := "wt45probe.rb:3: warning: given block not used\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// error.c's rb_write_warning_str is `rb_funcallv(Warning, :warn, str)`, so an
// overridden Warning.warn sees every internal warning. #699's own eight-line probe
// counted 3 on MRI 4.0.5 and 0 on rbgo; all three arrive here now — and none of them
// reaches the real stderr. Measured byte-for-byte against MRI 4.0.5 on the same
// file: `ruby /tmp/wt45probe.rb` and `rbgo /tmp/wt45probe.rb` now print the same
// four lines.
func TestOverriddenWarningWarnInterceptsInternalWarnings(t *testing.T) {
	got := runWarn(t, `$seen = []
module Warning
  def self.warn(msg, category: nil); $seen << msg; end
end
[1, 2].fetch(5, 1) { |i| i }
BAR = 1
BAR = 2
print "intercepted=", $seen.length, "\n"
print $seen.join("")
`)
	const want = `intercepted=3
wt45probe.rb:5: warning: block supersedes default value argument
wt45probe.rb:7: warning: already initialized constant BAR
wt45probe.rb:6: warning: previous definition of BAR was here
`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// warnEnabled / warningEnabled are the only two gates, so they are also the only
// two places where an ASSIGNED $VERBOSE and an unset one can disagree. They may
// not: a fresh VM must answer exactly as one that was handed MRI's default.
func TestWarnGatesAgreeBetweenUnsetAndExplicitFalse(t *testing.T) {
	var buf bytes.Buffer
	unset := New(&buf)
	explicit := New(&buf)
	explicit.globals["$VERBOSE"] = object.False
	if unset.warnEnabled() != explicit.warnEnabled() {
		t.Errorf("warnEnabled: unset=%v explicit false=%v — one value, two answers",
			unset.warnEnabled(), explicit.warnEnabled())
	}
	if unset.warningEnabled() != explicit.warningEnabled() {
		t.Errorf("warningEnabled: unset=%v explicit false=%v",
			unset.warningEnabled(), explicit.warningEnabled())
	}
	if !unset.warnEnabled() {
		t.Error("an unset $VERBOSE must enable rb_warn — MRI starts it at false")
	}
	if unset.warningEnabled() {
		t.Error("an unset $VERBOSE must NOT enable rb_warning — that needs true")
	}
}
