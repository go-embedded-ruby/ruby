// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// facterRun runs a Ruby program with `require "facter"` already prepended,
// driving the whole binding through rbgo exactly as a user would.
func facterRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"facter\"\n"+body)
}

// TestFacterCustomFactFlow drives the headline flow: Facter.add with a setcode
// block, then reading it back through Facter.value, the Facter[] Fact handle
// (#value / #name / #to_s / #class), Facter.fact, Facter.to_hash and Facter.list.
func TestFacterCustomFactFlow(t *testing.T) {
	got := facterRun(t, `
Facter.add(:greeting) { setcode { "hello" } }
puts Facter.value(:greeting)
f = Facter[:greeting]
puts f.value
puts f.name
puts f.to_s
puts f.class
puts Facter.fact(:greeting).value
puts Facter.to_hash["greeting"]
puts Facter.list.include?("greeting")
puts Facter.add(:r2) { setcode { 1 } }.class
`)
	want := "hello\nhello\ngreeting\ngreeting\nFacter::Util::Fact\nhello\nhello\ntrue\nFacter::Util::Fact"
	if got != want {
		t.Fatalf("custom fact flow:\n got=%q\nwant=%q", got, want)
	}
}

// TestFacterValueTypes proves each Ruby value a setcode block can return marshals
// out through Facter.value with its Ruby type intact.
func TestFacterValueTypes(t *testing.T) {
	cases := []struct{ body, want string }{
		{`Facter.add(:n)  { setcode { 42 } };        puts Facter.value(:n)`, "42"},
		{`Facter.add(:fl) { setcode { 3.5 } };       puts Facter.value(:fl)`, "3.5"},
		{`Facter.add(:bl) { setcode { true } };      puts Facter.value(:bl)`, "true"},
		{`Facter.add(:ar) { setcode { ["a","b"] } }; puts Facter.value(:ar).inspect`, `["a", "b"]`},
		{`Facter.add(:hs) { setcode { {"k"=>"v"} } };puts Facter.value(:hs)["k"]`, "v"},
		{`Facter.add(:nl) { setcode { nil } };       puts Facter.value(:nl).inspect`, "nil"},
	}
	for _, c := range cases {
		if got := facterRun(t, c.body); got != c.want {
			t.Fatalf("%s\n got=%q want=%q", c.body, got, c.want)
		}
	}
}

// TestFacterWeightedResolutions proves multiple resolutions for one fact are
// ranked by has_weight (highest matching wins), the go-ruby-facter behaviour the
// direct engine could not express.
func TestFacterWeightedResolutions(t *testing.T) {
	got := facterRun(t, `
Facter.add(:tier) { has_weight 1; setcode { "low" } }
Facter.add(:tier) { has_weight 5; setcode { "high" } }
Facter.add(:tier) { has_weight 3; setcode { "mid" } }
puts Facter.value(:tier)
`)
	if got != "high" {
		t.Fatalf("weighted resolutions: got=%q want=high", got)
	}
}

// TestFacterConfine covers the confine family: a Hash guard, a per-value
// predicate block (confine(:fact) { |v| … }), a bare boolean predicate block,
// and a no-op confine. A matching guard lets the resolution fire; a mismatch or
// a false predicate suppresses it.
func TestFacterConfine(t *testing.T) {
	got := facterRun(t, `
Facter.add(:role) { setcode { "web" } }
Facter.add(:vhost)  { confine :role => "web"; setcode { "example.com" } }
Facter.add(:secret) { confine :role => "db";  setcode { "x" } }
Facter.add(:pred)   { confine(:role) { |v| v == "web" }; setcode { "yes" } }
Facter.add(:predno) { confine(:role) { |v| v == "db" };  setcode { "no" } }
Facter.add(:bare)   { confine { true };  setcode { "on" } }
Facter.add(:bareno) { confine { false }; setcode { "off" } }
Facter.add(:noop)   { confine; setcode { "plain" } }
puts Facter.value(:vhost)
puts Facter.value(:secret).inspect
puts Facter.value(:pred)
puts Facter.value(:predno).inspect
puts Facter.value(:bare)
puts Facter.value(:bareno).inspect
puts Facter.value(:noop)
`)
	want := "example.com\nnil\nyes\nnil\non\nnil\nplain"
	if got != want {
		t.Fatalf("confine:\n got=%q\nwant=%q", got, want)
	}
}

// TestFacterAbsentAndEmpty covers the nil paths: an unknown fact via value / [],
// a resolution whose block never called setcode, and Facter.add with no block.
func TestFacterAbsentAndEmpty(t *testing.T) {
	got := facterRun(t, `
puts Facter.value(:nope).inspect
puts Facter[:nope].inspect
Facter.add(:empty) { has_weight 1 }
puts Facter.value(:empty).inspect
puts Facter[:empty].value.inspect
Facter.add(:nb)
puts Facter.value(:nb).inspect
`)
	want := "nil\nnil\nnil\nnil\nnil"
	if got != want {
		t.Fatalf("absent/empty:\n got=%q\nwant=%q", got, want)
	}
}

// TestFacterClearReset proves both clear and reset drop custom facts.
func TestFacterClearReset(t *testing.T) {
	for _, m := range []string{"clear", "reset"} {
		got := facterRun(t, `
Facter.add(:c) { setcode { "1" } }
Facter.`+m+`
puts Facter.value(:c).inspect
`)
		if got != "nil" {
			t.Fatalf("Facter.%s did not drop custom fact: got=%q", m, got)
		}
	}
}

