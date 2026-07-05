// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	bundler "github.com/go-ruby-bundler/bundler"
	"github.com/go-ruby-rubygems/rubygems"
)

// TestBundlerWrapperDisplay exercises the ToS/Inspect/Truthy of every wrapper
// type directly: #inspect is reached through Ruby's `p`, but #to_s (via `puts`)
// and truthiness are covered here so every arm is proven.
func TestBundlerWrapperDisplay(t *testing.T) {
	spec := &bundler.Spec{Name: "rake", Version: rubygems.MustVersion("13.4.2")}
	dep := &bundler.Dependency{Name: "rspec"}
	wrappers := []struct {
		v interface {
			ToS() string
			Inspect() string
			Truthy() bool
		}
		toS string
	}{
		{&BundlerLockfile{&bundler.Lockfile{}}, "#<Bundler::LockfileParser>"},
		{&BundlerSpec{spec}, "#<Bundler::LazySpecification rake-13.4.2>"},
		{&BundlerDependency{dep}, "#<Bundler::Dependency rspec>"},
		{&BundlerGemfile{&bundler.Gemfile{}}, "#<Bundler::Dsl>"},
		{&BundlerIndex{m: bundler.MapIndex{}}, "#<Bundler::Index>"},
	}
	for _, w := range wrappers {
		if got := w.v.ToS(); got != w.toS {
			t.Errorf("ToS = %q, want %q", got, w.toS)
		}
		if got := w.v.Inspect(); got != w.toS {
			t.Errorf("Inspect = %q, want %q", got, w.toS)
		}
		if !w.v.Truthy() {
			t.Errorf("Truthy = false, want true for %q", w.toS)
		}
	}
}

// TestBundlerFullNamePlatform covers the platform-suffixed full name (a spec on a
// non-default platform), which a "ruby"-only lockfile does not reach.
func TestBundlerFullNamePlatform(t *testing.T) {
	s := &bundler.Spec{Name: "nokogiri", Version: rubygems.MustVersion("1.16.0"), Platform: "arm64-darwin"}
	if got, want := bundlerFullName(s), "nokogiri-1.16.0-arm64-darwin"; got != want {
		t.Errorf("bundlerFullName = %q, want %q", got, want)
	}
}

// TestBundlerPlatform covers both arms of the platform mapping: the default ""
// becomes "ruby", and an explicit platform passes through.
func TestBundlerPlatform(t *testing.T) {
	if got := bundlerPlatform("").ToS(); got != "ruby" {
		t.Errorf("bundlerPlatform(\"\") = %q, want %q", got, "ruby")
	}
	if got := bundlerPlatform("java").ToS(); got != "java" {
		t.Errorf("bundlerPlatform(\"java\") = %q, want %q", got, "java")
	}
}

// TestBundlerReqString covers the nil-requirement arm (a bare dependency edge),
// which maps to the RubyGems default ">= 0".
func TestBundlerReqString(t *testing.T) {
	if got := bundlerReqString(nil); got != ">= 0" {
		t.Errorf("bundlerReqString(nil) = %q, want %q", got, ">= 0")
	}
	if got := bundlerReqString(rubygems.MustRequirement("~> 2.0")); got != "~> 2.0" {
		t.Errorf("bundlerReqString(~> 2.0) = %q, want %q", got, "~> 2.0")
	}
}
