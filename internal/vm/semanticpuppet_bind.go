// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	semver "github.com/go-ruby-semantic-puppet/semantic-puppet"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// semverStrArg reads the single string/symbol argument common to Version.parse /
// VersionRange.parse, raising ArgumentError when it is missing.
func semverStrArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return args[0].ToS()
}

// registerSemVerClass installs SemanticPuppet::Version.
func (vm *VM) registerSemVerClass(cls *RClass) {
	// Version.parse(str) / Version.new(str) — parse a SemVer string, raising
	// SemanticPuppet::Version::ValidationFailure on a malformed version.
	parse := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		v, err := semver.Parse(semverStrArg(args))
		if err != nil {
			raise("SemanticPuppet::Version::ValidationFailure", "%s", err.Error())
		}
		return &SemVerObj{v: v}
	}
	cls.smethods["parse"] = &Method{name: "parse", owner: cls, native: parse}
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: parse}

	// Version.valid?(str) — true when the string parses.
	cls.smethods["valid?"] = &Method{name: "valid?", owner: cls, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(semver.IsValid(semverStrArg(args)))
	}}

	self := func(v object.Value) *semver.Version { return v.(*SemVerObj).v }

	cls.define("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).String())
	})
	cls.define("major", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Major()))
	})
	cls.define("minor", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Minor()))
	})
	cls.define("patch", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Patch()))
	})
	// prerelease / build return nil (not "") when absent, matching the gem.
	cls.define("prerelease", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return emptyToNil(self(v).Prerelease())
	})
	cls.define("build", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return emptyToNil(self(v).Build())
	})
	cls.define("stable?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Stable())
	})

	// next(part) — the version with :major, :minor or :patch bumped.
	cls.define("next", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		switch nameArg(args[0]) {
		case "major":
			return &SemVerObj{v: self(v).NextMajor()}
		case "minor":
			return &SemVerObj{v: self(v).NextMinor()}
		case "patch":
			return &SemVerObj{v: self(v).NextPatch()}
		default:
			raise("ArgumentError", "unknown version part %s", args[0].Inspect())
			return object.NilV
		}
	})

	// <=> returns nil against a non-Version; ==/eql? are false against one.
	cls.define("<=>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if c, ok := semverCompare(v, args); ok {
			return object.IntValue(int64(c))
		}
		return object.NilV
	})
	eq := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := semverArg(args)
		return object.Bool(ok && self(v).Equal(o))
	}
	cls.define("==", eq)
	cls.define("eql?", eq)

	// Ordering operators raise ArgumentError against a non-Version, matching
	// Comparable.
	rel := func(pred func(int) bool) NativeFn {
		return func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			c, ok := semverCompare(v, args)
			if !ok {
				raise("ArgumentError", "comparison of SemanticPuppet::Version failed")
			}
			return object.Bool(pred(c))
		}
	}
	cls.define("<", rel(func(c int) bool { return c < 0 }))
	cls.define("<=", rel(func(c int) bool { return c <= 0 }))
	cls.define(">", rel(func(c int) bool { return c > 0 }))
	cls.define(">=", rel(func(c int) bool { return c >= 0 }))
}

// registerSemVerRangeClass installs SemanticPuppet::VersionRange.
func (vm *VM) registerSemVerRangeClass(cls *RClass) {
	parse := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		r, err := semver.ParseRange(semverStrArg(args))
		if err != nil {
			raise("SemanticPuppet::VersionRange::InvalidRangeFormat", "%s", err.Error())
		}
		return &SemVerRange{r: r}
	}
	cls.smethods["parse"] = &Method{name: "parse", owner: cls, native: parse}
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: parse}

	self := func(v object.Value) *semver.VersionRange { return v.(*SemVerRange).r }

	cls.define("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).String())
	})

	// include?(v) / cover?(v) / === accept a Version or a version string.
	inc := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Include(rangeMemberArg(args)))
	}
	cls.define("include?", inc)
	cls.define("member?", inc)
	cls.define("===", inc)
	cls.define("cover?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Cover(rangeMemberArg(args)))
	})

	// intersection(other) — the overlapping range; an empty range when the two
	// are disjoint (matching the gem, which returns an empty range rather than
	// nil).
	cls.define("intersection", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := rangeArg(args)
		if !ok {
			raise("ArgumentError", "intersection requires a VersionRange")
		}
		return &SemVerRange{r: self(v).Intersection(o)}
	})

	cls.define("min", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return semverOrNil(self(v).Min())
	})
	cls.define("max", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return semverOrNil(self(v).Max())
	})
}

// semverArg extracts the wrapped *Version from the first argument, reporting
// false when it is not a SemanticPuppet::Version.
func semverArg(args []object.Value) (*semver.Version, bool) {
	if len(args) == 0 {
		return nil, false
	}
	o, ok := args[0].(*SemVerObj)
	if !ok {
		return nil, false
	}
	return o.v, true
}

// semverCompare compares the receiver against the first argument, reporting
// false when the argument is not a Version.
func semverCompare(v object.Value, args []object.Value) (int, bool) {
	o, ok := semverArg(args)
	if !ok {
		return 0, false
	}
	return v.(*SemVerObj).v.Compare(o), true
}

// rangeArg extracts the wrapped *VersionRange from the first argument.
func rangeArg(args []object.Value) (*semver.VersionRange, bool) {
	if len(args) == 0 {
		return nil, false
	}
	o, ok := args[0].(*SemVerRange)
	if !ok {
		return nil, false
	}
	return o.r, true
}

// rangeMemberArg reads the version tested for membership: a Version wrapper is
// used directly, any other value is parsed from its string form (an unparseable
// string raises InvalidRangeFormat's version sibling).
func rangeMemberArg(args []object.Value) *semver.Version {
	if v, ok := semverArg(args); ok {
		return v
	}
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	v, err := semver.Parse(args[0].ToS())
	if err != nil {
		raise("SemanticPuppet::Version::ValidationFailure", "%s", err.Error())
	}
	return v
}

// semverOrNil wraps a possibly-nil *Version as a Ruby value.
func semverOrNil(v *semver.Version) object.Value {
	if v == nil {
		return object.NilV
	}
	return &SemVerObj{v: v}
}

// emptyToNil maps an empty identifier string to Ruby nil, any other to a String.
func emptyToNil(s string) object.Value {
	if s == "" {
		return object.NilV
	}
	return object.NewString(s)
}
