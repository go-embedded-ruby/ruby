package vm_test

import "testing"

// TestRegexpLiteralSameOccurrenceIdentity pins the core "once literal" property:
// a non-interpolated /…/ evaluated repeatedly (here in a block yielded several
// times) returns the SAME frozen object each time — it is compiled once and
// memoised on the occurrence, not recompiled per evaluation (the perf fix).
func TestRegexpLiteralSameOccurrenceIdentity(t *testing.T) {
	src := `
seen = []
3.times { seen << /\d+/i }
p seen[0].equal?(seen[1])
p seen[1].equal?(seen[2])
p seen[0].frozen?
`
	if got, want := eval(t, src), "true\ntrue\ntrue\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRegexpLiteralDistinctOccurrences: two textually-identical literals written
// at different source locations are separate objects (per-occurrence memoisation,
// matching MRI), even though they are #== equal by source+flags.
func TestRegexpLiteralDistinctOccurrences(t *testing.T) {
	src := `
a = /foo/
b = /foo/
p a.equal?(b)
p a == b
`
	if got, want := eval(t, src), "false\ntrue\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRegexpLiteralMatchingUnchanged checks the observable behaviour is identical
// to before caching: =~, scan, match?, and the i/m/x flags all still work.
func TestRegexpLiteralMatchingUnchanged(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p(/abc/ =~ "xxabc")`, "2\n"},
		{`p(/ABC/i =~ "xabc")`, "1\n"},    // i: case-insensitive
		{`p("a\nb" =~ /^b/m)`, "2\n"},     // m: dot/anchors over newlines
		{`p(/ a b c /x =~ "abc")`, "0\n"}, // x: whitespace ignored
		{`p "a1 b2 c3".scan(/\d/)`, "[\"1\", \"2\", \"3\"]\n"},
		{`p "foo".match?(/o+/)`, "true\n"},
		{`m = /(\d+)-(\d+)/.match("12-34"); p [m[1], m[2]]`, "[\"12\", \"34\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRegexpInterpolationRebuilt: an interpolated literal WITHOUT /o rebuilds on
// every evaluation — the same occurrence yields distinct objects tracking the
// current interpolated value (it must NOT be cached).
func TestRegexpInterpolationRebuilt(t *testing.T) {
	src := `
res = []
["a", "b"].each { |v| res << /#{v}/ }
p res[0].source
p res[1].source
p res[0].equal?(res[1])
p res[0].frozen?
`
	if got, want := eval(t, src), "\"a\"\n\"b\"\nfalse\ntrue\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRegexpOnceInterpolation pins /o "interpolate once": the literal is built on
// the FIRST evaluation and that object is reused for every later evaluation, even
// as the interpolated value changes — so all evaluations are .equal? and match
// the first value.
func TestRegexpOnceInterpolation(t *testing.T) {
	src := `
res = []
["a", "b", "c"].each { |v| res << /#{v}/o }
p res[0].source
p res[1].source
p res[2].source
p res[0].equal?(res[1])
p res[0].equal?(res[2])
p res[0].frozen?
`
	if got, want := eval(t, src), "\"a\"\n\"a\"\n\"a\"\ntrue\ntrue\ntrue\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRegexpNewNotFrozen: runtime construction via Regexp.new is unchanged and is
// NOT frozen (only source literals are), matching MRI.
func TestRegexpNewNotFrozen(t *testing.T) {
	src := `
p Regexp.new("x").frozen?
p(/x/.frozen?)
p Regexp.new("ab", "i") == /ab/i
`
	if got, want := eval(t, src), "false\ntrue\ntrue\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
