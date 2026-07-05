# Minitest 5.25.5 spec-DSL conformance fixture for rbgo.
#
# A single describe block (one runnable) with a passing and a failing example,
# using the _(actual).must_equal(expected) expectation surface. Run through rbgo
# it must reproduce the real minitest 5.25.5 report byte-for-byte, modulo the
# seed / timing / source-location lines. See minitest_oracle_test.go.
require "minitest/autorun"
Minitest::Test.i_suck_and_my_tests_are_order_dependent!

describe "Calculator" do
  it "adds numbers" do
    _(2 + 3).must_equal 5
  end

  it "detects mismatch" do
    _(2 + 2).must_equal 5
  end
end
