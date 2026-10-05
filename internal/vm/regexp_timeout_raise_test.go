// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// These tests cover go-embedded-ruby/ruby#776, a fail-open in Regexp matching:
// a match abandoned at one of the engine's limits answered "no match" instead of
// raising, so Regexp::TimeoutError could never be rescued and — for a Regexp used
// as a validator or a denylist — "timed out" was indistinguishable from "does not
// match". A crafted subject passed the check.
//
// TestTimedOutMatchRaisesRatherThanReturningNil is the core regression: every Ruby
// entry point that can run a match must raise Regexp::TimeoutError when the limit
// fires, not answer "no match". The old behaviour for each line below is in the
// comment: note that scan answered [], gsub/sub answered the subject unchanged,
// split answered [subject], and match?/=== answered FALSE — which is the sharpest
// form, because a denylist written as `reject if DENY.match?(input)` lets the
// subject straight through.
func TestTimedOutMatchRaisesRatherThanReturningNil(t *testing.T) {
	// Every entry point the collapse reached. A fix at one call site that leaves
	// scan fail-open is the half-fix this defect class invites, so they are all
	// here and they all go through the same funnel (Regexp.match / .matchAt /
	// .matchString in internal/vm/regexp.go).
	entryPoints := []struct {
		name string
		expr string
		was  string // what the old, fail-open code answered
	}{
		{"regexp_match_op", `R =~ S`, "nil"},
		{"string_match_op", `S =~ R`, "nil"},
		{"match", `R.match(S)`, "nil"},
		{"match_with_pos", `R.match(S, 0)`, "nil"},
		{"match_p", `R.match?(S)`, "false"},
		{"case_eq", `R === S`, "false"},
		{"scan", `S.scan(R)`, "[]"},
		{"gsub", `S.gsub(R, "x")`, "the subject unchanged"},
		{"gsub_block", `S.gsub(R) { "x" }`, "the subject unchanged"},
		{"gsub_hash", `S.gsub(R, "a" => "x")`, "the subject unchanged"},
		{"sub", `S.sub(R, "x")`, "the subject unchanged"},
		{"split", `S.split(R)`, "[subject]"},
		{"string_index", `S.index(R)`, "nil"},
		{"string_rindex", `S.rindex(R)`, "nil"},
		{"string_byteindex", `S.byteindex(R)`, "nil"},
		{"string_byterindex", `S.byterindex(R)`, "nil"},
		{"string_slice", `S[R]`, "nil"},
		{"string_start_with_p", `S.start_with?(R)`, "false"},
		{"string_partition", `S.partition(R)`, "[subject, \"\", \"\"]"},
		{"string_rpartition", `S.rpartition(R)`, "[\"\", \"\", subject]"},
		{"match_data_class", `S.match(R)`, "nil"},
	}

	for _, ep := range entryPoints {
		t.Run(ep.name, func(t *testing.T) {
			src := `R = Regexp.new("(a+)+\\1b", timeout: 0.05)
S = "a" * 26
begin
  ` + ep.expr + `
  puts "NO RAISE"
rescue Regexp::TimeoutError => e
  puts "#{e.class}: #{e.message}"
rescue => e
  puts "WRONG: #{e.class}: #{e.message}"
end`
			got := eval(t, src)
			const want = "Regexp::TimeoutError: regexp match timeout\n"
			if got != want {
				t.Errorf("%s answered %q (it used to answer %s), want %q",
					ep.expr, strings.TrimRight(got, "\n"), ep.was, strings.TrimRight(want, "\n"))
			}
		})
	}
}

