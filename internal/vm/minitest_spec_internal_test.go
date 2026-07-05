// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"strings"
	"testing"

	minitest "github.com/go-ruby-minitest/minitest"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// specSuite wraps a spec/test body in require "minitest/autorun" and returns the
// full autorun report.
func specSuite(t *testing.T, body string) string {
	t.Helper()
	return eval(t, "require \"minitest/autorun\"\n"+body)
}

// TestMinitestSpecConstants covers the Spec class tree and the autorun/spec
// feature loads (requiring autorun installs the at_exit reporter, so those
// scripts also emit an empty report — hence the substring checks).
func TestMinitestSpecConstants(t *testing.T) {
	if got := eval(t, `require "minitest"; p Minitest::Spec < Minitest::Test`); got != "true\n" {
		t.Errorf("Spec < Test got=%q", got)
	}
	for _, feat := range []string{"minitest/autorun", "minitest/spec"} {
		if got := eval(t, `p require `+`"`+feat+`"`); !strings.HasPrefix(got, "true\n") {
			t.Errorf("require %q got=%q", feat, got)
		}
	}
}

// TestMinitestClassicRun covers the runnable registry + reporter for a classic
// Minitest::Test subclass: a pass, an assert_equal failure (its diff), an
// assert_raises, a skip, and an error, with the correct progress + counts.
func TestMinitestClassicRun(t *testing.T) {
	out := specSuite(t, `
class Broad < Minitest::Test
  def test_a_pass; assert_equal 2, 1 + 1; end
  def test_b_fail; assert_equal 1, 2; end
  def test_c_raise; assert_raises(RuntimeError) { raise "x" }; end
  def test_d_error; raise "boom"; end
  def test_e_skip; skip "later"; end
end`)
	if !strings.Contains(out, "Expected: 1\n  Actual: 2") {
		t.Errorf("diff message missing:\n%s", out)
	}
	if !strings.Contains(out, "RuntimeError: boom") {
		t.Errorf("error message missing:\n%s", out)
	}
	if !strings.Contains(out, "5 runs, 3 assertions, 1 failures, 1 errors, 1 skips") {
		t.Errorf("counts wrong:\n%s", out)
	}
	if !strings.Contains(out, "You have skipped tests") {
		t.Errorf("skip note missing:\n%s", out)
	}
}

// TestMinitestOrderingAndHooks covers the accepted ordering no-ops
// (i_suck_and_my_tests_are_order_dependent! etc.), a no-block before/after, and an
// after hook running through teardown.
func TestMinitestOrderingAndHooks(t *testing.T) {
	out := specSuite(t, `
Minitest::Test.i_suck_and_my_tests_are_order_dependent!
Minitest::Test.make_my_diffs_pretty!
Minitest::Test.parallelize_me!

describe "hooks" do
  before          # no block: ignored
  after           # no block: ignored
  before { @a = 1 }
  after { @a = nil }
  it "sees before" do
    _(@a).must_equal 1
  end
end`)
	if !strings.Contains(out, "1 runs, 1 assertions, 0 failures, 0 errors, 0 skips") {
		t.Errorf("ordering/hooks suite wrong:\n%s", out)
	}
}

// TestMinitestReportPassClean covers the reporter's all-passing path (no numbered
// section, no skip note) and Minitest.run's boolean.
func TestMinitestReportPassClean(t *testing.T) {
	out := eval(t, `require "minitest"
class Clean < Minitest::Test
  def test_ok; assert true; end
end
p Minitest.run`)
	if !strings.Contains(out, "1 runs, 1 assertions, 0 failures, 0 errors, 0 skips") {
		t.Errorf("pass-clean counts wrong:\n%s", out)
	}
	if strings.Contains(out, "You have skipped tests") || strings.Contains(out, "1) ") {
		t.Errorf("pass-clean produced a failure/skip section:\n%s", out)
	}
	if !strings.Contains(out, "true") {
		t.Errorf("Minitest.run should return true:\n%s", out)
	}
}

