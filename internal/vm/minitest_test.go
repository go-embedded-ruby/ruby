// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// mtPrelude sets up an object that mixes in Minitest::Assertions plus a `c`
// helper that runs a block and prints "<label><PASS>" on success or
// "<label><message>" when a Minitest::Assertion is raised, so a test can assert
// the byte-exact failure message.
const mtPrelude = `require "minitest"
class T; include Minitest::Assertions; end
$t = T.new
def c(l); begin; yield; puts "#{l}<PASS>"; rescue Minitest::Assertion => e; puts "#{l}<#{e.message}>"; end; end
`

// TestMinitestFeature covers the require probe and the module/exception-tree shape.
func TestMinitestFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "minitest"`, "true\n"},
		{`require "minitest"; p require "minitest"`, "false\n"},
		{`require "minitest"; p Minitest.is_a?(Module)`, "true\n"},
		{`require "minitest"; p Minitest::Assertions.is_a?(Module)`, "true\n"},
		{`require "minitest"; p(Minitest::Assertion < Exception)`, "true\n"},
		{`require "minitest"; p(Minitest::Skip < Minitest::Assertion)`, "true\n"},
		{`require "minitest"; p(Minitest::UnexpectedError < Minitest::Assertion)`, "true\n"},
		{`require "minitest"; p(MockExpectationError < StandardError)`, "true\n"},
		{`require "minitest"; p Minitest::Test.is_a?(Class)`, "true\n"},
		{`require "minitest"; p Minitest::Mock.is_a?(Class)`, "true\n"},
		{`require "minitest"; p Minitest::Result.is_a?(Class)`, "true\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestMinitestPassing covers the success (nil-error) arm of a representative
// spread of assertions, and that they return true and bump the count.
func TestMinitestPassing(t *testing.T) {
	src := mtPrelude + `
p $t.assert(1)
p $t.refute(nil)
p $t.assert_equal(1, 1)
p $t.refute_equal(1, 2)
p $t.assert_nil(nil)
p $t.refute_nil(1)
p $t.assert_empty([])
p $t.refute_empty([1])
p $t.assert_includes([1, 2], 2)
p $t.refute_includes([1, 2], 3)
p $t.assert_instance_of(Integer, 5)
p $t.refute_instance_of(String, 5)
p $t.assert_kind_of(Numeric, 5)
p $t.refute_kind_of(String, 5)
p $t.assert_respond_to(5, :to_i)
p $t.refute_respond_to(5, :nope)
p $t.assert_match(/fo/, "foo")
p $t.assert_match("fo", "foo")
p $t.refute_match(/x/, "foo")
p $t.assert_operator(2, :>, 1)
p $t.refute_operator(1, :>, 2)
p $t.assert_predicate(0, :zero?)
p $t.refute_predicate(1, :zero?)
p $t.assert_in_delta(1.0, 1.0)
p $t.refute_in_delta(1.0, 2.0, 0.5)
p $t.assert_in_epsilon(1.0, 1.0)
p $t.refute_in_epsilon(1.0, 2.0, 0.1)
p $t.pass
p $t.assertions
`
	// assert_empty/includes/match (and their refute twins) each also assert the
	// respond_to precondition, so they bump the count twice: 21 single-count
	// assertions + 7 double-count ones = 35.
	want := strings.Repeat("true\n", 28) + "35\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestMinitestFailureMessages covers the failure arm of each assertion and the
// byte-exact message the library produces (matching the minitest gem 5.x).
func TestMinitestFailureMessages(t *testing.T) {
	src := mtPrelude + `
