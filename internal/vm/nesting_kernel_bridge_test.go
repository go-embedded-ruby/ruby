// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestModuleNesting covers Module.nesting: the lexical module nesting at the
// point of call, innermost first, excluding Object. Asserted against MRI 4.0.5.
func TestModuleNesting(t *testing.T) {
	cases := []struct{ src, want string }{
		// Top level: empty nesting.
		{`p Module.nesting`, "[]\n"},
		// One and two lexical levels.
		{`module A; p Module.nesting; end`, "[A]\n"},
		{`module A; module B; p Module.nesting; end; end`, "[A::B, A]\n"},
		// A nested class adds to the chain.
		{`module A; module B; class C; p Module.nesting; end; end; end`, "[A::B::C, A::B, A]\n"},
		// A singleton class (class << self) appears in the nesting.
		{`module A; class << self; p Module.nesting; end; end`, "[#<Class:A>, A]\n"},
		// Compact form: `module A::B` nests only B itself.
		{`module A; end; module A::B; p Module.nesting; end`, "[A::B]\n"},
		// The nesting is captured at the point of call, not where the method runs:
		// a method defined in a nested module reports that module's nesting.
		{`module A; module B; def self.n; Module.nesting; end; end; end; p A::B.n`, "[A::B, A]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestKernelObjectBridge covers the reflective module operations bridging the
// Kernel module to Object, where rbgo physically keeps Kernel's methods and
// where user code reopens `class Object` / `module Kernel`. Asserted against MRI
// 4.0.5, which resolves the same way (Object includes Kernel).
func TestKernelObjectBridge(t *testing.T) {
	cases := []struct{ src, want string }{
		// alias_method inside `module Kernel` finds a method defined on Object.
		{`class Object; def my_obj_m; :ok; end; end
module Kernel; alias_method :my_kernel_alias, :my_obj_m; end
p my_kernel_alias`, ":ok\n"},
		// Kernel.instance_method resolves a genuine Kernel method (respond_to?).
		{`p Kernel.instance_method(:respond_to?).is_a?(UnboundMethod)`, "true\n"},
		// method_defined? / public_method_defined? see Kernel methods living on Object.
		{`p [Kernel.method_defined?(:respond_to?), Kernel.public_method_defined?(:respond_to?)]`, "[true, true]\n"},
		// Visibility setters inside `module Kernel` accept a method defined on Object.
		{`class Object; def obj_vis_m; end; end
module Kernel; private :obj_vis_m; end
p :ok`, ":ok\n"},
		// module_function inside Kernel converts an Object-defined method.
		{`class Object; def obj_mf_m; :mf; end; end
module Kernel; module_function :obj_mf_m; end
p Kernel.obj_mf_m`, ":mf\n"},
		// A subclass can undef an inherited (Object/Kernel) method via normal
		// ancestor resolution — no bridge needed.
		{`class Object; def obj_undef_m; end; end
class Sub; undef_method :obj_undef_m; end
p Sub.new.respond_to?(:obj_undef_m)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestKernelBridgeErrors covers the NameError raised when a reflective module
// operation names a method that resolves nowhere — both on the Kernel module
// (after the Object fallback also misses) and on an ordinary class.
func TestKernelBridgeErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`module Kernel; alias_method :x, :totally_absent_kernel_m; end`, "undefined method"},
		{`module Kernel; undef_method :totally_absent_kernel_m; end`, "undefined method"},
		{`module Kernel; private :totally_absent_kernel_m; end`, "undefined method"},
		{`Kernel.instance_method(:totally_absent_kernel_m)`, "undefined method"},
		{`class Plain; end; Plain.instance_method(:totally_absent_plain_m)`, "undefined method"},
	}
	for _, c := range cases {
		err := runErr(t, c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q\n got err=%v\nwant contains %q", c.src, err, c.want)
		}
	}
}

// TestRespondToMissingDefault covers Kernel's default respond_to_missing?: it is
// a private method returning false, so respond_to? for an unknown name is false,
// respond_to?(:respond_to_missing?) is false (private), and undef/alias of it
// resolve rather than raising.
func TestRespondToMissingDefault(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class Q; end; p Q.new.respond_to?(:nope)`, "false\n"},
		{`p Object.new.respond_to?(:respond_to_missing?)`, "false\n"},
		{`p Object.new.respond_to?(:respond_to_missing?, true)`, "true\n"},
		// A subclass can undef the inherited default without error.
		{`class R; undef_method :respond_to_missing?; end; p R.new.respond_to?(:nope)`, "false\n"},
		// A user override still takes effect.
		{`class S; def respond_to_missing?(m, _=false); m == :magic; end; end
p [S.new.respond_to?(:magic), S.new.respond_to?(:other)]`, "[true, false]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
