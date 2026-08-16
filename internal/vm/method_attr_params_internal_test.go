// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestMethodParametersAttr covers #parameters for the accessors attr_reader /
// attr_writer / attr_accessor synthesise: a reader takes no arguments ([]) and a
// writer one required (anonymous) value ([[:req]]), rather than the native
// method default [[:rest]]. Ordinary methods are unchanged. Verified against
// ruby 4.0.6 (and the core/method/parameters spec).
func TestMethodParametersAttr(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class AR; attr_reader :r; end; p AR.instance_method(:r).parameters`, "[]"},
		{`class AW; attr_writer :w; end; p AW.instance_method(:w=).parameters`, "[[:req]]"},
		{`class AA; attr_accessor :a; end; p AA.instance_method(:a).parameters`, "[]"},
		{`class AB; attr_accessor :a; end; p AB.instance_method(:a=).parameters`, "[[:req]]"},
		// The bound Method form shares the same accessor shape.
		{`class AC; attr_reader :r; end; p AC.new.method(:r).parameters`, "[]"},
		{`class AD; attr_writer :w; end; p AD.new.method(:w=).parameters`, "[[:req]]"},
		// An ordinary method still reports its real signature (the default path).
		{`def om(x, y = 1, *z); end; p method(:om).parameters`, "[[:req, :x], [:opt, :y], [:rest, :z]]"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