c("EQ"){ $t.assert_equal 1, 2 }
c("EQMSG"){ $t.assert_equal 1, 2, "ctx" }
c("REQ"){ $t.refute_equal 1, 1 }
c("ASSERT"){ $t.assert nil }
c("REFUTE"){ $t.refute 1 }
c("NIL"){ $t.assert_nil 5 }
c("RNIL"){ $t.refute_nil nil }
c("EMPTY"){ $t.assert_empty [1] }
c("REMPTY"){ $t.refute_empty [] }
c("INC"){ $t.assert_includes [1,2], 3 }
c("RINC"){ $t.refute_includes [1], 1 }
c("INST"){ $t.assert_instance_of String, 5 }
c("RINST"){ $t.refute_instance_of Integer, 5 }
c("KIND"){ $t.assert_kind_of String, 5 }
c("RKIND"){ $t.refute_kind_of Integer, 5 }
c("RESP"){ $t.assert_respond_to 5, :nope }
c("RRESP"){ $t.refute_respond_to 5, :to_i }
c("MATCH"){ $t.assert_match(/foo/, "bar") }
c("RMATCH"){ $t.refute_match(/f/, "f") }
c("OP"){ $t.assert_operator 1, :>, 2 }
c("ROP"){ $t.refute_operator 2, :>, 1 }
c("PRED"){ $t.assert_predicate 1, :zero? }
c("RPRED"){ $t.refute_predicate 0, :zero? }
c("OPPRED"){ $t.assert_operator 1, :zero? }
c("DELTA"){ $t.assert_in_delta 1.0, 2.0, 0.5 }
c("RDELTA"){ $t.refute_in_delta 1.0, 1.0, 0.5 }
c("EPS"){ $t.assert_in_epsilon 1.0, 2.0, 0.1 }
c("REPS"){ $t.refute_in_epsilon 1.0, 1.0, 0.1 }
c("FLUNK"){ $t.flunk }
c("FLUNKMSG"){ $t.flunk "boom" }
`
	want := strings.Join([]string{
		"EQ<Expected: 1\n  Actual: 2>",
		"EQMSG<ctx.\nExpected: 1\n  Actual: 2>",
		"REQ<Expected 1 to not be equal to 1.>",
		"ASSERT<Expected nil to be truthy.>",
		"REFUTE<Expected 1 to not be truthy.>",
		"NIL<Expected 5 to be nil.>",
		"RNIL<Expected nil to not be nil.>",
		"EMPTY<Expected [1] to be empty.>",
		"REMPTY<Expected [] to not be empty.>",
		"INC<Expected [1, 2] to include 3.>",
		"RINC<Expected [1] to not include 1.>",
		"INST<Expected 5 to be an instance of String, not Integer.>",
		"RINST<Expected 5 to not be an instance of Integer.>",
		"KIND<Expected 5 to be a kind of String, not Integer.>",
		"RKIND<Expected 5 to not be a kind of Integer.>",
		"RESP<Expected 5 (Integer) to respond to #nope.>",
		"RRESP<Expected 5 to not respond to to_i.>",
		"MATCH<Expected /foo/ to match \"bar\".>",
		"RMATCH<Expected /f/ to not match \"f\".>",
		"OP<Expected 1 to be > 2.>",
		"ROP<Expected 2 to not be > 1.>",
		"PRED<Expected 1 to be zero?.>",
		"RPRED<Expected 0 to not be zero?.>",
		"OPPRED<Expected 1 to be zero?.>",
		"DELTA<Expected |1.0 - 2.0| (1.0) to be <= 0.5.>",
		"RDELTA<Expected |1.0 - 1.0| (0.0) to not be <= 0.5.>",
		"EPS<Expected |1.0 - 2.0| (1.0) to be <= 0.1.>",
		"REPS<Expected |1.0 - 1.0| (0.0) to not be <= 0.1.>",
		"FLUNK<Epic Fail!>",
		"FLUNKMSG<boom>",
		"",
	}, "\n")
	if got := eval(t, src); got != want {
		t.Errorf("got=\n%q\nwant=\n%q", got, want)
	}
}

// TestMinitestCustomMessageForms covers the optional trailing-message argument in
// its String, Proc, nil and other-value forms (minitestMsg's branches).
func TestMinitestCustomMessageForms(t *testing.T) {
	src := mtPrelude + `
c("STR"){ $t.assert_equal 1, 2, "str" }
c("PROC"){ $t.assert_nil 5, proc { "lazy" } }
c("NILM"){ $t.assert_nil 5, nil }
c("OTHER"){ $t.assert_nil 5, 42 }
`
	want := strings.Join([]string{
		"STR<str.\nExpected: 1\n  Actual: 2>",
		"PROC<lazy.\nExpected 5 to be nil.>",
		"NILM<Expected 5 to be nil.>",
		"OTHER<42.\nExpected 5 to be nil.>",
		"",
	}, "\n")
	if got := eval(t, src); got != want {
		t.Errorf("got=\n%q\nwant=\n%q", got, want)
	}
}

// TestMinitestSameAndDelta covers assert_same's oid message shape and the
// non-numeric delta coercion path.
func TestMinitestSameAndDelta(t *testing.T) {
	src := mtPrelude + `
