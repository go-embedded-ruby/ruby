// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	semver "github.com/go-ruby-semantic-puppet/semantic-puppet"
)

// This file installs the SemanticPuppet module and its Ruby-facing API
// (require "semantic_puppet"). The SemVer 2.0.0 value types — Version parsing
// and precedence, and the node-semver range grammar with membership and
// intersection — live in github.com/go-ruby-semantic-puppet/semantic-puppet
// (a pure-Go port of the semantic_puppet gem). rbgo owns only the object-model
// bridge: the Ruby SemanticPuppet::Version / SemanticPuppet::VersionRange
// classes and the Ruby⇄Go value conversion. The types are immutable, so the
// binding is stateless (no per-VM field).

// SemVerObj is a Ruby SemanticPuppet::Version instance — a handle over one
// immutable *semver.Version.
type SemVerObj struct{ v *semver.Version }

func (o *SemVerObj) ToS() string     { return o.v.String() }
func (o *SemVerObj) Inspect() string { return "#<SemanticPuppet::Version " + o.v.String() + ">" }
func (o *SemVerObj) Truthy() bool    { return true }

// SemVerRange is a Ruby SemanticPuppet::VersionRange instance — a handle over
// one immutable *semver.VersionRange.
type SemVerRange struct{ r *semver.VersionRange }

func (o *SemVerRange) ToS() string     { return o.r.String() }
func (o *SemVerRange) Inspect() string { return "#<SemanticPuppet::VersionRange " + o.r.String() + ">" }
func (o *SemVerRange) Truthy() bool    { return true }

// registerSemanticPuppet installs the SemanticPuppet module (require
// "semantic_puppet"): Version.parse/new build and compare SemVer versions, and
// VersionRange.parse tests membership and intersects ranges.
func (vm *VM) registerSemanticPuppet() {
	mod := newClass("SemanticPuppet", nil)
	mod.isModule = true
	vm.consts["SemanticPuppet"] = mod

	// The gem's ArgumentError-family failures: a bad version string raises
	// SemanticPuppet::Version::ValidationFailure, a bad range string raises
	// SemanticPuppet::VersionRange::InvalidRangeFormat. Both descend from
	// ArgumentError so a plain `rescue ArgumentError` catches them.
	argErr := vm.consts["ArgumentError"].(*RClass)

	verCls := newClass("SemanticPuppet::Version", vm.cObject)
	mod.consts["Version"] = verCls
	vm.consts["SemanticPuppet::Version"] = verCls
	valFail := newClass("SemanticPuppet::Version::ValidationFailure", argErr)
	verCls.consts["ValidationFailure"] = valFail
	vm.consts["SemanticPuppet::Version::ValidationFailure"] = valFail

	rangeCls := newClass("SemanticPuppet::VersionRange", vm.cObject)
	mod.consts["VersionRange"] = rangeCls
	vm.consts["SemanticPuppet::VersionRange"] = rangeCls
	badRange := newClass("SemanticPuppet::VersionRange::InvalidRangeFormat", argErr)
	rangeCls.consts["InvalidRangeFormat"] = badRange
	vm.consts["SemanticPuppet::VersionRange::InvalidRangeFormat"] = badRange

	vm.registerSemVerClass(verCls)
	vm.registerSemVerRangeClass(rangeCls)
}
