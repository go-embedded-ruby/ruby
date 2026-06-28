// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestPuppetApplyPrimitives covers the small primitive fixes that together let
// `Puppet::Parser::Compiler#compile` evaluate a trivial manifest: each case was
// reduced from the apply trace to a minimal rbgo-vs-MRI snippet and is asserted
// against MRI Ruby 4.0.5.
func TestPuppetApplyPrimitives(t *testing.T) {
	cases := []struct{ src, want string }{
		// Module.new builds a real module: def self.x is a singleton method, the
		// result is includable, and methods define into it.
		{`m = Module.new { def self.f; 7; end }; p m.f`, "7\n"},
		{`m = Module.new { def g; 5; end }; c = Class.new { include m }; p c.new.g`, "5\n"},
		{`p Module.new.class`, "Module\n"},
		{`p Module.new.is_a?(Class)`, "false\n"},
		{`module Foo; end; p Foo.class`, "Module\n"},
		{`p String.class`, "Class\n"},

		// extend mixes the module's whole include tree (transitive) as class methods.
		{`module I; def desc(x); @d=x; end; end
module O; include I; end
class T; extend O; end
T.desc("hi"); p T.instance_variable_get(:@d)`, "\"hi\"\n"},
		// public_methods / singleton_methods enumerate those extended methods.
		{`module I2; def m2; end; end
module O2; include I2; end
class T2; extend O2; end
p T2.public_methods.include?(:m2)`, "true\n"},
		// An object that extends a module reports that module's methods in
		// #singleton_methods (the object-singleton include path).
		{`module M3; def sm; end; end; o = Object.new; o.extend(M3); p o.singleton_methods.include?(:sm)`, "true\n"},
		// extend a module that prepends another: the prepended module's methods are
		// enumerated too (the prepend walk in the singleton-method collector).
		{`module P4; def pm; end; end
module O4; prepend P4; end
class T4; extend O4; end
p T4.singleton_methods.include?(:pm)`, "true\n"},
		// A subclass inherits class methods contributed by a module its SUPERCLASS
		// extended (and that module's prepends), via the super-chain meta walk.
		{`module P5; def pm5; end; end
module O5; prepend P5; def om5; end; end
class Base5; extend O5; end
class Sub5 < Base5; end
p [Sub5.singleton_methods.include?(:om5), Sub5.singleton_methods.include?(:pm5)]`, "[true, true]\n"},

		// SystemExit#status defaults to 0; with @status unset (allocate skips
		// initialize) it falls back to 0 rather than nil.
		{`p SystemExit.new.status`, "0\n"},
		{`p SystemExit.allocate.status`, "0\n"},

		// A class method contributed via `class << SomeClass; prepend Mod; end`
		// (the metaclass-prepend path) appears in #singleton_methods.
		{`module Pre6; def pp6; end; end; class K6; end
class << K6; prepend Pre6; end
p K6.singleton_methods.include?(:pp6)`, "true\n"},

		// public/private/protected_method_defined?.
		{`class A; def a; end; private def b; end; protected def c; end; end
p [A.public_method_defined?(:a), A.private_method_defined?(:b), A.protected_method_defined?(:c), A.public_method_defined?(:zz)]`,
			"[true, true, true, false]\n"},

		// Proc#source_location: nil for a Proc compiled without a source file.
		{`p proc { 1 }.source_location`, "nil\n"},

		// Array#insert (positive, negative, end-padding, multi-value).
		{`a=[1,2,3]; a.insert(1,:x); p a`, "[1, :x, 2, 3]\n"},
		{`a=[1,2,3]; a.insert(-1,:y); p a`, "[1, 2, 3, :y]\n"},
		{`a=[1,2,3]; a.insert(-2,:z); p a`, "[1, 2, :z, 3]\n"},
		{`a=[]; a.insert(2,:g); p a`, "[nil, nil, :g]\n"},
		{`a=[1,2]; a.insert(1,:a,:b); p a`, "[1, :a, :b, 2]\n"},
		{`a=[1,2,3]; p a.insert(1).equal?(a)`, "true\n"}, // no values: returns self unchanged

		// MatchData#[] Range and (start, length) slice the group array.
		{`m = /(\w+)(\d+)/.match("abc123"); p m[1..]`, "[\"abc12\", \"3\"]\n"},
		{`m = /(\w+)(\d+)/.match("abc123"); p m[1, 1]`, "[\"abc12\"]\n"},
		{`m = /(\w+)(\d+)/.match("abc123"); p m[1]`, "\"abc12\"\n"},                         // integer still works
		{`m = /(\w+)(\d+)/.match("abc123"); p m[5..]`, "nil\n"},                             // Range out of bounds -> nil
		{`m = /(\w+)(\d+)/.match("abc123"); p m[5, 1]`, "nil\n"},                            // start past end -> nil
		{`m = /(\w+)(\d+)/.match("abc123"); p m[-1, 1]`, "[\"3\"]\n"},                       // negative start
		{`m = /(\w+)(\d+)/.match("abc123"); p m[0, 9]`, "[\"abc123\", \"abc12\", \"3\"]\n"}, // length clamps to end
		{`m = /(\w+)(\d+)/.match("abc123"); p m[1, -1]`, "nil\n"},                           // negative length -> nil

		// ObjectSpace: finalizer API + reflective no-ops.
		{`p ObjectSpace.define_finalizer(Object.new) { |i| }`, "[0, #<Proc>]\n"},
		{`pr = proc { }; p ObjectSpace.define_finalizer(Object.new, pr)[1].equal?(pr)`, "true\n"}, // explicit callable
		{`o = Object.new; p ObjectSpace.undefine_finalizer(o).equal?(o)`, "true\n"},
		{`p ObjectSpace.each_object`, "0\n"},
		{`p ObjectSpace.garbage_collect`, "nil\n"},
		{`p ObjectSpace.count_objects`, "{}\n"},
		{`require "fiber"; require "objspace"; p :ok`, ":ok\n"},

		// Concurrent::ThreadLocalVar honours a default block lazily.
		{`require "concurrent"; t = Concurrent::ThreadLocalVar.new { [9] }; p t.value`, "[9]\n"},
		{`require "concurrent"; t = Concurrent::ThreadLocalVar.new(5); p t.value`, "5\n"},
		{`require "concurrent"; t = Concurrent::ThreadLocalVar.new; p t.value`, "nil\n"},
		{`require "concurrent"; t = Concurrent::ThreadLocalVar.new { [9] }; t.value = [1]; p t.value`, "[1]\n"},

		// A syntactic setter expression evaluates to the assigned value, not the
		// setter method's return value (attribute and index forms).
		{`class C; def foo=(v); @v=v; :ignored; end; end; p(C.new.foo = 7)`, "7\n"},
		{`class D; def []=(k,v); @h=v; :ignored; end; end; p(D.new[1] = 99)`, "99\n"},
		{`class E; attr_accessor :x; def set; self.x = 3; end; end; p E.new.set`, "3\n"},
		// An explicitly-written operator method (=='s name ends in '=') is NOT a
		// setter and returns the method's own value.
		{`p 1.==(2)`, "false\n"},
		{`p "a".=~(/b/)`, "nil\n"},
		{`o = Object.new; def o.x=(v); 42; end; o.x = 9; p o.send(:x=, 9)`, "42\n"}, // explicit send keeps method return

		// A Set holds arbitrary objects keyed by identity (the apply path adds
		// Puppet resources to a Set).
		{`require "set"; s = Set.new; s << Object.new << Object.new; p s.size`, "2\n"},
		{`require "set"; o = Object.new; s = Set.new; s << o << o; p s.size`, "1\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Proc#source_location reports [file, 0] when the block was written in a file
	// with a known path (here the script path the VM was given).
	if got, err := runScript(t, `p proc { }.source_location`, "/scripts/main.rb"); err != nil || got != "[\"/scripts/main.rb\", 0]\n" {
		t.Errorf("Proc#source_location got=%q err=%v", got, err)
	}
	// ObjectSpace.start aliases garbage_collect; both no-ops.
	if got := eval(t, `p ObjectSpace.start`); got != "nil\n" {
		t.Errorf("ObjectSpace.start got=%q", got)
	}
	// The finalizer methods require an object argument; Array#insert requires at
	// least an index, and rejects an index more negative than -(len+1).
	for _, c := range []struct{ src, want string }{
		{`ObjectSpace.define_finalizer`, "ArgumentError"},
		{`ObjectSpace.undefine_finalizer`, "ArgumentError"},
		{`[1].insert`, "ArgumentError"},
		{`[1, 2].insert(-5, :x)`, "IndexError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want %s", c.src, err, c.want)
		}
	}
}