a = "x"
p $t.assert_same(a, a)
c("SAME"){ $t.assert_same "x", "y" }
p($t.assert_in_delta(nil, nil))
`
	got := eval(t, src)
	lines := strings.Split(got, "\n")
	if len(lines) < 3 || lines[0] != "true" {
		t.Fatalf("assert_same pass: got %q", got)
	}
	if !strings.Contains(lines[1], "to be the same as") {
		t.Errorf("assert_same fail message: got %q", lines[1])
	}
	if lines[2] != "true" {
		t.Errorf("assert_in_delta(nil,nil): got %q", lines[2])
	}
}

// TestMinitestSkip covers skip raising Minitest::Skip with the default and custom
// messages, without bumping the assertion count.
func TestMinitestSkip(t *testing.T) {
	src := mtPrelude + `
begin; $t.skip; rescue Minitest::Skip => e; puts "D<#{e.message}>"; end
begin; $t.skip "later"; rescue Minitest::Skip => e; puts "M<#{e.message}>"; end
p $t.assertions
`
	want := "D<Skipped, no message given>\nM<later>\n0\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestMinitestAssertRaises covers assert_raises: a matched raise returns the
// caught exception, a clean block and a wrong class flunk with the gem's message,
// and a missing block flunks.
func TestMinitestAssertRaises(t *testing.T) {
	src := mtPrelude + `
e = $t.assert_raises(ArgumentError) { raise ArgumentError, "boom" }
puts "M<#{e.class}:#{e.message}>"
e2 = $t.assert_raises(RuntimeError, ArgumentError) { raise "plain" }
puts "M2<#{e2.class}>"
d = $t.assert_raises { raise "default" }
puts "D<#{d.class}>"
c("NONE"){ $t.assert_raises(ArgumentError) { 1 } }
c("WRONG"){ $t.assert_raises(TypeError) { raise ArgumentError, "x" } }
c("NOBLK"){ $t.assert_raises(ArgumentError) }
`
	want := strings.Join([]string{
		"M<ArgumentError:boom>",
		"M2<RuntimeError>",
		"D<RuntimeError>",
		"NONE<ArgumentError expected but nothing was raised.>",
		"WRONG<[TypeError] exception expected, not\nClass: <ArgumentError>\nMessage: <\"x\">\n---Backtrace---\n\n--------------->",
		"NOBLK<assert_raises requires a block to capture errors.>",
		"",
	}, "\n")
	if got := eval(t, src); got != want {
		t.Errorf("got=\n%q\nwant=\n%q", got, want)
	}
}

// TestMinitestAssertRaisesReRaise covers the passthrough re-raise: a
// Minitest::Assertion raised inside the block is not caught by assert_raises but
// propagates (it is an assertion, not a matchable error), and its return value on
// a raise carried by a native binding (nil-Obj) is rebuilt.
func TestMinitestAssertRaisesReRaise(t *testing.T) {
	src := mtPrelude + `
begin
  $t.assert_raises(StandardError) { $t.flunk "inner" }
rescue Minitest::Assertion => e
  puts "RERAISE<#{e.message}>"
end
m = Minitest::Mock.new
m.expect(:foo, 1, ["a"])
e = $t.assert_raises(MockExpectationError) { m.verify }
puts "NILOBJ<#{e.class}:#{e.message}>"
`
	want := "RERAISE<inner>\nNILOBJ<MockExpectationError:Expected foo(\"a\") => 1>\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestMinitestLifecycle covers Minitest::Test#run: the setup→body flow, and the
// four result kinds (pass / failure / error / skip) with their codes, predicates,
// assertion counts, failure messages and to_s rendering.
func TestMinitestLifecycle(t *testing.T) {
	src := `require "minitest"
class MyTest < Minitest::Test
  def setup; @x = 40; end
  def test_pass; assert_equal 42, @x + 2; end
  def test_fail; assert_equal 1, 2; end
  def test_error; raise "kaboom"; end
  def test_skip; skip "nope"; end
