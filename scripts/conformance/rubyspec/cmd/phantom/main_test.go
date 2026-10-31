// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The parser's own control. It also runs inside the binary on every invocation —
// see checkVocabulary — because this tool is used as a binary from CI, which
// never runs this file. Having it in both places is deliberate: here it fails a
// change at review time, there it protects a consumer.
func TestVocabulary(t *testing.T) {
	if err := checkVocabulary(); err != nil {
		t.Fatal(err)
	}
}

// writeSpec puts a spec file in a fake corpus and returns the root.
func writeSpec(t *testing.T, tree, name, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, tree)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Both trees are walked; the other must exist or the walk errors.
	other := "core"
	if tree == "core" {
		other = "language"
	}
	if err := os.MkdirAll(filepath.Join(root, other), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// ⛔ The shape this tool exists for, verbatim from core/array/rfind_spec.rb: the
// expectation means "42 is not callable", and on a build without Array#rfind it
// is satisfied by rfind's own NoMethodError instead.
func TestCollectAttributesToTheDescribe(t *testing.T) {
	root := writeSpec(t, "core", "rfind_spec.rb", `require_relative '../../spec_helper'

describe "Array#rfind" do
  it "returns the last element" do
    [1, 2, 3].rfind { |x| x == 2 }.should == 2
  end

  it "raises a NoMethodError if the ifnone argument does not respond to #call" do
    -> { [1, 2, 3].rfind(42) { |x| false } }.should.raise(NoMethodError)
  end
end
`)
	sites, files, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Errorf("read %d spec file(s), want 1", files)
	}
	if len(sites) != 1 {
		t.Fatalf("found %d site(s), want 1: %+v", len(sites), sites)
	}
	if got := sites[0].name(); got != "Array#rfind" {
		t.Errorf("attributed to %q, want %q", got, "Array#rfind")
	}
	if sites[0].line != 9 {
		t.Errorf("line %d, want 9", sites[0].line)
	}
}

// A nested describe that names a method takes over from its parent, and an
// expectation under a describe that names NO method is skipped rather than
// guessed at — attributing it to an enclosing describe would put a claim in the
// census that the file does not make.
func TestCollectSkipsWhatItCannotAttribute(t *testing.T) {
	root := writeSpec(t, "language", "defined_spec.rb", `
describe "RUBY_VERSION" do
  it "raises a NoMethodError for nonsense" do
    -> { RUBY_VERSION.nope }.should.raise(NoMethodError)
  end
end

describe "String#squeeze" do
  it "raises a NoMethodError" do
    -> { "x".squeeze(42) }.should.raise(NoMethodError)
  end
end

describe :shared_thing, shared: true do
  it "raises a NoMethodError" do
    -> { nil.nope }.should.raise(NoMethodError)
  end
end
`)
	sites, _, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	// The first describe names a constant, not a method; the third is a shared
	// block. Only String#squeeze is attributable — but note the shared block
	// follows it, so a parser that simply never cleared `cur` would attribute
	// the third expectation to String#squeeze and report two.
	if len(sites) != 1 {
		t.Fatalf("found %d site(s), want 1: %+v", len(sites), sites)
	}
	if got := sites[0].name(); got != "String#squeeze" {
		t.Errorf("attributed to %q, want %q", got, "String#squeeze")
	}
}

