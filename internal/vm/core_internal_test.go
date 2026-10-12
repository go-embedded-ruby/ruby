// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoreFeaturesAreProvided holds coreFeatures to require.go: every name it
// keeps must be one require answers without the file system, or the core build
// would "keep" a feature nothing provides.
func TestCoreFeaturesAreProvided(t *testing.T) {
	if len(coreFeatures) == 0 {
		t.Fatal("coreFeatures is empty")
	}
	for f := range coreFeatures {
		if !providedFeatures[f] && !preloadedFeatures[f] {
			t.Errorf("core feature %q is not a provided feature", f)
		}
	}
	// The gems the playground must never pull in are out, by name.
	for _, f := range []string{"rails", "nokogiri", "yaml", "erb", "uri", "openssl", "sass", "images", "optparse"} {
		if coreFeatures[f] {
			t.Errorf("%q is listed as core", f)
		}
	}
}

// coreRequire requires name on vm and returns the LoadError message, or "" when
// the require succeeded.
func coreRequire(t *testing.T, vm *VM, name string) (msg string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(RubyError)
			if !ok {
				panic(r)
			}
			if e.Class != "LoadError" {
				t.Fatalf("require %q raised %s, want LoadError", name, e.Class)
			}
			msg = e.Message
		}
	}()
	vm.doRequire(name, false)
	return ""
}

// TestHookOutOfCore runs the core build's require hook on a default VM: every
// provided feature outside coreFeatures raises the core LoadError (and again on
// a retry), while every core feature still loads.
func TestHookOutOfCore(t *testing.T) {
	vm := New(&bytes.Buffer{})
	vm.featureHooks = nil // the hook must cope with a VM that has none yet
	vm.hookOutOfCore()
	for f := range providedFeatures {
		got := coreRequire(t, vm, f)
		if coreFeatures[f] {
			if got != "" {
				t.Errorf("core feature %q: %s", f, got)
			}
			continue
		}
		want := "cannot load such file -- " + f + " (not available in the rbgo_core build)"
		if got != want {
			t.Errorf("require %q = %q, want %q", f, got, want)
		}
		if again := coreRequire(t, vm, f); again != want {
			t.Errorf("a retried require %q = %q, want the same LoadError", f, again)
		}
	}
}

// TestHookOutOfCoreNeverReadsTheLoadPath: an excluded gem's require raises
// before require.go searches $LOAD_PATH, so a rails.rb sitting there is not run
// in place of the gem the build left out.
func TestHookOutOfCoreNeverReadsTheLoadPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rails.rb"), []byte("$hijacked = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	vm := New(&out)
	vm.hookOutOfCore()
	if err := vm.loadSource("$LOAD_PATH.unshift " + rubyQuote(dir) + `
begin
  require "rails"
rescue LoadError => e
  puts e.message
end
p $hijacked
`); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "cannot load such file -- rails (not available in the rbgo_core build)\nnil\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestRaiseBuildUnavailableFormatsTheNameVerbatim: the feature name is an
// argument, never the format, so a '%' in it is printed as written.
func TestRaiseBuildUnavailableFormatsTheNameVerbatim(t *testing.T) {
	vm := New(&bytes.Buffer{})
	got := rubyErr(t, func() { vm.raiseCoreUnavailable("x%sy%d") })
	if !strings.HasPrefix(got.Message, "cannot load such file -- x%sy%d (") {
		t.Errorf("message = %q", got.Message)
	}
}

// CoreBuild exposes coreBuild to the external vm_test package (see
// core_external_test.go); it exists only in the test binary.
var CoreBuild = coreBuild

// skipOnCore skips a test that measures something the rbgo_core build leaves
// out on purpose. The default build's lanes still run it.
func skipOnCore(t *testing.T, why string) {
	t.Helper()
	if coreBuild {
		t.Skip("not on the rbgo_core build: " + why)
	}
}

// rubyQuote returns s as a double-quoted Ruby string literal.
func rubyQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `#`, `\#`).Replace(s) + `"`
}
