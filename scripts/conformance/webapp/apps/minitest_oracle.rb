# Minitest 5.25.5 assertion / runner conformance fixture for rbgo.
#
# A single Minitest::Test class (one runnable, so the run order is deterministic)
# exercising a passing assertion, a failing assert_equal (its diff message), an
# assert_raises with a message check, and a skip. Run through rbgo it must
# reproduce the real minitest 5.25.5 report byte-for-byte, modulo the three lines
# rbgo cannot make deterministic (the seed, the timing, and the source-location
# line — rbgo carries no source line numbers). See minitest_oracle_test.go.
#
# i_suck_and_my_tests_are_order_dependent! pins the gem to alphabetical order (a
# no-op under rbgo, which is always alphabetical) so the oracle is reproducible.
require "minitest/autorun"
Minitest::Test.i_suck_and_my_tests_are_order_dependent!

class CalcTest < Minitest::Test
  def test_addition
    assert_equal 4, 2 + 2
  end

  def test_equal_diff
    assert_equal 1, 2
  end

  def test_raises_message
    err = assert_raises(RuntimeError) { raise "boom" }
    assert_equal "boom", err.message
  end

  def test_skipped
    skip "pending feature"
  end
end