// TestClassLevelTimeoutTakesEffect is defect 1: Regexp.timeout= was stored, read
// back faithfully, and never applied — it had a reader, a writer, and no consumer.
//
// The per-Regexp form is the CONTROL, exactly as the bug report measured it: it
// proves the probe works, so a class-level row that does not fire is the setter
// being ignored and not a probe that failed to arrive.
func TestClassLevelTimeoutTakesEffect(t *testing.T) {
	// A limit well under the ~0.85s at which the step budget is reached, so the
	// exception that arrives names the clock and not the budget. That distinction
	// is the whole assertion: before the fix the class-level row reached the BUDGET
	// at 0.83s and reported RegexpError, which is why the message is checked.
	const src = `S = "a" * 26
def probe
  yield
  "NO RAISE"
rescue Regexp::TimeoutError => e
  "TimeoutError"
rescue => e
  "#{e.class}"
end

# CONTROL: the per-Regexp option, which was already honoured.
Regexp.timeout = nil
per = Regexp.new("(a+)+\\1b", timeout: 0.05)
puts "control(per-regexp): #{probe { per =~ S }}"

# SUBJECT: the class-level setter, which was not.
Regexp.timeout = 0.05
cls = Regexp.new("(a+)+\\1b")
puts "subject(class-level): #{probe { cls =~ S }}"
puts "reads back: #{Regexp.timeout.inspect}"
puts "per-regexp reader unaffected: #{cls.timeout.inspect}"
Regexp.timeout = nil`

	got := eval(t, src)
	want := strings.Join([]string{
		"control(per-regexp): TimeoutError",
		"subject(class-level): TimeoutError",
		"reads back: 0.05",
		// MRI's Regexp#timeout reports only this Regexp's OWN limit and does not fall
		// back to the class default (ruby/ruby re.c:4776, rb_reg_timeout_get, tag v4.0.5;
		// measured on the installed ruby 4.0.5, where it reads nil with Regexp.timeout = 0.05 in force).
		"per-regexp reader unaffected: nil",
		"",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestTimeoutPrecedenceMatchesMRI pins the resolution rule, which is NOT "the
// tighter limit wins". ruby/ruby re.c:4688-4719 (rb_reg_timeout_p, tag
// v4.0.5) reads reg->timelimit first and falls back to the process-global
// rb_reg_match_time_limit only when it is zero, and re.c:3935-3942 (set_timeout)
// stores nil as zero — so `timeout: nil` means "unset, use the class default".
//
// Measured on ruby 4.0.5: class 0.05 with per-Regexp 1.0 fires at 1.000s, i.e.
// the LARGER per-Regexp value wins outright.
func TestTimeoutPrecedenceMatchesMRI(t *testing.T) {
	// A per-Regexp limit LARGER than the class-level one must win, so the match
	// survives past the class-level limit. Both values are kept well under the
	// ~0.85s step budget so the budget cannot decide the outcome instead.
	const src = `S = "a" * 26
def elapsed
  t = Process.clock_gettime(Process::CLOCK_MONOTONIC)
  begin
    yield
  rescue Regexp::TimeoutError
  end
  Process.clock_gettime(Process::CLOCK_MONOTONIC) - t
end

Regexp.timeout = 0.02
larger = Regexp.new("(a+)+\\1b", timeout: 0.30)
d = elapsed { larger =~ S }
# The per-Regexp 0.30 won if the match outlived the class-level 0.02.
puts "per-regexp overrides upwards: #{d > 0.10}"

Regexp.timeout = 0.30
smaller = Regexp.new("(a+)+\\1b", timeout: 0.02)
d = elapsed { smaller =~ S }
puts "per-regexp overrides downwards: #{d < 0.15}"

# timeout: nil is "unset", not "no limit": it falls back to the class default.
Regexp.timeout = 0.02
explicit_nil = Regexp.new("(a+)+\\1b", timeout: nil)
d = elapsed { explicit_nil =~ S }
puts "timeout: nil falls back to the class default: #{d < 0.15}"
puts "and its own reader still says nil: #{explicit_nil.timeout.inspect}"

# Regexp.timeout = nil clears the default.
Regexp.timeout = 0.02
Regexp.timeout = nil
puts "cleared: #{Regexp.timeout.inspect}"`

	got := eval(t, src)
	want := strings.Join([]string{
		"per-regexp overrides upwards: true",
		"per-regexp overrides downwards: true",
		"timeout: nil falls back to the class default: true",
		"and its own reader still says nil: nil",
		"cleared: nil",
		"",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestStepBudgetRaisesWithNoTimeoutConfigured is the budget half of the same
// defect, and the sharper half: the engine's DEFAULT step budget is reached on
// this pattern with no Regexp.timeout set anywhere, so the fail-open did not need
// a security setting to have been configured in order to be exploitable. The
// subject in the bug report's own row A — "a"*26 with no timeout, "nil after
// 0.994s" — was this, not an exhaustive search that found no match.
//
// MRI has no step budget, so there is no MRI exception to mirror. Its nearest
// analogue is Onigmo's match-stack limit, which reaches re.c:1724-1728's
// `default:` arm (tag v4.0.5) and raises a plain RegexpError — so that is what this raises.
// Rescuing Regexp::TimeoutError does NOT catch it; rescuing RegexpError catches
// both, which is what a guard should write.
func TestStepBudgetRaisesWithNoTimeoutConfigured(t *testing.T) {
	const src = `Regexp.timeout = nil
R = Regexp.new("(a+)+\\1b")
S = "a" * 26
puts "no limit configured: #{Regexp.timeout.inspect} / #{R.timeout.inspect}"
begin
  R =~ S
  puts "NO RAISE"
rescue Regexp::TimeoutError => e
  puts "TimeoutError (wrong: the clock did not run out)"
rescue RegexpError => e
  puts "#{e.class}: #{e.message}"
end
# It is a StandardError, so a bare rescue catches it.
begin
  R =~ S
rescue => e
  puts "bare rescue caught #{e.class}"
end`

	got := eval(t, src)
	want := strings.Join([]string{
		"no limit configured: nil / nil",
		"RegexpError: regexp match step budget exceeded",
		"bare rescue caught RegexpError",
		"",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestOrdinaryMatchNeverRaises is the negative control for all of the above, and
// it is what keeps the fix from being a new fail-CLOSED defect: with a limit set,
// an ordinary match must still answer rather than raise. /(a+)+b/ against "a"*40
// is included because it is the classic one-line ReDoS and does NOT reproduce
// here: the engine's (pc,sp) memoisation makes the RE2-compatible subset linear,
// so it decides in about a millisecond.
//
// Note that a limit small enough bounds EVERY match, including these: the
// capture-extracting paths always run on the backtracking VM (the lazy-NFA
// accelerator answers only the bounds-only question), so a 1-microsecond limit
// raises on anything at all, correctly. The limit here is a realistic one.
func TestOrdinaryMatchNeverRaises(t *testing.T) {
	const src = `Regexp.timeout = 1.0
puts (/(a+)+b/ =~ ("a" * 40)).inspect
puts (/(a+)+b/ =~ ("a" * 40 + "b")).inspect
puts "abc".scan(/\w/).inspect
puts "abc".gsub(/b/, "X")
puts "a,b,c".split(/,/).inspect
puts "abc".match?(/b/)
puts "abc".index(/c/)
Regexp.timeout = nil`

	got := eval(t, src)
	want := strings.Join([]string{
		"nil", "0", `["a", "b", "c"]`, "aXc", `["a", "b", "c"]`, "true", "2", "",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestInvalidTimeoutRaisesArgumentError covers the third thing the audit turned
// up: a non-nil, non-positive timeout was accepted silently. `Regexp.timeout = 0`
// returned 0 and read back as a configured limit that could never fire — a
// security setting that looks applied and is not. ruby/ruby re.c:3938-3940
// (set_timeout, tag v4.0.5) raises ArgumentError; measured on ruby 4.0.5 the message is
// "invalid timeout: 0".
func TestInvalidTimeoutRaisesArgumentError(t *testing.T) {
	for _, tc := range []struct{ name, expr, want string }{
		{"class_zero", `Regexp.timeout = 0`, "ArgumentError: invalid timeout: 0"},
		{"class_negative", `Regexp.timeout = -1`, "ArgumentError: invalid timeout: -1"},
		{"class_zero_float", `Regexp.timeout = 0.0`, "ArgumentError: invalid timeout: 0.0"},
		{"class_negative_float", `Regexp.timeout = -0.5`, "ArgumentError: invalid timeout: -0.5"},
		{"new_zero", `Regexp.new("a", timeout: 0)`, "ArgumentError: invalid timeout: 0"},
		{"new_negative", `Regexp.new("a", timeout: -2.5)`, "ArgumentError: invalid timeout: -2.5"},
		{"class_bad_type", `Regexp.timeout = "x"`, "TypeError: no implicit conversion to float from String"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "begin\n  " + tc.expr + "\n  puts \"NO RAISE\"\nrescue => e\n  puts \"#{e.class}: #{e.message}\"\nend"
			if got := eval(t, src); got != tc.want+"\n" {
				t.Errorf("%s raised %q, want %q", tc.expr, strings.TrimRight(got, "\n"), tc.want)
			}
		})
	}

	// A positive value is still accepted and read back.
	if got := eval(t, "Regexp.timeout = 0.5\np Regexp.timeout\nRegexp.timeout = 1\np Regexp.timeout\nRegexp.timeout = nil\np Regexp.timeout"); got != "0.5\n1.0\nnil\n" {
		t.Errorf("valid timeouts: got %q, want %q", got, "0.5\n1.0\nnil\n")
	}
}

// TestTimeoutErrorIsRescuableAsRegexpError pins the ancestry a caller writes a
// rescue against: Regexp::TimeoutError < RegexpError < StandardError
// (ruby/ruby re.c:4862 at tag v4.0.5,
// rb_define_class_under(rb_cRegexp, "TimeoutError", rb_eRegexpError)). Rescuing RegexpError catches BOTH limits; that is the form a
// guard should use, since the step budget raises the parent.
func TestTimeoutErrorIsRescuableAsRegexpError(t *testing.T) {
	const src = `p Regexp::TimeoutError.ancestors[0, 4]
R = Regexp.new("(a+)+\\1b", timeout: 0.05)
S = "a" * 26
begin
  R.match?(S)
rescue RegexpError => e
  puts "rescued as RegexpError: #{e.class}"
end
begin
  R.match?(S)
rescue => e
  puts "rescued bare: #{e.class}"
end`

	got := eval(t, src)
	want := strings.Join([]string{
		"[Regexp::TimeoutError, RegexpError, StandardError, Exception]",
		"rescued as RegexpError: Regexp::TimeoutError",
		"rescued bare: Regexp::TimeoutError",
		"",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestAnchoredPathHonoursTheTimeout is the regression for the gap the audit found
// underneath this one: the engine's anchored match paths carried no deadline at
// all (they called the budget-only VM entry point), so String#rindex — which
// probes one start position after another — ran 3.593s under a 0.05s limit, 72x
// over. The bound here is deliberately loose so it measures the fix, not the
// machine.
func TestAnchoredPathHonoursTheTimeout(t *testing.T) {
	const src = `R = Regexp.new("(a+)+\\1b", timeout: 0.05)
S = "a" * 26
t = Process.clock_gettime(Process::CLOCK_MONOTONIC)
begin
  S.rindex(R)
  puts "NO RAISE"
rescue Regexp::TimeoutError
  d = Process.clock_gettime(Process::CLOCK_MONOTONIC) - t
  puts "raised, bounded: #{d < 1.0}"
end`

	if got := eval(t, src); got != "raised, bounded: true\n" {
		t.Errorf("rindex under a 0.05s limit: got %q, want %q", strings.TrimRight(got, "\n"), "raised, bounded: true")
	}
}