// The other half of the attribution rule: a describe nested INSIDE a method's
// own is a sub-grouping of the same method, and attribution must survive it.
// Clearing on every unattributable describe would be safe but would lose these,
// which is how ruby/spec is usually written.
func TestCollectAttributionSurvivesANestedGrouping(t *testing.T) {
	root := writeSpec(t, "core", "nested_spec.rb", `
describe "Array#rfind" do
  describe "with a block" do
    it "raises a NoMethodError" do
      -> { [1].rfind(42) { false } }.should.raise(NoMethodError)
    end
  end

  describe "without a block" do
    it "raises a NoMethodError too" do
      -> { [1].rfind(42) }.should.raise(NoMethodError)
    end
  end
end

describe "Array#first" do
  it "raises a NoMethodError" do
    -> { [1].first(42) }.should.raise(NoMethodError)
  end
end
`)
	sites, _, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 3 {
		t.Fatalf("found %d site(s), want 3: %+v", len(sites), sites)
	}
	want := []string{"Array#rfind", "Array#rfind", "Array#first"}
	for i, w := range want {
		if got := sites[i].name(); got != w {
			t.Errorf("site %d attributed to %q, want %q", i, got, w)
		}
	}
}

// ⛔ THE FALSE POSITIVES THE FIRST RUN PRODUCED, verbatim from
// core/complex/negative_spec.rb. MRI does not define Complex#negative? — Complex
// has no ordering — and the spec asserts that absence on purpose, so rbgo
// passing it is conformance. An example that asserts the absence is asserting
// exactly what it got, and reporting it would send a reader to fix something
// that is right.
func TestCollectWithdrawsAnExampleThatAssertsTheAbsence(t *testing.T) {
	root := writeSpec(t, "core", "negative_spec.rb", `require_relative '../../spec_helper'

describe "Complex#negative?" do
  it "is undefined" do
    c = Complex(1)

    c.methods.should_not.include?(:negative?)

    -> {
      c.negative?
    }.should.raise(NoMethodError)
  end
end
`)
	sites, _, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 0 {
		t.Errorf("found %d site(s), want 0 — the example asserts the absence: %+v", len(sites), sites)
	}
}

// And the withdrawal must be narrow: one describe asserting an absence must not
// silence a sibling that does not. Without the flush-per-block, a single
// `should_not.include?` anywhere in a file would hide every candidate in it.
func TestTheAbsenceWithdrawalIsPerDescribeBlock(t *testing.T) {
	root := writeSpec(t, "core", "mixed_spec.rb", `
describe "Complex#negative?" do
  it "is undefined" do
    Complex(1).methods.should_not.include?(:negative?)
    -> { Complex(1).negative? }.should.raise(NoMethodError)
  end
end

describe "Array#rfind" do
  it "raises a NoMethodError if the ifnone argument does not respond to #call" do
    -> { [1, 2, 3].rfind(42) { |x| false } }.should.raise(NoMethodError)
  end
end
`)
	sites, _, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 {
		t.Fatalf("found %d site(s), want 1: %+v", len(sites), sites)
	}
	if got := sites[0].name(); got != "Array#rfind" {
		t.Errorf("kept %q, want the sibling that asserts no absence (%q)", got, "Array#rfind")
	}
}

// NameError is a different claim and must not be collected: a spec asserting
// NameError is usually about a constant, and treating it as a method-absence
// candidate would pad the census.
func TestCollectIgnoresOtherExceptionClasses(t *testing.T) {
	root := writeSpec(t, "core", "other_spec.rb", `
describe "Array#first" do
  it "raises a NameError" do
    -> { NoSuchConstant }.should.raise(NameError)
  end

  it "raises an ArgumentError" do
    -> { [1].first(1, 2) }.should.raise(ArgumentError)
  end
end
`)
	sites, _, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 0 {
		t.Errorf("found %d site(s), want 0: %+v", len(sites), sites)
	}
}

// RSpec's spelling as well as mspec's, since the corpus carries both.
func TestCollectReadsBothSpellings(t *testing.T) {
	root := writeSpec(t, "core", "both_spec.rb", `
describe "Hash#dig" do
  it "one" do
    -> { {}.dig(42) }.should.raise(NoMethodError)
  end

  it "two" do
    expect { {}.dig(42) }.should raise_error(NoMethodError)
  end
end
`)
	sites, _, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 {
		t.Fatalf("found %d site(s), want 2: %+v", len(sites), sites)
	}
}
