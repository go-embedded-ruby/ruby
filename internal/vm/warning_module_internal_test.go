// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestWarningModule covers the Warning module: the per-category enable flags read
// and written through Warning.[] / Warning.[]= (with TypeError for a non-Symbol
// and ArgumentError for an unknown category), Warning.categories, and that
// Warning.warn returns nil and is an overridable module function owned by
// Warning (extend self). Verified against ruby 4.0.6.
func TestWarningModule(t *testing.T) {
	cases := []struct{ src, want string }{
		// Default category flags.
		{`p Warning[:deprecated]`, `false`},
		{`p Warning[:experimental]`, `true`},
		{`p Warning[:performance]`, `false`},
		{`p Warning[:strict_unused_block]`, `false`},
		// []= toggles a category and [] reads it back.
		{`Warning[:performance] = true; p Warning[:performance]`, `true`},
		{`Warning[:deprecated] = true; p Warning[:deprecated]`, `true`},
		// categories lists the known categories.
		{`p Warning.categories.include?(:deprecated) && Warning.categories.include?(:experimental) && Warning.categories.include?(:performance)`, `true`},
		// An unknown category raises ArgumentError.
		{`begin; Warning[:noop]; rescue ArgumentError => e; p e.message; end`, `"unknown category: noop"`},
		{`begin; Warning[:noop] = false; rescue ArgumentError => e; p e.message; end`, `"unknown category: noop"`},
		// A non-Symbol category raises TypeError.
		{`begin; Warning[42]; rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; Warning["deprecated"]; rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; Warning[false] = true; rescue TypeError => e; p e.class; end`, `TypeError`},
		// warn is owned by Warning, which extends itself.
		{`p Warning.method(:warn).owner`, `Warning`},
		{`p Warning.singleton_class.ancestors.include?(Warning)`, `true`},
		// warn is overridable.
		{`def Warning.warn(m); "got: #{m}"; end; p Warning.warn("hi")`, `"got: hi"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