// TestMinitestSpecDSLFull covers describe/it, before/let (memoised), the
// must_*/wont_* value + block flips, and the no-block it skip stub.
func TestMinitestSpecDSLFull(t *testing.T) {
	out := specSuite(t, `
describe "Calc" do
  before { @base = 10 }
  before { @extra = 5 }
  let(:doubled) { @base * 2 }

  it "adds" do
    _(2 + 3).must_equal 5
    _(2 + 2).wont_equal 5
  end

  it "uses let and before" do
    _(doubled).must_equal 20
    _(doubled).must_equal 20 # memoised second read
    _(@extra).must_equal 5
  end

  it "raises" do
    _(-> { raise "x" }).must_raise RuntimeError
  end

  it "fails a must" do
    _(1).must_equal 2
  end

  it "pending"
end`)
	if !strings.Contains(out, "Calc#test_0004_fails a must") {
		t.Errorf("spec failure location missing:\n%s", out)
	}
	if !strings.Contains(out, "5 runs, 7 assertions, 1 failures, 0 errors, 1 skips") {
		t.Errorf("spec counts wrong:\n%s", out)
	}
}

// TestMinitestExpectationWrapForms covers the _ block form, the bare-self form,
// and a non-callable must_raise target (an Error result).
func TestMinitestExpectationWrapForms(t *testing.T) {
	out := specSuite(t, `
describe "arms" do
  it "block and self" do
    _ { raise "x" }.must_raise RuntimeError
    assert _().is_a?(Minitest::Spec)
  end
end`)
	if !strings.Contains(out, "1 runs, 2 assertions, 0 failures") {
		t.Errorf("wrap forms wrong:\n%s", out)
	}

	bad := specSuite(t, `describe("x") { it("bad") { _(5).must_raise RuntimeError } }`)
	if !strings.Contains(bad, "ArgumentError: must_raise expects a callable target") {
		t.Errorf("non-callable must_raise not reported:\n%s", bad)
	}
}

// TestMinitestLetInvalidName covers let's name validation.
func TestMinitestLetInvalidName(t *testing.T) {
	class, msg := evalErr(t, `require "minitest/autorun"
describe("X") { let(:test_bad) { 1 } }`)
	if class != "ArgumentError" || !strings.Contains(msg, "cannot begin with 'test'") {
		t.Errorf("let invalid name -> %s: %s", class, msg)
	}
}

// TestMinitestExpectationOutsideRun covers the must_* guard when no test is
// running (minitestCurInstance is nil).
func TestMinitestExpectationOutsideRun(t *testing.T) {
	class, msg := evalErr(t, `require "minitest"; _(1).must_equal(1)`)
	if class != "RuntimeError" || !strings.Contains(msg, "outside of a Minitest run") {
		t.Errorf("must outside run -> %s: %s", class, msg)
	}
}

// TestMinitestSpecCoverageEdges covers the Go-only arms: minitestRunOne's
// defensive non-Result path, the autorun double-run guard, minitestToRuby's nil /
// non-object arms, minitestDescribe with no block, and the ProcBox protocol.
func TestMinitestSpecCoverageEdges(t *testing.T) {
	// minitestRunOne defensive path: a class whose #run does not return a Result.
	vm := New(io.Discard)
	weird := newClass("WeirdRun", vm.consts["Minitest::Test"].(*RClass))
	weird.define("initialize", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return object.NilV })
	weird.define("run", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.NilV })
	if res := vm.minitestRunOne(weird, "test_x"); res == nil || res.TestName != "test_x" || res.Klass != "WeirdRun" {
		t.Errorf("defensive run result -> %+v", res)
	}

	// Autorun at_exit block is idempotent.
	var buf strings.Builder
	vm2 := New(&buf)
	vm2.minitestInstallAutorun()
	hook := vm2.atExit[len(vm2.atExit)-1]
	hook.native(vm2, nil)
	if !vm2.minitestAutorunDone {
		t.Error("autorun did not mark itself done")
	}
	first := buf.String()
	hook.native(vm2, nil)
	if buf.String() != first {
		t.Error("autorun ran a second time")
	}

	// minitestToRuby: nil placeholder, a real value, and a non-object.Value.
	got := minitestToRuby([]minitest.Value{nil, object.Integer(1), "raw"})
	if !object.IsNil(got[0]) || got[1] != object.Value(object.Integer(1)) || !object.IsNil(got[2]) {
		t.Errorf("minitestToRuby -> %v", got)
	}

	// minitestDescribe with a nil block still builds and registers a spec class.
	before := len(vm.minitestRunnables)
	cls := vm.minitestDescribe([]object.Value{object.NewString("Empty")}, nil, vm.cMinitestSpec)
	if _, ok := cls.(*RClass); !ok || len(vm.minitestRunnables) != before+1 {
		t.Errorf("describe(no block) -> %T", cls)
	}

	// ProcBox protocol.
	pb := &minitestProcBox{}
	if pb.ToS() == "" || pb.Inspect() == "" || !pb.Truthy() {
		t.Error("minitestProcBox protocol")
	}
}