end
%w[test_pass test_fail test_error test_skip].each do |m|
  r = MyTest.new(m).run
  puts "#{m} #{r.result_code} p=#{r.passed?} s=#{r.skipped?} e=#{r.error?} a=#{r.assertions} n=#{r.name}"
end
puts MyTest.new("test_fail").run.failures.inspect
puts MyTest.new("test_fail").run.to_s
`
	want := strings.Join([]string{
		"test_pass . p=true s=false e=false a=1 n=test_pass",
		"test_fail F p=false s=false e=false a=1 n=test_fail",
		"test_error E p=false s=false e=true a=0 n=test_error",
		"test_skip S p=false s=true e=false a=0 n=test_skip",
		`["Expected: 1\n  Actual: 2"]`,
		"Failure:",
		"MyTest#test_fail [unknown:-1]:",
		"Expected: 1",
		"  Actual: 2",
		"",
	}, "\n")
	if got := eval(t, src); got != want {
		t.Errorf("got=\n%q\nwant=\n%q", got, want)
	}
}

// TestMinitestLifecyclePassthrough covers a SystemExit raised in a test body
// being classified as a passthrough (the run aborts rather than recording a
// failure) and Minitest::Test.new with no name.
func TestMinitestLifecyclePassthrough(t *testing.T) {
	src := `require "minitest"
class ET < Minitest::Test
  def test_exit; exit; end
end
r = ET.new("test_exit").run
puts "#{r.result_code} #{r.assertions}"
p Minitest::Test.new.name
`
	want := ". 0\n\"\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestMinitestMock covers Minitest::Mock: expect (positional args + return),
// method routing, verify success, the block-validated form, and the empty-args
// (nil array) form.
func TestMinitestMock(t *testing.T) {
	src := `require "minitest"
m = Minitest::Mock.new
m.expect(:foo, 99, [1, 2])
p m.foo(1, 2)
p m.verify
m2 = Minitest::Mock.new
m2.expect(:bar, true) { |x| x > 5 }
p m2.bar(10)
m3 = Minitest::Mock.new
m3.expect(:baz, :ok, nil)
p m3.baz
p m3.verify
m4 = Minitest::Mock.new
m4.expect(:noret)
p m4.noret
`
	want := "99\ntrue\ntrue\n:ok\ntrue\nnil\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestMinitestMockErrors covers the mock error classes: an unmet verify and a
// no-more-expects call (MockExpectationError), an arity mismatch (ArgumentError),
// an unmocked method (NoMethodError), an unexpected-argument mismatch (the ==
// fallback in the matcher), and expect's non-array args guard.
func TestMinitestMockErrors(t *testing.T) {
	src := `require "minitest"
def r(l); begin; yield; rescue => e; puts "#{l}<#{e.class}:#{e.message}>"; end; end
m = Minitest::Mock.new
m.expect(:bar, :ok)
r("VERIFY"){ m.verify }
m2 = Minitest::Mock.new
m2.expect(:z, 1, [1])
r("ARITY"){ m2.z(1, 2) }
m3 = Minitest::Mock.new
m3.expect(:only, 1)
r("NOMETH"){ m3.other }
m4 = Minitest::Mock.new
m4.expect(:mm, 1, [1])
r("ARGS"){ m4.mm(2) }
m5 = Minitest::Mock.new
m5.expect(:once, 1)
m5.once
r("NOMORE"){ m5.once }
m6 = Minitest::Mock.new
r("NOTARR"){ m6.expect(:bad, 1, "nope") }
`
	want := strings.Join([]string{
		"VERIFY<MockExpectationError:Expected bar() => :ok>",
		"ARITY<ArgumentError:mocked method :z expects 1 arguments, got [1, 2]>",
		"NOMETH<NoMethodError:unmocked method :other, expected one of [:only]>",
		"ARGS<MockExpectationError:mocked method :mm called with unexpected arguments [2]>",
		"NOMORE<MockExpectationError:No more expects available for :once: [] {}>",
		"NOTARR<ArgumentError:args must be an array>",
		"",
	}, "\n")
	if got := eval(t, src); got != want {
		t.Errorf("got=\n%q\nwant=\n%q", got, want)
	}
}
