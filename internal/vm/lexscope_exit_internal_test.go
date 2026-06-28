// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestClassEvalDefLexScope covers a `def` written inside a class_eval'd block:
// its body resolves bare constants against the block's lexical scope (where the
// def was written), not the eval receiver — the fix that lets a Puppet provider's
// `def mode=; File.chmod … end` reach ::File rather than Puppet::Type::File.
func TestClassEvalDefLexScope(t *testing.T) {
	cases := []struct{ src, want string }{
		// Receiver itself is named File; a def inside class_eval must see ::File.
		{`module P
  module Type
    class File
      def self.provide(&b); c = Class.new; const_set(:Prov, c); c.class_eval(&b); c; end
    end
  end
end
P::Type::File.provide do
  def core?; File.respond_to?(:dirname); end
end
p P::Type::File::Prov.new.core?`, "true\n"},
		// A normal def (no class_eval) is unaffected: bare const resolves via the
		// class's own nesting, so a same-named nested const still wins.
		{`module Q
  WIDGET = :outer
  class Thing
    WIDGET = :inner
    def w; WIDGET; end
  end
end
p Q::Thing.new.w`, ":inner\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDefineMethodReturn covers `return` inside a define_method body (a return
// target — returns from the method invocation) and a non-local return from a
// block nested inside such a body (returns from the define_method method).
func TestDefineMethodReturn(t *testing.T) {
	cases := []struct{ src, want string }{
		// return in the dm body returns from the method.
		{`class A
  def self.cap(&b); define_method(:run, &b); end
  cap { return :ok }
end
p A.new.run`, ":ok\n"},
		// return under class_eval too.
		{`class B; def self.cap(&b); define_method(:run, &b); end; end
B.class_eval { cap { return :z } }
p B.new.run`, ":z\n"},
		// non-local return from a block nested in the dm body exits the method.
		{`class C
  def self.cap(&b); define_method(:run, &b); end
  cap do
    [1, 2, 3].each { |x| return "hit#{x}" if x == 2 }
    "fell"
  end
end
p C.new.run`, "\"hit2\"\n"},
		// a dm body that does not return falls through to its last expression.
		{`class D
  def self.cap(&b); define_method(:run, &b); end
  cap { 41 + 1 }
end
p D.new.run`, "42\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKernelExit covers Kernel#exit / exit! / abort and Kernel.exit, including a
// clean (non-error) termination and at_exit handlers still running, plus
// rescue SystemExit catching the raised exit.
func TestKernelExit(t *testing.T) {
	cases := []struct{ src, want string }{
		{`puts "a"; exit; puts "b"`, "a\n"},                                             // exit halts before "b"
		{`puts "a"; exit!; puts "b"`, "a\n"},                                            // exit! likewise
		{`Kernel.exit; puts "x"`, ""},                                                   // module-method form
		{`at_exit { puts "bye" }; puts "hi"; exit`, "hi\nbye\n"},                        // at_exit runs on exit
		{`begin; exit; rescue SystemExit; puts "caught"; end`, "caught\n"},              // rescuable
		{`begin; abort("boom"); rescue SystemExit; puts "after"; end`, "boom\nafter\n"}, // abort prints+raises
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIsSystemExit covers the isSystemExit helper for SystemExit, a subclass,
// and an unrelated/unknown class.
func TestIsSystemExit(t *testing.T) {
	vm := New(nil)
	if !vm.isSystemExit("SystemExit") {
		t.Errorf("SystemExit not recognised")
	}
	// A user subclass of SystemExit is still a SystemExit.
	runFSOn(t, vm, `class MyExit < SystemExit; end`)
	if !vm.isSystemExit("MyExit") {
		t.Errorf("MyExit (< SystemExit) not recognised")
	}
	if vm.isSystemExit("StandardError") {
		t.Errorf("StandardError wrongly recognised as SystemExit")
	}
	// An unknown class name falls back to the literal match.
	if vm.isSystemExit("NoSuchClassXYZ") {
		t.Errorf("unknown class wrongly recognised")
	}
}

// runFSOn evaluates src on an existing VM (so a follow-up isSystemExit sees the
// class it defined).
func runFSOn(t *testing.T, vm *VM, src string) {
	t.Helper()
	iseq, err := parseCompileFn(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	if _, rerr := vm.Run(iseq); rerr != nil {
		t.Fatalf("runtime error: %v", rerr)
	}
}

// TestHashDeleteMutators covers Hash#delete_if/reject!/keep_if/select!/filter!,
// their return-value contract (delete_if/keep_if return the hash; reject!/select!
// return nil when nothing changed), and the enumerator form when no block is
// given. Asserted vs MRI 4.0.5.
func TestHashDeleteMutators(t *testing.T) {
	cases := []struct{ src, want string }{
		{`h={a:1,b:2,c:3}; h.delete_if{|k,v| v.even?}; p h`, "{a: 1, c: 3}\n"},
		{`h={a:1,b:2}; r=h.reject!{|k,v| v>1}; p [h, r.equal?(h)]`, "[{a: 1}, true]\n"},
		{`h={a:1,b:2}; p h.reject!{|k,v| v>9}`, "nil\n"}, // nothing removed -> nil
		{`h={a:1,b:2,c:3}; h.keep_if{|k,v| v.odd?}; p h`, "{a: 1, c: 3}\n"},
		{`h={a:1,b:2}; r=h.select!{|k,v| v==1}; p [h, r.equal?(h)]`, "[{a: 1}, true]\n"},
		{`h={a:1,b:2}; p h.select!{|k,v| v>0}`, "nil\n"}, // all kept -> nil
		{`h={a:1,b:2}; h.filter!{|k,v| v==2}; p h`, "{b: 2}\n"},
		// delete_if returns self (the hash), enabling chaining.
		{`h={a:1,b:2}; p h.delete_if{|k,v| false}.equal?(h)`, "true\n"},
		// no-block forms return an Enumerator.
		{`p({a:1}.delete_if.class.to_s)`, "\"Enumerator\"\n"},
		{`p({a:1}.reject!.class.to_s)`, "\"Enumerator\"\n"},
		{`p({a:1}.keep_if.class.to_s)`, "\"Enumerator\"\n"},
		{`p({a:1}.select!.class.to_s)`, "\"Enumerator\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestEncodingDefaults covers Encoding.default_external/default_internal/find and
// the setters, matching MRI (default_external UTF-8, default_internal nil).
func TestEncodingDefaults(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Encoding.default_external.name`, "\"UTF-8\"\n"},
		{`p Encoding.default_internal`, "nil\n"},
		{`p Encoding.find("UTF-8").name`, "\"UTF-8\"\n"},
		{`p Encoding.find(Encoding::UTF_8).name`, "\"UTF-8\"\n"},
		// Setters are accepted and return their argument; external is remembered.
		{`Encoding.default_external = "US-ASCII"; p Encoding.default_external.name`, "\"US-ASCII\"\n"},
		{`p(Encoding.default_internal = "UTF-8")`, "\"UTF-8\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestOpenSSLVersionConstants covers the OpenSSL version-identification
// constants Puppet's log_runtime_environment reads.
func TestOpenSSLVersionConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p OpenSSL::VERSION`, "\"4.0.0\"\n"},
		{`p OpenSSL::OPENSSL_VERSION.is_a?(String)`, "true\n"},
		{`p OpenSSL::OPENSSL_LIBRARY_VERSION.is_a?(String)`, "true\n"},
		{`p OpenSSL::OPENSSL_VERSION_NUMBER`, "0\n"},
		{`p OpenSSL::OPENSSL_FIPS`, "false\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
