// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A guard used as an EXPRESSION must yield what mspec yields (#962).
//
// mspec's SpecGuard#run_if is `yield if yield?(false)` (guard.rb:80), so with a
// block the guard's value is the block's value when it holds and nil when it
// does not. The shim's `_guard_run` ended in a hardcoded nil, so the open case
// returned nothing as well — and core/file/stat/birthtime_spec.rb:3-5 splats
// exactly that value into a platform list:
//
//	platform_is(:windows, :darwin, :freebsd, :netbsd,
//	            *ruby_version_is("4.0") { :linux },
//	           ) do
//
// ⛔ TWO SIDES, because one side alone is a weaker rule that also passes. A
// helper that returned the block's value UNCONDITIONALLY would turn
// birthtime_spec green too — and would add :linux on a Ruby where the corpus
// says not to. So every open row below has a shut twin.
//
// Every row is platform-independent: versions "0.0" and "999" are open and shut
// on any interpreter, and RUBY_PLATFORM matches itself on every machine.
const guardValueProbe = `require_relative '../spec_helper'
def say(label, v); puts "VALUE #{label}=#{v.inspect}"; end

say "version_open",  ruby_version_is("0.0") { :yes }
say "version_shut",  ruby_version_is("999") { :yes }
say "platform_open", platform_is(RUBY_PLATFORM) { :yes }
say "platform_shut", platform_is(:definitely_not_a_platform) { :yes }
say "not_open",      platform_is_not(:definitely_not_a_platform) { :yes }
say "not_shut",      platform_is_not(RUBY_PLATFORM) { :yes }

# The corpus's own shape, both ways.
say "splat_open",    [:windows, *ruby_version_is("0.0") { :linux }]
say "splat_shut",    [:windows, *ruby_version_is("999") { :linux }]

# The no-block form, fixed earlier, must not have moved.
say "bare_open",     ruby_version_is("0.0")
say "bare_shut",     ruby_version_is("999")

puts "DONE"

describe "a guard-value probe" do
  it "runs at all" do
    1.should == 1
  end
end
`

func TestAGuardUsedAsAnExpressionYieldsWhatMspecYields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the rbgo build in -short")
	}
	bin := buildRbgo(t)

	root := t.TempDir()
	shim, err := os.ReadFile(shimRelPath)
	if err != nil {
		t.Fatalf("read the shim under test: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "spec_helper.rb"), shim, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "probe_spec.rb"), []byte(guardValueProbe), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runProbe(t, bin, root, "")

	// The CONTROL: the probe reached its last line. A probe that died early
	// leaves the map empty, and an empty map is missing every key below —
	// which this test reports as "never answered", not as a wrong value.
	if !strings.Contains(out, "DONE") {
		t.Fatalf("the probe never reached its last line, so this run proves nothing:\n%s", out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "VALUE ")
		if !ok {
			continue
		}
		if label, value, ok := strings.Cut(rest, "="); ok {
			got[label] = value
		}
	}

	for _, tc := range []struct{ label, want string }{
		{"version_open", ":yes"},
		{"version_shut", "nil"},
		{"platform_open", ":yes"},
		{"platform_shut", "nil"},
		{"not_open", ":yes"},
		{"not_shut", "nil"},
		{"splat_open", "[:windows, :linux]"},
		{"splat_shut", "[:windows]"},
		{"bare_open", "true"},
		{"bare_shut", "false"},
	} {
		v, ok := got[tc.label]
		if !ok {
			t.Errorf("the probe never answered %q:\n%s", tc.label, out)
			continue
		}
		if v != tc.want {
			t.Errorf("%s = %s, want %s (mspec guard.rb:80 is `yield if yield?(false)`)", tc.label, v, tc.want)
		}
	}
}
