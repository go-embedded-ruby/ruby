// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestRangeConstructionCompare covers vm.newRange — the construction-time
// begin <=> end comparison MRI performs and some specs observe. Verified against
// ruby 4.0.6.
//
//   - Range.new / Range#initialize are strict: an unordered pair (#<=> gives nil
//     and the bounds are not #==) is a "bad value for range" ArgumentError, while
//     a pair that is #== but has no #<=> ordering (two Regexp literals, whose only
//     comparison is Object's identity default) still builds — mirroring MRI's
//     Object#<=>, which answers 0 for #== objects.
//   - The range literal is not strict: it performs the comparison (a mocked #<=>
//     is dispatched) but tolerates an unordered result, so a (1.."z") literal
//     still builds.
//   - Two Integer bounds and a nil bound skip the comparison entirely.
func TestRangeConstructionCompare(t *testing.T) {
	cases := []struct{ src, want string }{
		// Strict path (Range.new): a real ordering builds.
		{`p Range.new("a", "c").to_a`, `["a", "b", "c"]`},
		// Strict path: two #==-but-unordered bounds (identical Regexps) build,
		// because Object#<=> answers 0 for #== objects — the #== fallback.
		{`p Range.new(//, //).class`, `Range`},
		{`p Range.new(//, //).begin`, `//`},
		// Strict path: an unordered, non-#== pair is a bad value for range.
		{`begin; Range.new(1, Object.new); rescue ArgumentError => e; p e.message; end`, `"bad value for range"`},
		// Range#initialize shares the strict path.
		{`class MyR < Range; end
begin; MyR.new(1, Object.new); rescue ArgumentError => e; p e.message; end`, `"bad value for range"`},
		// Two Integer bounds skip the comparison (Fixnum fast path).
		{`p (1..3).to_a`, `[1, 2, 3]`},
		// A nil bound skips the comparison (beginless / endless).
		{`p (1..).begin`, `1`},
		{`p (..3).end`, `3`},
		// The literal is not strict: an unordered literal still builds and is usable.
		{`p (1.."z").exclude_end?`, `false`},
		// The literal performs the #<=> dispatch: a mocked begin sees it.
		{`class M
  def initialize; @n = 0; end
  def hits; @n; end
  def <=>(o); @n += 1; 0; end
end
m = M.new
r = (m..m)
p m.hits`, `1`},
		// Two identical Regexp literals build (the literal tolerates the nil #<=>).
		{`p (//..//).class`, `Range`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestRangeStepCoerce covers vm.coerceRangeStep — MRI 4.0's Range#step coercion of
// a non-numeric step against the range's begin. A numeric step is used as-is; a
// step that answers #coerce is replaced by the numeric [begin', step'] pair it
// returns; a step with no #coerce, or a #coerce that returns something other than
// a two-element pair, is a TypeError. Verified against ruby 4.0.6.
func TestRangeStepCoerce(t *testing.T) {
	// Numeric step: used unchanged.
	if got := eval(t, `a = []; (1..3).step(2) { |x| a << x }; p a`); got != "[1, 3]\n" {
		t.Errorf("numeric step: got %q", got)
	}
	// A #coerce-able step is coerced against begin: obj.coerce(1) => [1, 2] walks 1, 3.
	if got := eval(t, `class C; def coerce(x); [x, 2]; end; end
a = []; (1..3).step(C.new) { |x| a << x }; p a`); got != "[1, 3]\n" {
		t.Errorf("coerce step: got %q", got)
	}
	// A step with no #coerce is a TypeError.
	if err := runErr(t, `(1..2).step(Object.new) { |x| x }`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("non-coercible step: got err=%v, want TypeError", err)
	}
	// A #coerce that returns a wrong-length pair is a TypeError.
	if err := runErr(t, `class C1; def coerce(x); [1]; end; end
(1..3).step(C1.new) { |x| x }`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("coerce wrong length: got err=%v, want TypeError", err)
	}
	// A #coerce that returns a non-Array is a TypeError.
	if err := runErr(t, `class C2; def coerce(x); 5; end; end
(1..3).step(C2.new) { |x| x }`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("coerce non-array: got err=%v, want TypeError", err)
	}
}
