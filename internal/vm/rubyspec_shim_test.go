// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests exercise the MSpec-compatible shim that scores rbgo against
// ruby/spec (scripts/conformance/rubyspec/spec_helper.rb). They load the REAL
// shim source and run mini-specs through it, asserting the shim reproduces the
// mspec/MRI harness behaviour the ratchet relies on. Each test guards a shim fix
// that was reclaiming previously-false-failed ruby/spec examples, so a shim
// regression turns these red before it can silently drop the conformance floor.

// shimSource reads the actual ratchet shim (relative to internal/vm).
func shimSource(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "scripts", "conformance", "rubyspec", "spec_helper.rb")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read shim: %v", err)
	}
	return string(b)
}

// runShim evals prelude + the real shim + body and returns captured stdout,
// including the shim's at_exit "RBGO_RESULT pass=.. fail=.. error=.. skip=.." line.
func runShim(t *testing.T, prelude, body string) string {
	t.Helper()
	return eval(t, prelude+"\n"+shimSource(t)+"\n"+body)
}

var rbgoResultRe = regexp.MustCompile(`RBGO_RESULT pass=(\d+) fail=(\d+) error=(\d+) skip=(\d+)`)

// assertShimResult checks the shim's tally line matches the wanted counts and
// surfaces any RBGO_DETAIL lines (fail/error diagnostics) on mismatch.
func assertShimResult(t *testing.T, out, want string) {
	t.Helper()
	m := rbgoResultRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no RBGO_RESULT in output:\n%s", out)
	}
	got := "pass=" + m[1] + " fail=" + m[2] + " error=" + m[3] + " skip=" + m[4]
	if got != want {
		var detail []string
		for _, ln := range strings.Split(out, "\n") {
			if strings.HasPrefix(ln, "RBGO_DETAIL") {
				detail = append(detail, ln)
			}
		}
		t.Fatalf("shim result = %q, want %q\n%s", got, want, strings.Join(detail, "\n"))
	}
}

