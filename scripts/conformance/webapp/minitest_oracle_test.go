package webapp

import (
	"regexp"
	"testing"
)

// The minitest oracle proves the go-ruby-minitest binding is MRI-identical
// *through rbgo* (the public ruby.Run path): rbgo runs a real Minitest 5.x suite
// via `require "minitest/autorun"` and produces the gem's report — progress
// codes, the assert_equal diff message, the assert_raises + skip handling, the
// summary counts, the spec DSL — byte-for-byte identical to the captured output
// of the real `minitest` gem 5.25.5 (MRI ruby 4.0.5, run with the MINITEST5_LIB
// escape hatch the lib documents, since the 5.x gem does not install on ruby 4).
//
// Three lines are normalised because rbgo cannot make them deterministic: the
// random --seed (rbgo pins it to 0), the "Finished in …" timing, and the
// bracketed source location (rbgo carries no source line numbers). Everything
// else — every assertion message, every count — is asserted verbatim. The
// goldens are captured constants so the suite stays hermetic (no ruby, no gem;
// runs under qemu on every 64-bit target); regenerate by running
// apps/minitest_oracle.rb + apps/minitest_spec_oracle.rb under MRI + the minitest
// 5.25.5 gem and applying minitestNormalize.

var (
	mtSeedRE     = regexp.MustCompile(`--seed [0-9]+`)
	mtFinishedRE = regexp.MustCompile(`(?m)^Finished in .*$`)
	mtLocRE      = regexp.MustCompile(`\[[^\]]*\]`)
)

// minitestNormalize erases the three non-deterministic elements of a Minitest
// report so a byte comparison isolates the load-bearing content (the messages,
// codes and counts).
func minitestNormalize(s string) string {
	s = mtSeedRE.ReplaceAllString(s, "--seed SEED")
	s = mtFinishedRE.ReplaceAllString(s, "Finished in TIMING")
	s = mtLocRE.ReplaceAllString(s, "[LOCATION]")
	return s
}

// minitestAssertGolden is apps/minitest_oracle.rb's report from the real minitest
// gem 5.25.5, normalised. Progress ".F.S": test_addition passes, test_equal_diff
// fails with the diff message, test_raises_message passes, test_skipped skips.
const minitestAssertGolden = `Run options: --seed SEED

# Running:

.F.S

Finished in TIMING

  1) Failure:
CalcTest#test_equal_diff [LOCATION]:
Expected: 1
  Actual: 2

4 runs, 4 assertions, 1 failures, 0 errors, 1 skips

You have skipped tests. Run with --verbose for details.
`

// minitestSpecGolden is apps/minitest_spec_oracle.rb's report from the real
// minitest gem 5.25.5, normalised. Progress ".F": the "adds numbers" example
// passes, "detects mismatch" fails (must_equal → assert_equal diff), and the
// example names carry the gem's test_%04d_ morphing.
const minitestSpecGolden = `Run options: --seed SEED

# Running:

.F

Finished in TIMING

  1) Failure:
Calculator#test_0002_detects mismatch [LOCATION]:
Expected: 5
  Actual: 4

2 runs, 2 assertions, 1 failures, 0 errors, 0 skips
`

// TestMinitestGemOracle proves the assertion surface + runner + reporter are
// MRI-identical through rbgo: a real Minitest::Test suite (passing + failing
// assert_equal, assert_raises with a message check, and a skip) reproduces the
// minitest 5.25.5 report byte-for-byte after normalising the seed/timing/location.
func TestMinitestGemOracle(t *testing.T) {
	if ok, detail := featureLoadable("minitest"); !ok {
		t.Fatalf("minitest must be loadable for the oracle: %s", detail)
	}
	got := minitestNormalize(mustRun(t, "minitest_oracle.rb"))
	if got != minitestAssertGolden {
		t.Fatalf("rbgo output is not MRI-identical to minitest 5.25.5\n got:\n%s\nwant:\n%s", got, minitestAssertGolden)
	}
}

// TestMinitestSpecGemOracle proves the spec DSL (describe/it + the _/must_equal
// expectation surface) is MRI-identical through rbgo, byte-for-byte against
// minitest 5.25.5 after the same normalisation.
func TestMinitestSpecGemOracle(t *testing.T) {
	if ok, detail := featureLoadable("minitest"); !ok {
		t.Fatalf("minitest must be loadable for the oracle: %s", detail)
	}
	got := minitestNormalize(mustRun(t, "minitest_spec_oracle.rb"))
	if got != minitestSpecGolden {
		t.Fatalf("rbgo spec output is not MRI-identical to minitest 5.25.5\n got:\n%s\nwant:\n%s", got, minitestSpecGolden)
	}
}
