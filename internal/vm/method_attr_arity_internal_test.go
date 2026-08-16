// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestMethodArityAttr covers #arity for the accessors attr_reader / attr_writer
// / attr_accessor synthesise: a reader takes no arguments (arity 0) and a writer
// exactly one (arity 1), rather than the native method default -1. Ordinary
// methods keep their real arity. Verified against ruby 4.0.6.
func TestMethodArityAttr(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class AR; attr_reader :r; end; p AR.instance_method(:r).arity`, "0"},
		{`class AW; attr_writer :w; end; p AW.instance_method(:w=).arity`, "1"},
		{`class AA; attr_accessor :a; end; p AA.instance_method(:a).arity`, "0"},
		{`class AB; attr_accessor :a; end; p AB.instance_method(:a=).arity`, "1"},
		// The bound Method form shares the same accessor shape.
		{`class AC; attr_reader :r; end; p AC.new.method(:r).arity`, "0"},
		{`class AD; attr_writer :w; end; p AD.new.method(:w=).arity`, "1"},
		// Ordinary methods still report their real arity (the default path).
		{`def om0; end; p method(:om0).arity`, "0"},
		{`def om1(x); end; p method(:om1).arity`, "1"},
		{`def omo(x, y = 1); end; p method(:omo).arity`, "-2"},
		{`def oms(*z); end; p method(:oms).arity`, "-1"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