// TestShimFixturePathResolution guards defect #1: IOSpecs.io_fixture resolves a
// fixture through fixture(__FILE__, name) where __FILE__ already lives in a
// .../fixtures directory. The shim must resolve relative to the spec file's dir
// WITHOUT doubling the "fixtures" segment (real mspec:
// mspec/lib/mspec/helpers/fixture.rb), so fixture-backed gets/each_line/readline
// instance examples can actually open their file.
func TestShimFixturePathResolution(t *testing.T) {
	root := t.TempDir()
	fixDir := filepath.Join(root, "core", "io", "fixtures")
	if err := os.MkdirAll(filepath.Join(root, "core", "io", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixDir, "lines.txt"), []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// classesPath mimics core/io/fixtures/classes.rb (the __FILE__ io_fixture
	// passes); specPath mimics core/io/gets_spec.rb; sharedPath mimics a shared/
	// helper. All three must resolve to the SAME fixtures/lines.txt.
	classesPath := filepath.Join(fixDir, "classes.rb")
	specPath := filepath.Join(root, "core", "io", "gets_spec.rb")
	sharedPath := filepath.Join(root, "core", "io", "shared", "read.rb")
	want := filepath.ToSlash(filepath.Join(fixDir, "lines.txt"))

	prelude := "CLASSES = " + rubyString(classesPath) + "\n" +
		"SPECF = " + rubyString(specPath) + "\n" +
		"SHAREDF = " + rubyString(sharedPath) + "\n"

	body := `
puts "FROM_FIXTURES=" + fixture(CLASSES, "lines.txt")
puts "FROM_SPEC=" + fixture(SPECF, "lines.txt")
puts "FROM_SHARED=" + fixture(SHAREDF, "lines.txt")
describe "io_fixture-style resolution" do
  it "reads the fixture opened relative to a classes.rb inside fixtures/" do
    File.read(fixture(CLASSES, "lines.txt")).should == "line1\nline2\n"
  end
  it "reads the fixture opened relative to a spec file's dir" do
    File.read(fixture(SPECF, "lines.txt")).should == "line1\nline2\n"
  end
end`

	out := runShim(t, prelude, body)
	for _, marker := range []string{"FROM_FIXTURES=", "FROM_SPEC=", "FROM_SHARED="} {
		var got string
		for _, ln := range strings.Split(out, "\n") {
			if strings.HasPrefix(ln, marker) {
				got = strings.TrimPrefix(ln, marker)
			}
		}
		if got != want {
			t.Errorf("%s resolved to %q, want %q (no doubled fixtures/ segment)", marker, got, want)
		}
		if strings.Contains(got, "fixtures/fixtures") {
			t.Errorf("%s doubled the fixtures segment: %q", marker, got)
		}
	}
	assertShimResult(t, out, "pass=2 fail=0 error=0 skip=0")
}

// TestShimMockCallCounts guards defect #2: mock call-count matchers must be
// honored exactly as mspec's MockProxy/Mock.verify_count do — symbol counts
// (:once/:twice) map to integers, exactly/at_least/at_most are all enforced, a
// bare should_receive defaults to exactly-once, and and_return with several
// values both sequences the returns (last one sticks) and bumps the expected
// count. Previously exactly(:twice)/at_least(:twice) raised (symbol vs Integer)
// and at_most was never checked, so those examples false-failed or false-passed.
func TestShimMockCallCounts(t *testing.T) {
	// Positive cases: each example is satisfied, so all six pass.
	pass := `
describe "mock counts (satisfied)" do
  it "exactly(:twice) honored at two calls" do
    o = mock("m"); o.should_receive(:f).exactly(:twice); o.f; o.f
  end
  it "at_least(:twice) honored at three calls" do
    o = mock("m"); o.should_receive(:f).at_least(:twice); o.f; o.f; o.f
  end
  it "at_most(1) honored at one call" do
    o = mock("m"); o.should_receive(:f).at_most(1); o.f
  end
  it "bare should_receive defaults to exactly-once" do
    o = mock("m"); o.should_receive(:f); o.f
  end
  it "and_return sequences values and bumps the count" do
    o = mock("m"); o.should_receive(:f).and_return(10, 20)
    [o.f, o.f].should == [10, 20]
  end
  it "and_return sequence keeps the last value once exhausted" do
    o = mock("m"); o.should_receive(:f).at_least(1).and_return(1, 2)
    [o.f, o.f, o.f].should == [1, 2, 2]
  end
end`
	assertShimResult(t, runShim(t, "", pass), "pass=6 fail=0 error=0 skip=0")

	// Negative cases: each violates its count, so the shim must FAIL (not error,
	// not pass) each — proving the matchers are actually enforced.
	fail := `
describe "mock counts (violated)" do
  it "exactly(:twice) fails at one call" do
    o = mock("m"); o.should_receive(:f).exactly(:twice); o.f
  end
  it "at_most(1) fails at two calls" do
    o = mock("m"); o.should_receive(:f).at_most(1); o.f; o.f
  end
  it "default exactly-once fails at two calls" do
    o = mock("m"); o.should_receive(:f); o.f; o.f
  end
end`
	assertShimResult(t, runShim(t, "", fail), "pass=0 fail=3 error=0 skip=0")
}

// TestShimMockInterceptsRealMethods guards defect #3: should_receive on a method
// the receiver really defines must intercept it (mspec's Mock.install_method
// installs a singleton method, saving the original) and restore the original at
// example teardown (Mock.cleanup). Previously a mock object's real #to_s/#inspect
// shadowed the expectation (method_missing never fired), so those examples ran
// the real method instead of the stub. The bookkeeping must also never dispatch a
// method on the receiver under test (e.g. #object_id), which a spec may forbid.
func TestShimMockInterceptsRealMethods(t *testing.T) {
	src := `
class Widget; def to_s; "real-widget"; end; end
W = Widget.new                 # to_s is an inherited/class method
SINGLE = Object.new
def SINGLE.greet; "real-greet"; end   # greet is a pre-existing singleton method

describe "mock intercepts + restores real methods" do
  it "intercepts a mock object's own #to_s" do
    m = mock("m"); m.should_receive(:to_s).and_return("stubbed")
    m.to_s.should == "stubbed"
  end
  it "an unmocked mock object still has its real #to_s" do
    mock("m").to_s.should == "#<mock m>"
  end
  it "intercepts an inherited/class method" do
    W.should_receive(:to_s).and_return("mockw"); W.to_s.should == "mockw"
  end
  it "restores the inherited method after the mocking example" do
    W.to_s.should == "real-widget"
  end
  it "intercepts a pre-existing singleton method" do
    SINGLE.should_receive(:greet).and_return("mockg"); SINGLE.greet.should == "mockg"
  end
  it "restores the singleton method after the mocking example" do
    SINGLE.greet.should == "real-greet"
  end
  it "does not dispatch a forbidden bookkeeping method on the receiver" do
    o = mock("o")
    o.should_not_receive(:object_id)
    o.should_receive(:f).and_return(1)
    o.f.should == 1
  end
end`
	assertShimResult(t, runShim(t, "", src), "pass=7 fail=0 error=0 skip=0")
}

// TestShimTimeToleranceConstant guards defect #4: the shim must define the
// top-level (Object) constant TIME_TOLERANCE, faithfully to real mspec
// (mspec/lib/mspec/matchers/be_close.rb: `TIME_TOLERANCE = 20.0`). ruby/spec's
// concurrency shared specs (shared/queue/deque.rb, shared/sizedqueue/enque.rb)
// pass it as a blocking-call timeout INSIDE a spawned thread
// (q.pop(timeout: TIME_TOLERANCE)); when the constant is undefined that thread
// dies with NameError and the sibling `Thread.pass until t.status == "sleep"`
// loop spins forever, hanging the whole file into a timeout/FILEFAIL that scores
// zero. Defining the constant lets those files run to completion (core/queue/pop
// and core/sizedqueue/pop each went 0 -> 20 passing). The value must match mspec
// exactly — a shim that diverges to pass is worse than the hang.
func TestShimTimeToleranceConstant(t *testing.T) {
	// The constant resolves as a top-level Object constant and equals mspec's 20.0.
	if out := runShim(t, "", `puts "TT=" + TIME_TOLERANCE.inspect`); !strings.Contains(out, "TT=20.0") {
		t.Fatalf("TIME_TOLERANCE did not resolve to 20.0:\n%s", out)
	}

	// Referencing TIME_TOLERANCE from a spec example (the way the concurrency
	// shared specs do) must NOT raise NameError; the example scores as a pass.
	src := `
describe "TIME_TOLERANCE (mspec constant)" do
  it "is defined and equals mspec's 20.0" do
    TIME_TOLERANCE.should == 20.0
  end
  it "is resolvable as a plain timeout argument" do
    (0.0 < TIME_TOLERANCE).should == true
  end
end`
	assertShimResult(t, runShim(t, "", src), "pass=2 fail=0 error=0 skip=0")
}

// ⛔ CUR_VERSION selects the POPULATION the whole conformance figure is counted
// over, and it used to be a second hardcoded copy of what
// kernel_introspect.go's `rubyVersion` already says (#799). They agreed, so
// nothing was wrong on any given day; the defect was that half a version change
// could land — the runtime bumped, the shim forgotten — and the gate would go on
// measuring the old population while every page claimed the new version. Both
// halves look right on their own, which is why nothing would have said so.
//
// This is the test that makes that state impossible to commit. It asserts the
// equality through the REAL shim rather than by matching the file's text,
// because the thing that matters is not how the line is spelled but what the
// constant holds at run time.
func TestShimVersionIsTheRuntimesNotASecondCopy(t *testing.T) {
	src := `
describe "CUR_VERSION" do
  it "is the version this interpreter reports, not a second copy of it" do
    CUR_VERSION.should == RUBY_VERSION
  end
end`
	assertShimResult(t, runShim(t, "", src), "pass=1 fail=0 error=0 skip=0")
}

// And the constant has to actually GOVERN the guard: an equality that nothing
// reads would satisfy the test above while the guards resolved against
// something else entirely. mspec's chain is
// guards/version.rb -> SpecGuard.ruby_version(:full) -> RUBY_VERSION
// (guard.rb:41-52, at mspec b79c65e); here it is CUR_VERSION, and this proves
// _version_in_range is fed by it.
//
// A guard AT the running version must keep its examples; one above it must
// remove them — note "remove", not "skip": a block guard below the threshold
// deletes the examples rather than counting them as skipped, so the failing
// example inside the second block contributes nothing to any counter. If it
// ever did run, `1.should == 2` makes that visible as fail=1 instead of a
// silently absent example.
//
// ⛔ WHAT THIS ONE CANNOT SEE, measured by planting it: with the shim set back
// to a literal `CUR_VERSION = "9.9.9"`, this test still PASSES — a guard at
// `>= RUBY_VERSION` is satisfied by any higher value and `"99.0"` still removes
// its block. Only the test above catches that, and it did, naming the
// disagreement (`expected "9.9.9" to == "3.4.1"`). So this test proves the
// WIRING and the one above proves the VALUE; neither substitutes for the other,
// and reading this one alone as the guard against a hardcode would be wrong.
func TestShimVersionGuardsResolveAtThatVersion(t *testing.T) {
	src := `
describe "version guards" do
  ruby_version_is RUBY_VERSION do
    it "keeps an example guarded at exactly the running version" do
      1.should == 1
    end
  end

  ruby_version_is "99.0" do
    it "removes an example guarded above it" do
      1.should == 2
    end
  end
end`
	assertShimResult(t, runShim(t, "", src), "pass=1 fail=0 error=0 skip=0")
}
