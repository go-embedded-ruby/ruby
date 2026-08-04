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
