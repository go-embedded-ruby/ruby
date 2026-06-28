// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestPuppetTxnPrimitives covers the primitive fixes that let Puppet build a
// real catalog from a resource declaration (`notify { ... }`) and turn it into
// the Resource Abstraction Layer (to_ral). Each case was reduced from the apply
// trace to a minimal rbgo-vs-MRI snippet and is asserted against MRI Ruby 4.0.5.
func TestPuppetTxnPrimitives(t *testing.T) {
	const extendSetup = "module M; end\nclass C; extend M; end\nclass D < C; end\n"
	cases := []struct{ src, want string }{
		// Hash#fetch with a block default: Puppet's Pops AST does
		// `init_hash.fetch('form') { "regular" }` while parsing a resource
		// declaration. The block is invoked with the missing key only when absent.
		{`p({"a" => 1}.fetch("zz") { "regular" })`, "\"regular\"\n"},
		{`p({"a" => 1}.fetch("a") { "regular" })`, "1\n"},
		{`p({}.fetch("zz") { |k| "blk-#{k}" })`, "\"blk-zz\"\n"},
		{`p({"a" => 1}.fetch("a", 99))`, "1\n"}, // present key beats positional default
		{`p({}.fetch("a", 99))`, "99\n"},        // positional default when absent and no block

		// Array#replace swaps contents for a copy of another array and returns self.
		// Used by Puppet::Resource#initialize (@sensitive_parameters.replace(...)).
		{`a = [1, 2, 3]; a.replace([4, 5]); p a`, "[4, 5]\n"},
		{`a = []; a.replace([:x, :y]); p a`, "[:x, :y]\n"},
		{`a = [1]; p a.replace([2]).equal?(a)`, "true\n"},              // returns self
		{`a = [1]; b = [2, 3]; a.replace(b); b << 4; p a`, "[2, 3]\n"}, // source copied, not aliased

		// Array#member? is an alias of include? (used by Puppet's settings init).
		{`p [1, 2, 3].member?(2)`, "true\n"},
		{`p [1, 2, 3].member?(9)`, "false\n"},
		{`p [].member?(1)`, "false\n"},

		// Pathname#relative_path_from expresses self relative to a base directory
		// using only lexical components (Puppet's autoload walks lib dirs this way).
		{`require "pathname"; p Pathname.new("/a/b/c/d").relative_path_from(Pathname.new("/a/b")).to_s`, "\"c/d\"\n"},
		{`require "pathname"; p Pathname.new("/a/b").relative_path_from(Pathname.new("/a/b/c")).to_s`, "\"..\"\n"},
		{`require "pathname"; p Pathname.new("/a/b/c").relative_path_from(Pathname.new("/a/b/c")).to_s`, "\".\"\n"},
		{`require "pathname"; p Pathname.new("/a/x").relative_path_from(Pathname.new("/a/b/c")).to_s`, "\"../../x\"\n"},
		{`require "pathname"; p Pathname.new("x/y/z").relative_path_from("x").to_s`, "\"y/z\"\n"}, // String base is coerced

		// is_a?/kind_of? through `extend`: after `C.extend(M)`, C and every subclass
		// of C are is_a?(M) because M is inserted into the singleton (meta) class
		// ancestry. This is what makes Puppet::Type (which does
		// `extend Puppet::CompilableResourceType`) and its subtypes (notify, file,
		// …) report as compilable resource types in Resource#initialize.
		{extendSetup + `p C.is_a?(M)`, "true\n"},
		{extendSetup + `p D.is_a?(M)`, "true\n"},                               // subclass inherits the extend
		{extendSetup + `p D.kind_of?(M)`, "true\n"},                            // kind_of? is the alias
		{extendSetup + `p D.new.is_a?(M)`, "false\n"},                          // instances are NOT extended
		{extendSetup + `p C.is_a?(Class)`, "true\n"},                           // ordinary class membership still holds
		{`module M; end; o = Object.new; o.extend(M); p o.is_a?(M)`, "true\n"}, // per-object extend
		{`module M; end; p Object.new.is_a?(M)`, "false\n"},
		// A module prepended into a class's singleton (metaclass) — `class << C;
		// prepend M; end` — makes C.is_a?(M) (the class-meta prepend branch).
		{`module M; end; class C; end; class << C; prepend M; end; p C.is_a?(M)`, "true\n"},
		// A module prepended into a plain object's singleton class makes the object
		// is_a?(M) (the object-singleton prepend branch).
		{`module N; end; o = Object.new; class << o; prepend N; end; p o.is_a?(N)`, "true\n"},
		// extend with a module that transitively includes another: is_a? sees the
		// included module through the object-singleton include branch.
		{`module I; end; module O; include I; end; o = Object.new; o.extend(O); p o.is_a?(I)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`{}.fetch("nope")`, `key not found: "nope"`},
		{`[1].replace(5)`, "no implicit conversion of Integer into Array"},
		// Mixing absolute/relative, or a base that escapes upward, is an error.
		{`require "pathname"; Pathname.new("/a/b").relative_path_from(Pathname.new("x/y"))`, `different prefix: "/" and "x/y"`},
		{`require "pathname"; Pathname.new("a/b").relative_path_from(Pathname.new("../x"))`, `base_directory has ..: "../x"`},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestDefineMethodSuper covers `super` inside a define_method body. Puppet's
// :loglevel metaparam munge is `munge do |loglevel| val = super(loglevel) … end`
// — the munge DSL does `define_method(:unsafe_munge, &block)`, and the block
// calls super against the parent parameter's unsafe_munge. The define_method
// body must anchor `super` to the method's name+owner so super resolves up the
// superclass chain. MRI permits an *explicit*-argument super here but raises a
// RuntimeError for a bare (implicit-argument) super. Asserted against MRI Ruby
// 4.0.5.
func TestDefineMethodSuper(t *testing.T) {
	const base = "class Base\n  def greet(x); \"base-#{x}\"; end\nend\n"
	cases := []struct{ src, want string }{
		// Explicit-argument super from a define_method body resolves to the
		// superclass method.
		{base + "class Sub < Base\n  define_method(:greet) { |x| \"sub-\" + super(x) }\nend\nputs Sub.new.greet(\"hi\")", "sub-base-hi\n"},
		// super from a proc passed as the body argument (define_method(:m, proc)).
		{base + "blk = proc { |x| \"p-\" + super(x) }\nclass Sub2 < Base; end\nSub2.define_method(:greet, blk)\nputs Sub2.new.greet(\"z\")", "p-base-z\n"},
		// A super nested inside a block within the define_method body still resolves.
		{base + "class Sub3 < Base\n  define_method(:greet) { |x| [x].map { |y| super(y) }.first }\nend\nputs Sub3.new.greet(\"q\")", "base-q\n"},
		// A normal (def) method's super is unaffected by the dmBody flag.
		{base + "class Sub4 < Base\n  def greet(x); \"d-\" + super; end\nend\nputs Sub4.new.greet(\"k\")", "d-base-k\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// Bare (implicit-argument) super from a define_method body is forbidden.
		{base + "class S < Base\n  define_method(:greet) { |x| super }\nend\nS.new.greet(\"a\")",
			"implicit argument passing of super from method defined by define_method()"},
		// Explicit super with no matching superclass method raises NoMethodError.
		{"class Lone\n  define_method(:foo) { super() }\nend\nLone.new.foo",
			"super: no superclass method 'foo'"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