// TestFacterArgErrors covers the missing-argument ArgumentError raised by
// Facter.value / Facter.add / Facter[].
func TestFacterArgErrors(t *testing.T) {
	for _, expr := range []string{"Facter.value", "Facter.add", "Facter[]"} {
		got := facterRun(t, `begin; `+expr+`; rescue => e; puts e.class; end`)
		if got != "ArgumentError" {
			t.Fatalf("%s expected ArgumentError, got %q", expr, got)
		}
	}
}

// TestFacterRealFacts exercises the default collectors: a real host fact resolves
// non-nil through value and the [] handle, and to_hash returns a Hash. Assertions
// stay host- and arch-independent (kernel resolves on every supported OS).
func TestFacterRealFacts(t *testing.T) {
	got := facterRun(t, `
puts !Facter.value("kernel").nil?
puts !Facter["kernel"].nil?
puts Facter.to_hash.class
`)
	want := "true\ntrue\nHash"
	if got != want {
		t.Fatalf("real facts:\n got=%q\nwant=%q", got, want)
	}
}

// TestFacterSetcodeStringForm covers the Ruby string form `setcode "cmd"`, which
// records the command on the resolution; it is not resolved here (execution
// through the seam is covered by TestFacterSetcodeCommand), so no command runs.
func TestFacterSetcodeStringForm(t *testing.T) {
	got := facterRun(t, `Facter.add(:cmd) { setcode "noop" }; puts Facter[:cmd].class`)
	if got != "Facter::Util::Fact" {
		t.Fatalf("string setcode: got=%q", got)
	}
}

// fakeFacterExec is a deterministic Executor stand-in so the string-form setcode
// (setcode "cmd") is testable without touching real binaries.
type fakeFacterExec struct {
	out string
	ok  bool
}

func (f fakeFacterExec) Execute(string) (string, bool) { return f.out, f.ok }
func (f fakeFacterExec) Which(string) (string, bool)   { return "", false }

// TestFacterSetcodeCommand covers the string-form setcode path — a command run
// through the adapter's execution seam — for both a successful and a failing
// command, using an injected fake executor for determinism.
func TestFacterSetcodeCommand(t *testing.T) {
	var buf bytes.Buffer
	vm := New(&buf)

	vm.facterFacter.SetExecutor(fakeFacterExec{out: "myhost\n", ok: true})
	vm.facterRegister("host", &FacterResolution{vm: vm, command: "hostname", hasCmd: true})
	if got := vm.facterValue("host"); got.ToS() != "myhost" {
		t.Fatalf("command fact: got=%q want=myhost", got.ToS())
	}

	vm.facterFacter.SetExecutor(fakeFacterExec{ok: false})
	vm.facterRegister("down", &FacterResolution{vm: vm, command: "flaky", hasCmd: true})
	if got := vm.facterValue("down"); !object.IsNil(got) {
		t.Fatalf("failing command fact should be nil, got=%q", got.ToS())
	}
}

// TestFacterWrapperStringers covers the object.Value marker methods on the two
// wrapper types.
func TestFacterWrapperStringers(t *testing.T) {
	var buf bytes.Buffer
	vm := New(&buf)
	vm.facterRegister("greeting", &FacterResolution{vm: vm, code: nil})
	f := &FacterFact{vm: vm, f: vm.facterFacter.Fact("greeting")}
	if !strings.HasPrefix(f.ToS(), "#<Facter::Util::Fact") || f.Inspect() != f.ToS() || !f.Truthy() {
		t.Errorf("Fact stringers = %q / %q / %v", f.ToS(), f.Inspect(), f.Truthy())
	}
	r := &FacterResolution{}
	if r.ToS() != "#<Facter::Util::Resolution>" || r.Inspect() != r.ToS() || !r.Truthy() {
		t.Errorf("Resolution stringers = %q / %q / %v", r.ToS(), r.Inspect(), r.Truthy())
	}
}

// TestFacterHelpers covers the two small conversion helpers directly: the
// value-shape conversions a setcode round-trip cannot produce ([]string,
// map[string]string, uint64, int and the fmt.Sprint fallback), and facterIntArg's
// non-integer branch.
func TestFacterHelpers(t *testing.T) {
	if got := facterValueToRuby([]string{"a", "b"}).Inspect(); got != `["a", "b"]` {
		t.Fatalf("[]string: %q", got)
	}
	if got := facterValueToRuby(map[string]string{"k": "v"}).Inspect(); !strings.Contains(got, `"k"`) || !strings.Contains(got, `"v"`) {
		t.Fatalf("map[string]string: %q", got)
	}
	if got := facterValueToRuby(uint64(7)).ToS(); got != "7" {
		t.Fatalf("uint64: %q", got)
	}
	if got := facterValueToRuby(int(5)).ToS(); got != "5" {
		t.Fatalf("int: %q", got)
	}
	if got := facterValueToRuby(int32(9)).ToS(); got != "9" {
		t.Fatalf("fallback: %q", got)
	}
	if facterIntArg(object.NewString("x")) != 0 {
		t.Fatalf("facterIntArg non-integer should be 0")
	}
}
