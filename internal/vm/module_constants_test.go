// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestModuleConstantsInherit covers Module#constants and the Module.constants
// singleton against MRI's rb_mod_constants / rb_mod_const_of / rb_const_list
// (ruby/ruby v3_4_1 variable.c) and rb_mod_s_constants (eval.c). Every
// expectation was produced by MRI 3.4 on the same source (issue #833: Object
// .constants used to be empty because the inherit walk skipped Object always).
func TestModuleConstantsInherit(t *testing.T) {
	cases := []struct{ src, want string }{
		// Object.constants lists every top-level constant, core and user-defined.
		{`class Zed833; end
c = Object.constants
p [c.size > 50, c.include?(:Zed833), c.include?(:String), c.include?(:Comparable), c.include?(:Math)]`,
			"[true, true, true, true, true]\n"},
		{`p Object.constants == Object.constants(true), Object.constants(false).include?(:Integer)`,
			"true\ntrue\n"},
		// For Object the walk goes on through Kernel and Object's prepends; for any
		// other class it stops before Object (rb_mod_const_of's break).
		{`module Kernel; KC833 = 1; end
module P833; PC833 = 1; end
class Object; prepend P833; end
p [Object.constants.include?(:KC833), String.constants.include?(:KC833)]
p [Object.constants.include?(:PC833), String.constants.include?(:PC833)]`,
			"[true, false]\n[true, false]\n"},
		// A private constant is never listed, inherit or not, at any level.
		{`X833 = 1
class Object; private_constant :X833; end
p [Object.constants.include?(:X833), Object.constants(false).include?(:X833), Module.constants.include?(:X833)]
p Object.const_defined?(:X833)`,
			"[false, false, false]\ntrue\n"},
		{`module M833; A = 1; B = 2; private_constant :B; end
class K833; include M833; C = 3; end
class K2833 < K833; D = 4; end
p K833.constants, K833.constants(false), M833.constants, K2833.constants`,
			"[:C, :A]\n[:C]\n[:A]\n[:D, :C, :A]\n"},
		// The nearest definition decides: a private QA in R hides Q's public QA.
		{`module Q833; QA = 1; end
module R833; QA = 2; private_constant :QA; end
class S833; include Q833; include R833; end
p S833.constants`,
			"[]\n"},
		// A pending autoload is listed before its file loads.
		{`autoload :Lazy833, "nope_not_here_833"
p [Object.constants.include?(:Lazy833), Module.constants.include?(:Lazy833)]`,
			"[true, true]\n"},
		// Module.constants with no argument: at the top level it equals
		// Object.constants; inside a class body it adds the lexical scope.
		{`p Module.constants == Object.constants`, "true\n"},
		{`class Foo833
  INNER = 1
  p [Module.constants.include?(:INNER), Module.constants.include?(:String)]
end`, "[true, true]\n"},
		{`module Outer833; O = 1; module Inner833; I = 1
  p [Module.constants.include?(:O), Module.constants.include?(:I), Module.constants.include?(:Inner833)]
end; end`, "[true, true, true]\n"},
		// An argument, or a Module subclass receiver, means Module#constants.
		{`p [Module.constants(false).include?(:Zed833b), Module.constants(true).include?(:String)]`,
			"[false, false]\n"},
		{`class MyMod833 < Module; MC = 1; end
p MyMod833.constants`, "[:MC]\n"},
		// Ancestors other than Object still count for a plain class, listed table
		// by table from the receiver up (rb_const_list's insertion order).
		{`module Mix833; MIXED = 1; end
class P833b; PARENT = 1; end
class C833 < P833b; include Mix833; OWN = 1; end
p C833.constants`, "[:OWN, :MIXED, :PARENT]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestObjectConstantsScales guards against a quadratic Object.constants: 10 000
// top-level constants are all listed, each exactly once.
func TestObjectConstantsScales(t *testing.T) {
	var b strings.Builder
	for i := range 10000 {
		fmt.Fprintf(&b, "K833_%d = %d\n", i, i)
	}
	b.WriteString(`c = Object.constants
p [c.size == c.uniq.size, c.count { |s| s.to_s.start_with?("K833_") }]`)
	if got, want := eval(t, b.String()), "[true, 10000]\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
