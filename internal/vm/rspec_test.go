// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"
)

// TestRSpecConstants covers the RSpec loadable module and its class / error tree
// (require "rspec").
func TestRSpecConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rspec"; p RSpec.is_a?(Module)`, "true\n"},
		{`p require "rspec"`, "true\n"},
		{`require "rspec"; p require "rspec"`, "false\n"},
		{`p require "rspec/expectations"`, "true\n"},
		// The expectation-failure error is an Exception (not a StandardError).
		{`require "rspec"; p RSpec::Expectations::ExpectationNotMetError < Exception`, "true\n"},
		// It is NOT a StandardError, so `<` yields nil (Ruby returns nil for
		// unrelated classes), confirming a bare rescue does not swallow it.
		{`require "rspec"; p RSpec::Expectations::ExpectationNotMetError < StandardError`, "nil\n"},
		// eq(1) builds a matcher object.
		{`require "rspec"; p eq(1).is_a?(RSpec::Matchers::BuiltIn::BaseMatcher)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRSpecExpect covers the core expect(x).to / .not_to flow across the matcher
// surface, checking both the passing path (no output / nil) and the failing path
// (ExpectationNotMetError with the library's failure message).
func TestRSpecExpect(t *testing.T) {
	cases := []struct{ src, want string }{
		// A passing expectation returns nil and raises nothing.
		{`require "rspec"; p(expect(1).to(eq(1)))`, "nil\n"},
		{`require "rspec"; expect(1).to eq(1); puts "ok"`, "ok\n"},
		// A failing expectation raises ExpectationNotMetError with the message.
		{`require "rspec"
begin
  expect(1).to eq(2)
rescue RSpec::Expectations::ExpectationNotMetError => e
  puts e.message
end`, "\nexpected: 2\n     got: 1\n\n(compared using ==)\n"},
		// not_to inverts.
		{`require "rspec"; expect(1).not_to eq(2); puts "ok"`, "ok\n"},
		{`require "rspec"
begin
  expect(1).not_to eq(1)
rescue RSpec::Expectations::ExpectationNotMetError
  puts "failed"
end`, "failed\n"},
		// to_not is an alias for not_to.
		{`require "rspec"; expect(1).to_not eq(2); puts "ok"`, "ok\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRSpecMatchers covers the matcher constructors and their match logic through
// expect, plus the matcher predicate surface (matches? / failure_message /
// description).
func TestRSpecMatchers(t *testing.T) {
	cases := []struct{ src, want string }{
		// matches? / failure_message / failure_message_when_negated / description.
		{`require "rspec"
m = eq(1)
p m.matches?(1)
p m.matches?(2)
p m.description`, "true\nfalse\n\"eq 1\"\n"},
		// be with no arg is truthiness; be(x) is identity.
		{`require "rspec"
expect(1).to be
expect(nil).not_to be
expect(:x).to be(:x)
puts "ok"`, "ok\n"},
		// be_truthy / be_falsey / be_nil.
		{`require "rspec"
expect(true).to be_truthy
expect(false).to be_falsey
expect(nil).to be_nil
puts "ok"`, "ok\n"},
		// comparison and be_within (Float and Integer delta/centre arms).
		{`require "rspec"
expect(5).to be_greater_than(3)
expect(2).to be_less_than(4)
expect(3.14).to be_within(0.01).of(3.14)
expect(10).to be_within(2).of(11)
puts "ok"`, "ok\n"},
		// type matchers.
		{`require "rspec"
expect("s").to be_a(String)
expect(1).to be_an_instance_of(Integer)
expect([]).to be_kind_of(Array)
puts "ok"`, "ok\n"},
		// collection matchers.
		{`require "rspec"
expect([1, 2, 3]).to include(2)
expect([1, 2, 3]).to start_with(1)
expect([1, 2, 3]).to end_with(3)
expect([3, 1, 2]).to contain_exactly(1, 2, 3)
expect([3, 1, 2]).to match_array([1, 2, 3])
puts "ok"`, "ok\n"},
		// match against a Regexp.
		{`require "rspec"
expect("hello").to match(/ell/)
puts "ok"`, "ok\n"},
		// cover over a Range.
		{`require "rspec"
expect(1..10).to cover(5)
puts "ok"`, "ok\n"},
		// respond_to on a real object, with a Symbol.
		{`require "rspec"
expect("string").to respond_to(:upcase)
expect("string").not_to respond_to(:nonexistent_zzz)
puts "ok"`, "ok\n"},
		// and / or combinators via & / |.
		{`require "rspec"
expect(5).to (be_greater_than(1) & be_less_than(10))
expect(5).to (eq(5) | eq(6))
puts "ok"`, "ok\n"},
		// all(matcher).
		{`require "rspec"
expect([2, 4, 6]).to all(be_greater_than(0))
puts "ok"`, "ok\n"},
		// eql (value+type) and equal (identity).
		{`require "rspec"
expect(1).to eql(1)
expect(:sym).to equal(:sym)
puts "ok"`, "ok\n"},
		// Value shapes exercised through eq: Bignum, Float, Range, Regexp, Hash,
		// Symbol, nested Array, and an arbitrary object.
		{`require "rspec"
expect(2 ** 70).to eq(2 ** 70)
expect(1.5).to eq(1.5)
expect(1..3).to eq(1..3)
expect({a: 1}).to eq({a: 1})
expect([[1], [2]]).to eq([[1], [2]])
puts "ok"`, "ok\n"},
		// A user object reflects its class through be_a.
		{`require "rspec"
class Widget; end
w = Widget.new
expect(w).to be_a(Widget)
puts "ok"`, "ok\n"},
		// be_a accepts a String or Symbol class name.
		{`require "rspec"
expect("x").to be_a("String")
puts "ok"`, "ok\n"},
		// respond_to(:m).with(n).arguments chaining.
		{`require "rspec"
expect("s").to respond_to(:upcase).with(0).arguments
puts "ok"`, "ok\n"},
		// be_within(d).of(x) failing path.
		{`require "rspec"
begin
  expect(3.0).to be_within(0.1).of(5.0)
rescue RSpec::Expectations::ExpectationNotMetError
  puts "within-failed"
end`, "within-failed\n"},
		// raise_error(Class) constrains the class through the block observation.
		{`require "rspec"
begin
  expect { raise TypeError, "x" }.to raise_error(ArgumentError)
rescue RSpec::Expectations::ExpectationNotMetError
  puts "wrong-class"
end`, "wrong-class\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRSpecRaiseError covers the block-form raise_error matcher: expect { … }.to
// raise_error observes the block's execution through rbgo.
func TestRSpecRaiseError(t *testing.T) {
	cases := []struct{ src, want string }{
		// A block that raises satisfies raise_error.
		{`require "rspec"
expect { raise "boom" }.to raise_error
puts "ok"`, "ok\n"},
		// raise_error(Class) constrains the class.
		{`require "rspec"
expect { raise ArgumentError, "x" }.to raise_error(ArgumentError)
puts "ok"`, "ok\n"},
		// raise_error(Class, message) constrains both.
		{`require "rspec"
expect { raise ArgumentError, "bad thing" }.to raise_error(ArgumentError, "bad thing")
puts "ok"`, "ok\n"},
		// raise_error(Class, /regexp/) matches the message by pattern.
		{`require "rspec"
expect { raise RuntimeError, "bad thing" }.to raise_error(RuntimeError, /bad/)
puts "ok"`, "ok\n"},
		// A block that does NOT raise fails the positive expectation.
		{`require "rspec"
begin
  expect { 1 + 1 }.to raise_error
rescue RSpec::Expectations::ExpectationNotMetError
  puts "no-raise-failed"
end`, "no-raise-failed\n"},
		// not_to raise_error passes when the block is clean.
		{`require "rspec"
expect { 1 + 1 }.not_to raise_error
puts "ok"`, "ok\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRSpecErrors covers the argument-arity and matcher-type guards.
func TestRSpecErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// expect with no argument and no block raises ArgumentError.
		{`require "rspec"; begin; expect; rescue ArgumentError; p :e; end`, ":e\n"},
		// to with a non-matcher raises ArgumentError.
		{`require "rspec"; begin; expect(1).to(1); rescue ArgumentError; p :m; end`, ":m\n"},
		// matches? with no argument raises ArgumentError.
		{`require "rspec"; begin; eq(1).matches?; rescue ArgumentError; p :a; end`, ":a\n"},
		// of on a non-be_within matcher raises NoMethodError.
		{`require "rspec"; begin; eq(1).of(2); rescue NoMethodError; p :of; end`, ":of\n"},
		// with on a non-respond_to matcher raises NoMethodError.
		{`require "rspec"; begin; eq(1).with(2); rescue NoMethodError; p :with; end`, ":with\n"},
		// & / | with a non-matcher argument raise ArgumentError.
		{`require "rspec"; begin; eq(1) & 5; rescue ArgumentError; p :amp; end`, ":amp\n"},
		{`require "rspec"; begin; eq(1) | 5; rescue ArgumentError; p :pipe; end`, ":pipe\n"},
		// bignum arg to with / of and a non-numeric to be_within cover coercion.
		{`require "rspec"; begin; be_within("x"); rescue TypeError; p :bw; end`, ":bw\n"},
		{`require "rspec"; begin; respond_to(:m).with("x"); rescue TypeError; p :wt; end`, ":wt\n"},
		// failure_message / failure_message_when_negated / description surface.
		{`require "rspec"
m = eq(2)
m.matches?(1)
p m.failure_message.include?("expected")
p m.failure_message_when_negated.include?("!=")`, "true\ntrue\n"},
		// matches? on a matcher without its own description falls back to "match".
		{`require "rspec"; p be_truthy.description`, "\"match\"\n"},
		// The arguments / argument tails are no-ops returning the matcher.
		{`require "rspec"; p respond_to(:x).argument.is_a?(RSpec::Matchers::BuiltIn::BaseMatcher)`, "true\n"},
		// The be_* aliases (be_falsy / be_an / be_a_kind_of / be_instance_of).
		{`require "rspec"
expect(false).to be_falsy
expect([]).to be_an(Array)
expect(1).to be_a_kind_of(Integer)
expect(1).to be_instance_of(Integer)
puts "ok"`, "ok\n"},
		// eq with no argument compares against nil.
		{`require "rspec"; expect(nil).to eq; puts "ok"`, "ok\n"},
		// match_array with a non-single-array argument list (varargs form).
		{`require "rspec"; expect([1, 2]).to match_array(1, 2); puts "ok"`, "ok\n"},
		// with(Integer) chaining.
		{`require "rspec"; expect("s").to respond_to(:upcase).with(0); puts "ok"`, "ok\n"},
		// respond_to sees a per-object singleton method.
		{`require "rspec"
o = Object.new
def o.custom_zzz; end
expect(o).to respond_to(:custom_zzz)
puts "ok"`, "ok\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
