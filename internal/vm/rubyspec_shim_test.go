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
