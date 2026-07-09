// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	semver "github.com/go-ruby-semantic-puppet/semantic-puppet"
)

// semverRun runs a Ruby program with `require "semantic_puppet"` prepended.
func semverRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"semantic_puppet\"\n"+body)
}

// TestSemVerParseAndParts covers Version.parse/new/valid? and the component
// accessors, including the nil returns for an absent prerelease / build.
func TestSemVerParseAndParts(t *testing.T) {
	got := semverRun(t, `
v = SemanticPuppet::Version.parse("1.2.3-rc1+build5")
puts v.class
puts v.to_s
puts v.major
puts v.minor
puts v.patch
puts v.prerelease
puts v.build
puts v.stable?
puts SemanticPuppet::Version.new("2.0.0").stable?
puts SemanticPuppet::Version.new("2.0.0").prerelease.inspect
puts SemanticPuppet::Version.new("2.0.0").build.inspect
puts SemanticPuppet::Version.valid?("1.0.0")
puts SemanticPuppet::Version.valid?("nope")
`)
	want := "SemanticPuppet::Version\n1.2.3-rc1+build5\n1\n2\n3\nrc1\nbuild5\nfalse\ntrue\nnil\nnil\ntrue\nfalse"
	if got != want {
		t.Fatalf("parse/parts:\n got=%q\nwant=%q", got, want)
	}
}

// TestSemVerNext covers Version#next for each part and the bad-part ArgumentError.
func TestSemVerNext(t *testing.T) {
	got := semverRun(t, `
v = SemanticPuppet::Version.parse("1.2.3")
puts v.next(:major)
puts v.next(:minor)
puts v.next(:patch)
begin; v.next(:nope); rescue => e; puts e.class; end
begin; v.next; rescue => e; puts e.class; end
`)
	want := "2.0.0\n1.3.0\n1.2.4\nArgumentError\nArgumentError"
	if got != want {
		t.Fatalf("next:\n got=%q\nwant=%q", got, want)
	}
}

// TestSemVerCompare covers <=>, ==/eql? and the ordering operators, plus the
// non-Version cases (nil <=>, false ==, ArgumentError <).
func TestSemVerCompare(t *testing.T) {
	got := semverRun(t, `
a = SemanticPuppet::Version.parse("1.0.0")
b = SemanticPuppet::Version.parse("2.0.0")
puts(a <=> b)
puts(b <=> a)
puts(a <=> a)
puts(a == SemanticPuppet::Version.parse("1.0.0"))
puts(a.eql?(b))
puts(a < b)
puts(a <= a)
puts(b > a)
puts(b >= b)
puts((a <=> "x").inspect)
puts(a == "x")
begin; a < "x"; rescue => e; puts e.class; end
`)
	want := "-1\n1\n0\ntrue\nfalse\ntrue\ntrue\ntrue\ntrue\nnil\nfalse\nArgumentError"
	if got != want {
		t.Fatalf("compare:\n got=%q\nwant=%q", got, want)
	}
}

// TestSemVerValidationFailure covers the ValidationFailure raised on a malformed
// version (both via .parse and the missing-argument ArgumentError).
func TestSemVerValidationFailure(t *testing.T) {
	got := semverRun(t, `
begin
  SemanticPuppet::Version.parse("not.a.version")
rescue SemanticPuppet::Version::ValidationFailure => e
  puts "caught"
end
puts SemanticPuppet::Version::ValidationFailure.ancestors.include?(ArgumentError)
begin; SemanticPuppet::Version.parse; rescue => e; puts e.class; end
`)
	want := "caught\ntrue\nArgumentError"
	if got != want {
		t.Fatalf("validation failure:\n got=%q\nwant=%q", got, want)
	}
}

// TestSemVerRange covers VersionRange.parse/new/to_s, membership (include?/
// member?/===/cover?) with both a Version and a version string, intersection
// (overlap, disjoint empty range, non-range ArgumentError), min/max on a single
// clause and the nil min/max of a disjoint union.
func TestSemVerRange(t *testing.T) {
	got := semverRun(t, `
r = SemanticPuppet::VersionRange.parse(">=1.0.0 <2.0.0")
puts r.class
puts r.to_s.empty?
v = SemanticPuppet::Version.parse("1.5.0")
puts r.include?(v)
puts r.member?(v)
puts(r === v)
puts r.cover?(SemanticPuppet::Version.parse("3.0.0"))
puts r.include?("1.2.0")
puts r.include?("2.5.0")
r2 = SemanticPuppet::VersionRange.new(">=1.5.0 <3.0.0")
puts r.intersection(r2).include?(v)
puts r.intersection(SemanticPuppet::VersionRange.parse(">=5.0.0")).include?(v)
begin; r.intersection("x"); rescue => e; puts e.class; end
puts r.min.class
puts r.max.class
u = SemanticPuppet::VersionRange.parse(">=1.0.0 <2.0.0 || >=3.0.0")
puts u.min.inspect
puts u.max.inspect
`)
	want := "SemanticPuppet::VersionRange\nfalse\ntrue\ntrue\ntrue\nfalse\ntrue\nfalse\n" +
		"true\nfalse\nArgumentError\nSemanticPuppet::Version\nSemanticPuppet::Version\nnil\nnil"
	if got != want {
		t.Fatalf("range:\n got=%q\nwant=%q", got, want)
	}
}

// TestSemVerRangeErrors covers the InvalidRangeFormat on a bad range, the
// missing-argument ArgumentError, and rangeMemberArg's bad-string and
// missing-argument paths.
func TestSemVerRangeErrors(t *testing.T) {
	got := semverRun(t, `
begin
  SemanticPuppet::VersionRange.parse(">=>=bad")
rescue SemanticPuppet::VersionRange::InvalidRangeFormat
  puts "bad range"
end
puts SemanticPuppet::VersionRange::InvalidRangeFormat.ancestors.include?(ArgumentError)
begin; SemanticPuppet::VersionRange.parse; rescue => e; puts e.class; end
r = SemanticPuppet::VersionRange.parse(">=1.0.0")
begin; r.include?("not.a.version"); rescue => e; puts e.class; end
begin; r.include?; rescue => e; puts e.class; end
begin; r.intersection; rescue => e; puts e.class; end
`)
	want := "bad range\ntrue\nArgumentError\nSemanticPuppet::Version::ValidationFailure\nArgumentError\nArgumentError"
	if got != want {
		t.Fatalf("range errors:\n got=%q\nwant=%q", got, want)
	}
}

// TestSemVerStringers covers the object.Value marker methods on both wrappers.
func TestSemVerStringers(t *testing.T) {
	v := &SemVerObj{v: semver.MustParse("1.2.3")}
	if v.ToS() != "1.2.3" || v.Inspect() != "#<SemanticPuppet::Version 1.2.3>" || !v.Truthy() {
		t.Errorf("version stringers = %q / %q / %v", v.ToS(), v.Inspect(), v.Truthy())
	}
	r := &SemVerRange{r: semver.MustParseRange(">=1.0.0")}
	if r.ToS() == "" || r.Inspect() == "" || !r.Truthy() {
		t.Errorf("range stringers = %q / %q / %v", r.ToS(), r.Inspect(), r.Truthy())
	}
}
