// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	bundler "github.com/go-ruby-bundler/bundler"
	"github.com/go-ruby-rubygems/rubygems"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-bundler/bundler library. The
// library owns the pure-compute core of Bundler — the Gemfile.lock codec
// (byte-for-byte invertible), the canonical Gemfile DSL reader, and the
// backtracking dependency resolver over an injected index — building on
// go-ruby-rubygems for the Gem::Version / Gem::Requirement algebra. rbgo wraps
// each library value as a Ruby object reporting the matching Bundler::* class
// (see bundler.go for the class + method registration) and converts values
// across the boundary here. The network fetch of the gem index and the install
// filesystem writes are host-side seams the library deliberately excludes; the
// resolver is driven from Ruby through an in-memory Bundler::Index fixture.

// The wrapper types. Each holds a pointer into the library and reports the
// matching Bundler::* class (see classOf); the methods registered in bundler.go
// read the held value.

// BundlerLockfile wraps a parsed *bundler.Lockfile (Bundler::LockfileParser): its
// source sections' specs, platforms, dependencies, ruby version and bundler
// version, plus the byte-exact re-emission via #to_lock.
type BundlerLockfile struct{ l *bundler.Lockfile }

// BundlerSpec wraps a resolved *bundler.Spec (Bundler::LazySpecification): a
// name, version, platform and the spec's own runtime dependencies.
type BundlerSpec struct{ s *bundler.Spec }

// BundlerDependency wraps a *bundler.Dependency (Bundler::Dependency): a gem name
// plus a requirement, the bundler groups and platforms it applies to. It backs
// both the top-level Gemfile/lockfile dependencies and — reconstructed from a
// SpecDependency — a spec's own runtime dependency edges.
type BundlerDependency struct{ d *bundler.Dependency }

// BundlerGemfile wraps a parsed *bundler.Gemfile (Bundler::Dsl): the sources,
// ruby version and declared dependencies read from the canonical DSL forms.
type BundlerGemfile struct{ gf *bundler.Gemfile }

// BundlerIndex wraps a mutable bundler.MapIndex (Bundler::Index): the in-memory
// resolution index the host fills with gem versions and feeds to Bundler.resolve
// (the network-backed index is a host seam).
type BundlerIndex struct{ m bundler.MapIndex }

func (v *BundlerLockfile) ToS() string     { return "#<Bundler::LockfileParser>" }
func (v *BundlerLockfile) Inspect() string { return "#<Bundler::LockfileParser>" }
func (v *BundlerLockfile) Truthy() bool    { return true }
func (v *BundlerSpec) ToS() string {
	return "#<Bundler::LazySpecification " + bundlerFullName(v.s) + ">"
}
func (v *BundlerSpec) Inspect() string   { return v.ToS() }
func (v *BundlerSpec) Truthy() bool      { return true }
func (v *BundlerDependency) ToS() string { return "#<Bundler::Dependency " + v.d.Name + ">" }
func (v *BundlerDependency) Inspect() string {
	return v.ToS()
}
func (v *BundlerDependency) Truthy() bool { return true }
func (v *BundlerGemfile) ToS() string     { return "#<Bundler::Dsl>" }
func (v *BundlerGemfile) Inspect() string { return "#<Bundler::Dsl>" }
func (v *BundlerGemfile) Truthy() bool    { return true }
func (v *BundlerIndex) ToS() string       { return "#<Bundler::Index>" }
func (v *BundlerIndex) Inspect() string   { return "#<Bundler::Index>" }
func (v *BundlerIndex) Truthy() bool      { return true }

// bundlerFullName is a spec's "name-version" identifier (Gem::Specification's
// full_name), with the platform appended when it is not the default "ruby".
func bundlerFullName(s *bundler.Spec) string {
	full := s.Name + "-" + s.Version.String()
	if s.Platform != "" {
		full += "-" + s.Platform
	}
	return full
}

// bundlerPlatform reports a spec's platform, mapping the library's "" (the
// default Gem::Platform::RUBY) to the Ruby string "ruby".
func bundlerPlatform(p string) object.Value {
	if p == "" {
		return object.NewString("ruby")
	}
	return object.NewString(p)
}

// bundlerReqString renders a requirement as its Gem::Requirement#to_s form,
// mapping a nil requirement (a bare dependency with no constraint) to the default
// ">= 0".
func bundlerReqString(r *rubygems.Requirement) string {
	if r == nil {
		return ">= 0"
	}
	return r.String()
}

// bundlerStrOrNil wraps a library scalar as a Ruby String, or Ruby nil when the
// field was absent (the empty string), used for the optional ruby/bundler
// version lines.
func bundlerStrOrNil(s string) object.Value {
	if s == "" {
		return object.NilV
	}
	return object.NewString(s)
}

// bundlerStrArray wraps a library []string (e.g. the platforms) as a Ruby Array
// of Strings.
func bundlerStrArray(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(elems)
}

// bundlerSymArray wraps a library []string of bundler groups as a Ruby Array of
// Symbols (:default, :test, …), matching Bundler::Dependency#groups.
func bundlerSymArray(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.Symbol(s)
	}
	return object.NewArrayFromSlice(elems)
}

// bundlerSpecs wraps a []*bundler.Spec as a Ruby Array of
// Bundler::LazySpecification.
func bundlerSpecs(specs []*bundler.Spec) object.Value {
	elems := make([]object.Value, len(specs))
	for i, s := range specs {
		elems[i] = &BundlerSpec{s}
	}
	return object.NewArrayFromSlice(elems)
}

// bundlerDeps wraps a []*bundler.Dependency as a Ruby Array of
// Bundler::Dependency.
func bundlerDeps(deps []*bundler.Dependency) object.Value {
	elems := make([]object.Value, len(deps))
	for i, d := range deps {
		elems[i] = &BundlerDependency{d}
	}
	return object.NewArrayFromSlice(elems)
}

// bundlerSpecDeps wraps a []bundler.SpecDependency (a spec's runtime dependency
// edges) as a Ruby Array of Bundler::Dependency, reconstructing a *Dependency
// from each name+requirement edge so the same accessor surface serves both.
func bundlerSpecDeps(deps []bundler.SpecDependency) object.Value {
	elems := make([]object.Value, len(deps))
	for i, sd := range deps {
		elems[i] = &BundlerDependency{&bundler.Dependency{Name: sd.Name, Requirement: sd.Requirement}}
	}
	return object.NewArrayFromSlice(elems)
}

// bundlerAllSpecs flattens every source section's specs into one slice, the
// combined spec set a Gemfile.lock locks (Bundler::LockfileParser#specs).
func bundlerAllSpecs(l *bundler.Lockfile) []*bundler.Spec {
	var out []*bundler.Spec
	for _, src := range l.Sources {
		out = append(out, src.Specs...)
	}
	return out
}

// bundlerGemSource is the GEM source attached to every spec a Ruby-driven
// resolution produces (the common rubygems.org remote case).
func bundlerGemSource() *bundler.Source {
	return &bundler.Source{Type: bundler.GemSource, Remotes: []string{"https://rubygems.org/"}}
}
