// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rubocop "github.com/go-ruby-rubocop/rubocop"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the RuboCop::Cop::Offense / Location value-object surface and
// the argument coercion / severity mapping helpers, over the go-ruby-rubocop
// library. An Offense's cop name / message / location / severity byte-match the
// gem; the Runner's file-walking is the host seam, so this surface takes a source
// String (and an optional path used only in the offense report).

// registerRuboCopOffense installs RuboCop::Cop::Offense and its nested Location
// class, both value objects the Runner#inspect result carries.
func (vm *VM) registerRuboCopOffense(cop *RClass) {
	loc := newClass("RuboCop::Cop::Offense::Location", vm.cObject)
	cls := newClass("RuboCop::Cop::Offense", vm.cObject)
	cop.consts["Offense"] = cls
	vm.consts["RuboCop::Cop::Offense"] = cls
	cls.consts["Location"] = loc
	vm.consts["RuboCop::Cop::Offense::Location"] = loc

	ld := func(name string, fn NativeFn) { loc.define(name, fn) }
	lself := func(v object.Value) rubocop.Location { return v.(*RuboCopLocation).l }
	ld("line", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(lself(v).Line)
	})
	ld("column", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(lself(v).Column)
	})
	ld("length", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(lself(v).Length)
	})

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) rubocop.Offense { return v.(*RuboCopOffense).o }
	d("cop_name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).CopName)
	})
	d("message", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Message)
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).String())
	})
	// #severity is the RuboCop severity name (:convention, :warning, …), matching
	// the gem's RuboCop::Cop::Severity#name.
	d("severity", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(rubocopSeverityName(self(v).Severity))
	})
	// #location returns the offense's Location value object.
	d("location", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RuboCopLocation{l: self(v).Location}
	})
	// #line / #column delegate to the location (the gem's Offense#line/#column).
	d("line", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).Location.Line)
	})
	d("column", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).Location.Column)
	})
	d("correctable?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Correctable)
	})
	d("corrected?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		// This surface never applies corrections (autocorrect returns a new
		// source), so a reported offense is never marked corrected, matching an
		// inspection-only run.
		return object.False
	})
}

// rubocopStr coerces an argument to its String contents: a String yields its
// bytes, any other value its to_s.
func rubocopStr(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// rubocopSourceArgs reads the (source[, path]) arguments of Runner#inspect /
// #autocorrect: the required source String and an optional path String
// (defaulting to "(string)", RuboCop's name for an inspected buffer with no file).
func rubocopSourceArgs(args []object.Value) (src, path string) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	src = rubocopStr(args[0])
	path = "(string)"
	if len(args) > 1 {
		path = rubocopStr(args[1])
	}
	return src, path
}

// rubocopSeverityName maps a rubocop.Severity to the RuboCop severity symbol name
// (:convention / :warning / :error / :fatal / :info / :refactor), the name the
// gem's Severity#name reports.
func rubocopSeverityName(s rubocop.Severity) string {
	switch s {
	case rubocop.Warning:
		return "warning"
	case rubocop.Error:
		return "error"
	case rubocop.Fatal:
		return "fatal"
	case rubocop.Info:
		return "info"
	case rubocop.Refactor:
		return "refactor"
	default:
		return "convention"
	}
}
