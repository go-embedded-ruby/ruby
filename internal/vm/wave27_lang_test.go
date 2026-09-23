// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestDefinedSuper covers OpDefinedSuper and superMethod: DEFINED_ZSUPER asks
// whether the method `super` would reach exists, without calling it, and answers
// nil when there is none — or when the frame has no method at all.
func TestDefinedSuper(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// A superclass method exists.
		{`class A; def m; end; end
		  class B < A; def m; p defined?(super); end; end
		  B.new.m`, "\"super\"\n"},
		// None does.
		{`class C; def m; p defined?(super); end; end
		  C.new.m`, "nil\n"},
		// From a block inside the method: a block is transparent to super.
		{`class A2; def m; end; end
		  class B2 < A2; def m; [1].each { p defined?(super) }; end; end
		  B2.new.m`, "\"super\"\n"},
		// From a define_method body.
		{`class A3; def m; end; end
		  class B3 < A3; define_method(:m) { p defined?(super) }; end
		  B3.new.m`, "\"super\"\n"},
		// Through an included module's method, into the including hierarchy.
		{`class D; def m; end; end
		  module M; def m; p defined?(super); end; end
		  class E < D; include M; end
		  E.new.m`, "\"super\"\n"},
		// An undef'ed superclass method is a blocker, not a definition.
		{`class F; def m; end; end
		  class G < F; undef_method :m; end
		  class H < G; def m; p defined?(super); end; end
		  H.new.m`, "nil\n"},
		// Outside any method there is no method entry to ask about.
		{`p defined?(super)`, "nil\n"},
		// The super ARGUMENTS are not evaluated.
		{`class A4; def m(x); end; end
		  class B4 < A4; def m(x); p defined?(super(raise("never"))); end; end
		  B4.new.m(1)`, "\"super\"\n"},
		// A class-method super.
		{`class A5; def self.m; end; end
		  class B5 < A5; def self.m; p defined?(super); end; end
		  B5.m`, "\"super\"\n"},
	} {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestDefinedSuperInRefinement covers superMethod's refinement arm: a `super`
// written in a refinement method resolves in the REFINED class alone.
func TestDefinedSuperInRefinement(t *testing.T) {
	src := `class RBase; def m; "base"; end; end
	        module RMod
	          refine RBase do
	            def m; p defined?(super); super; end
	          end
	        end
	        using RMod
	        p RBase.new.m`
	if got, want := eval(t, src), "\"super\"\n\"base\"\n"; got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestDefinedTagsAreFrozen covers the runtime half of the tag change: the tags
// the VM decides (constant/method/yield/…) are frozen too, as MRI's
// rb_iseq_defined_string fstrings are, and a frozen String CONSTANT is shared by
// OpPushConst rather than duplicated.
func TestDefinedTagsAreFrozen(t *testing.T) {
	src := `p defined?(nil).frozen?
	        p defined?(self).frozen?
	        p defined?(String).frozen?
	        p defined?(puts).frozen?
	        p defined?(@x = 1).frozen?
	        $g = 1; p defined?($g).frozen?
	        @iv = 1; p defined?(@iv).frozen?
	        class FT; @@cv = 1; def m; defined?(@@cv); end; end
	        p FT.new.m.frozen?
	        def fy; defined?(yield); end
	        p fy { }.frozen?
	        p defined?(::String).frozen?
	        p defined?(Kernel::Integer).frozen?
	        p "plain".frozen?`
	want := "true\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\nfalse\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestDefinedMethodVisibility covers definedResponds: DEFINED_FUNC (no receiver
// written) counts any visibility, DEFINED_METHOD (a receiver written) screens
// it, and a protected method answers only for a kin self.
func TestDefinedMethodVisibility(t *testing.T) {
	src := `class V
	          def pub; end
	          private; def priv; end
	          protected; def prot; end
	          public
	          def probe; [defined?(priv), defined?(self.priv), defined?(prot), defined?(self.prot), defined?(self.pub)]; end
	        end
	        p V.new.probe
	        p defined?(V.new.priv)
	        p defined?(V.new.prot)
	        class W < V; def probe2; defined?(V.new.prot); end; end
	        p W.new.probe2`
	want := "[\"method\", nil, \"method\", \"method\", \"method\"]\nnil\nnil\n\"method\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestDefinedRespondToMissing covers the respond_to_missing? fallback of both
// probes, and the branch where the receiver has no such hook to ask.
func TestDefinedRespondToMissing(t *testing.T) {
	src := `o = Object.new
	        def o.respond_to_missing?(n, p) = n == :yes
	        p defined?(o.yes)
	        p defined?(o.no)
	        p defined?(Object.new.whatever)
	        b = BasicObject.new
	        p defined?(b.whatever)`
	want := "\"method\"\nnil\nnil\nnil\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestDefinedOperatorReceiverKind covers hasInlineArith: the arithmetic
// operators have no method-table entry in rbgo, so defined? falls back to the
// fast path — but only for a receiver that path computes on. nil, true/false and
// a Symbol have no arithmetic at all, and a user object with no built-in backing
// belongs to the respond_to_missing? path.
func TestDefinedOperatorReceiverKind(t *testing.T) {
	src := `p defined?(1 / 2)
	        p defined?("a" % 1)
	        p defined?([1] + [2])
	        p defined?(nil / 2)
	        p defined?(true + 1)
	        p defined?(:a + 1)
	        p defined?(Object.new + 1)
	        class SubStr < String; end
	        p defined?(SubStr.new("a") + "b")`
	want := "\"method\"\n\"method\"\n\"method\"\nnil\nnil\nnil\nnil\n\"method\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestRespondsToOperatorFastPath covers the operator arm of respondsTo, the
// VM-internal predicate (distinct from defined?'s) that Object#try and the
// library bindings use to ask whether a receiver answers a name.
func TestRespondsToOperatorFastPath(t *testing.T) {
	if got, want := eval(t, `p 1.try(:+, 2)`), "3\n"; got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestDefinedInVoidContextDoesNotRunTheReceiver covers the runtime consequence
// of compileDiscarded.
func TestDefinedInVoidContextDoesNotRunTheReceiver(t *testing.T) {
	src := `$ran = false
	        def se; $ran = true; 1; end
	        defined?(se / 2)
	        p $ran
	        p defined?(se / 2)
	        p $ran`
	want := "false\n\"method\"\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestBlockAutoSplatUsesToAry covers the auto-splat conversion: a lone argument
// handed to a multi-parameter block goes through rb_check_array_type, so #to_ary
// converts, a non-Array answer is a TypeError, and an object that declines stays
// whole.
func TestBlockAutoSplatUsesToAry(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`o = Object.new; def o.to_ary = [1, 2]
		  [o].each { |a, b| p [a, b] }`, "[1, 2]\n"},
		{`o = Object.new
		  [o].each { |a, b| p [a.class, b] }`, "[Object, nil]\n"},
		{`o = Object.new; def o.to_ary = nil
		  [o].each { |a, b| p [a.class, b] }`, "[Object, nil]\n"},
		{`o = Object.new; def o.to_ary = 1
		  begin; [o].each { |a, b| }; rescue TypeError => e; p e.class; end`, "TypeError\n"},
		{`o = Object.new; def o.to_ary = raise("boom")
		  begin; [o].each { |a, b| }; rescue RuntimeError => e; p e.message; end`, "\"boom\"\n"},
		{`[[1, 2]].each { |a, b| p [a, b] }`, "[1, 2]\n"},
		{`[[1, 2]].each { |a| p a }`, "[1, 2]\n"},
	} {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestPatternMatchingDeconstructContract covers the pattern-matching changes:
// the constant is tested with ===, before the protocol call; #deconstruct_keys
// is told which keys the pattern wants; a wrong return type is a TypeError; and
// #deconstruct is called once per `case`.
func TestPatternMatchingDeconstructContract(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// The constant decides through ===, and is asked FIRST.
		{`class NoK; def self.===(o) = false; end
		  o = Object.new; def o.deconstruct_keys(k) = (p :asked; {a: 1})
		  case o
		  in NoK(a: 1) then p :matched
		  else p :else
		  end`, ":else\n"},
		{`class YesK; def self.===(o) = true; end
		  o = Object.new; def o.deconstruct_keys(k) = {a: 1}
		  case o
		  in YesK(a: 1) then p :matched
		  end`, ":matched\n"},
		// The key list reaches #deconstruct_keys.
		{`o = Object.new; def o.deconstruct_keys(k) = (p k; {a: 1, b: 2})
		  case o
		  in {a:, b:} then p :ok
		  end`, "[:a, :b]\n:ok\n"},
		{`o = Object.new; def o.deconstruct_keys(k) = (p k; {a: 1, b: 2})
		  case o
		  in {a:, **rest} then p :ok
		  end`, "nil\n:ok\n"},
		{`o = Object.new; def o.deconstruct_keys(k) = (p k; {a: 1})
		  case o
		  in {a:, **nil} then p :ok
		  end`, "nil\n:ok\n"},
		// A wrong return type is a TypeError, not a failed match.
		{`class BadD; def deconstruct = 1; end
		  begin; case BadD.new; in [1]; end; rescue TypeError => e; p e.message; end`,
			"\"deconstruct must return Array\"\n"},
		{`class BadK2; def deconstruct_keys(k) = 1; end
		  begin; case BadK2.new; in {a: 1}; end; rescue TypeError => e; p e.message; end`,
			"\"deconstruct_keys must return Hash\"\n"},
		// #deconstruct is asked once for the whole case, across clauses…
		{`$n = 0
		  o = Object.new; def o.deconstruct = ($n += 1; [0, 1])
		  case o
		  in [1, 2] then p false
		  in [0, 1] then p true
		  end
		  p $n`, "true\n1\n"},
		// …and its ABSENCE is remembered too, so respond_to? is asked once.
		{`$n = 0
		  o = Object.new
		  def o.respond_to?(m, p = false) = ($n += 1; false)
		  case o
		  in [1] then p :a
		  in [2] then p :b
		  else p :else
		  end
		  p $n`, ":else\n1\n"},
		// A nested pattern matches an ELEMENT, so it must not read the cache.
		{`case [[1, 2], [3, 4]]
		  in [[1, 2], [3, 4]] then p :nested
		  end`, ":nested\n"},
		// A case in a loop re-deconstructs each new subject.
		{`2.times do |i|
		    o = Object.new
		    o.define_singleton_method(:deconstruct) { [i] }
		    case o
		    in [0] then p :zero
		    in [1] then p :one
		    end
		  end`, ":zero\n:one\n"},
		// A find pattern takes the same protocol.
		{`o = Object.new; def o.deconstruct = [1, 2, 3]
		  case o
		  in [*, 2, *] then p :found
		  end`, ":found\n"},
		// A bare constant pattern goes through === as well.
		{`class OnlyEq; def self.===(o) = o == 7; end
		  case 7
		  in OnlyEq then p :seven
		  end`, ":seven\n"},
	} {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestBacktickLiteralCallsTheMethod covers OpXStr: the literal is a CALL of
// “ ` “ on self, with a frozen String, so a redefinition receives it.
func TestBacktickLiteralCallsTheMethod(t *testing.T) {
	src := `runner = Object.new
	        runner.singleton_class.define_method(:` + "`" + `) do |str|
	          p [str, str.frozen?]
	          :from_backtick
	        end
	        runner.instance_exec { p ` + "`test command`" + ` }
	        runner.instance_exec { p %x(other command) }`
	want := "[\"test command\", true]\n:from_backtick\n[\"other command\", true]\n:from_backtick\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}
