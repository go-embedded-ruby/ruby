// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package guardcensus

import (
	"strings"
	"testing"
)

// ⛔ Every case below except the first two is a line that actually appears in
// the pinned corpus, and the two marked FOUND BY PRINTING are the ones a
// `do\s*$` anchor misses. They are the reason this classifier exists: the
// earlier census reported 56 guards at "4.0" where there are 61, and the five
// it could not see were not exotic — three carry a trailing comment and one is
// merely parenthesised.
func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		want  Shape
		from  string
		to    string
		excl  bool
		inAnc bool // a `do$` anchor would have found it
	}{
		{
			name:  "the plain block guard",
			line:  `  ruby_version_is "4.0" do`,
			want:  Block,
			from:  "4.0",
			inAnc: true,
		},
		{
			name: "a range block guard",
			line: `  ruby_version_is ""..."4.0" do`,
			want: Block, from: "", to: "4.0", excl: true, inAnc: true,
		},
		{
			// FOUND BY PRINTING THE SITES: core/data/initialize_spec.rb:83
			name: "a block guard with a trailing comment",
			line: `  ruby_version_is "4.0" do # https://bugs.ruby-lang.org/issues/21844`,
			want: Block, from: "4.0", inAnc: false,
		},
		{
			// FOUND BY PRINTING THE SITES: core/set/pretty_print_cycle_spec.rb:9
			name: "a block guard that is parenthesised",
			line: `  ruby_version_is("4.0") do`,
			want: Block, from: "4.0", inAnc: false,
		},
		{
			name: "a brace block in an argument list",
			line: `    *ruby_version_is("4.0") { :linux },`,
			want: Block, from: "4.0", inAnc: false,
		},
		{
			name: "the ternary, which RUNS either way",
			line: `    msg_class = ruby_version_is("4.0") ? "Module" : "module"`,
			want: Inline, from: "4.0", inAnc: false,
		},
		{
			name: "a ternary on the 3.4 boundary",
			line: `    expected = ruby_version_is("3.4") ? "{a: 1}" : "{:a=>1}"`,
			want: Inline, from: "3.4", inAnc: false,
		},
		{
			name: "inside a lambda guard, which is neither",
			line: `  guard -> { ruby_version_is "3.4" and !"test".equal?("test") } do`,
			want: Block, from: "3.4", inAnc: false,
		},
		{
			name: "a line that never mentions it",
			line: `    it "returns a String" do`,
			want: Unknown,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, ok := Classify(c.line)
			if c.want == Unknown && c.line == `    it "returns a String" do` {
				if ok {
					t.Fatalf("a line with no mention was classified as %s", s.Shape)
				}
				return
			}
			if !ok {
				t.Fatal("no mention found in a line that has one")
			}
			if s.Shape != c.want {
				t.Errorf("shape %s, want %s", s.Shape, c.want)
			}
			if s.From != c.from {
				t.Errorf("from %q, want %q", s.From, c.from)
			}
			if s.To != c.to {
				t.Errorf("to %q, want %q", s.To, c.to)
			}
			if s.Excl != c.excl {
				t.Errorf("exclusive %v, want %v", s.Excl, c.excl)
			}
			if s.OldAnchor != c.inAnc {
				t.Errorf("OldAnchor %v, want %v", s.OldAnchor, c.inAnc)
			}
		})
	}
}

// ⛔ The example counter is a LOWER BOUND, and this pins the bound rather than
// pretending to an exact figure: nested blocks are followed, and a shared
// example is NOT counted because a static reader cannot expand one.
func TestExamplesIn(t *testing.T) {
	src := strings.Split(`ruby_version_is "4.0" do
  it "one" do
  end

  describe "nested" do
    it "two" do
    end
  end

  it_behaves_like :some_shared, :method
end
it "outside, must not be counted" do
end`, "\n")

	if got := ExamplesIn(src, 0); got != 2 {
		t.Errorf("counted %d examples, want 2 (the shared example is invisible to a static reader)", got)
	}
}

// A guard with no examples at all still reports zero rather than swallowing
// the file.
func TestAGuardWithNoExamples(t *testing.T) {
	src := strings.Split("ruby_version_is \"4.0\" do\n  CONST = 1\nend\n", "\n")
	if got := ExamplesIn(src, 0); got != 0 {
		t.Errorf("counted %d, want 0", got)
	}
}

// Census ties the two together and must mark an inline inside an `it`.
func TestCensusMarksAnInlineInsideAnExample(t *testing.T) {
	src := []byte(`describe "Hash#inspect" do
  it "formats" do
    expected = ruby_version_is("3.4") ? "{a: 1}" : "{:a=>1}"
    expected.should be_kind_of(String)
  end
end
`)
	sites := Census("core/hash/inspect_spec.rb", src)
	if len(sites) != 1 {
		t.Fatalf("found %d sites, want 1", len(sites))
	}
	if sites[0].Shape != Inline {
		t.Errorf("shape %s, want inline", sites[0].Shape)
	}
	if !sites[0].InIt {
		t.Error("the site is inside an `it` and was not marked so")
	}
}

// CompareVersions is spec_helper.rb's `_ver_cmp` and has to agree with it, or
// the census answers about a corpus the shim never ran. The cases that matter
// are the ones where the two spellings have different lengths — that is where a
// lexicographic comparison, the obvious wrong implementation, diverges.
func TestCompareVersionsMatchesTheShim(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"4.0", "4.0.0", 0},       // absent components read as zero
		{"3.4", "3.4.1", -1},      // and so "3.4" is BELOW "3.4.1"
		{"3.4.1", "4.0", -1},      //
		{"4.0.9", "4.0", 1},       //
		{"10.0", "9.0", 1},        // ⛔ lexicographically "10" < "9"
		{"3.4.10", "3.4.9", 1},    // ⛔ and "10" < "9" again
		{"4.0.0", "4.0.0", 0},     //
		{"4.0.0-rc1", "4.0.0", 0}, // Ruby's to_i takes the leading digits
	} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := CompareVersions(c.b, c.a); got != -c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d (not antisymmetric)", c.b, c.a, got, -c.want)
		}
	}
}

// OpenAt is the predicate the whole gate rests on: a bare string is a LOWER
// bound, a range is bounded at both ends, `...` excludes the top, and an empty
// endpoint is unbounded. Each row below is a guard shape that occurs in the
// corpus, read at the two versions #799's move runs between.
func TestOpenAtReadsEachGuardShape(t *testing.T) {
	for _, c := range []struct {
		name          string
		line          string
		at341, at409  bool
		opens, closes bool
	}{
		{
			name:  `ruby_version_is "4.0" — shut at 3.4.1, open at 4.0.9`,
			line:  `  ruby_version_is "4.0" do`,
			at341: false, at409: true, opens: true,
		},
		{
			name:  `ruby_version_is ""..."4.0" — the inverse, and it CLOSES`,
			line:  `  ruby_version_is ""..."4.0" do`,
			at341: true, at409: false, closes: true,
		},
		{
			name:  `ruby_version_is "3.4" — open on both sides, so the move does nothing to it`,
			line:  `  ruby_version_is "3.4" do`,
			at341: true, at409: true,
		},
		{
			name:  `ruby_version_is "4.1" — shut on both sides`,
			line:  `  ruby_version_is "4.1" do`,
			at341: false, at409: false,
		},
		{
			name:  `ruby_version_is "3.4".."4.0" — INCLUSIVE, so 4.0.9 is outside it`,
			line:  `  ruby_version_is "3.4".."4.0" do`,
			at341: true, at409: false, closes: true,
		},
		{
			name:  `ruby_version_is ""..."4.1" — still open at 4.0.9`,
			line:  `  ruby_version_is ""..."4.1" do`,
			at341: true, at409: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, ok := Classify(c.line)
			if !ok {
				t.Fatal("not classified")
			}
			if got := s.OpenAt("3.4.1"); got != c.at341 {
				t.Errorf("OpenAt(3.4.1) = %v, want %v", got, c.at341)
			}
			if got := s.OpenAt("4.0.9"); got != c.at409 {
				t.Errorf("OpenAt(4.0.9) = %v, want %v", got, c.at409)
			}
			m := ClassifyMove([]Site{s}, "3.4.1", "4.0.9")
			if (len(m.Opened) == 1) != c.opens {
				t.Errorf("opened %d, want opens=%v", len(m.Opened), c.opens)
			}
			if (len(m.Closed) == 1) != c.closes {
				t.Errorf("closed %d, want closes=%v", len(m.Closed), c.closes)
			}
		})
	}
}

// ⛔ THE CASE THAT COST A PUBLISHED FIGURE. core/binding/implicit_parameters_spec.rb
// is one `describe` entirely inside a `ruby_version_is "4.0"` guard: at 3.4.1
// the file is `pass=0 fail=0 error=0`, at 4.0.9 every example runs and fails.
// A pass-count delta over that file is ZERO in both directions, which is how
// 37 failures stayed out of a plan that was published as "ten examples".
//
// The census sees it from the SOURCE, before either engine runs, which is the
// only place it is visible at all.
func TestTheFileThatAPassDeltaCannotSee(t *testing.T) {
	src := []byte(`require_relative '../../spec_helper'

describe "Binding#implicit_parameters" do
  ruby_version_is "4.0" do
    it "returns the implicit parameters" do
      -> { _1 }.call(1).should == 1
    end

    it "is empty outside a block" do
      binding.implicit_parameters.should == []
    end
  end
end
`)
	sites := Census("core/binding/implicit_parameters_spec.rb", src)
	m := ClassifyMove(sites, "3.4.1", "4.0.9")
	if len(m.Opened) != 1 {
		t.Fatalf("opened %d guards, want 1", len(m.Opened))
	}
	if m.ExamplesOpened() != 2 {
		t.Errorf("opened %d examples, want 2", m.ExamplesOpened())
	}
	if files := m.Touched(); len(files) != 1 || files[0] != "core/binding/implicit_parameters_spec.rb" {
		t.Errorf("Touched() = %v, want the one file", files)
	}
	// And the control: the same census at a move that does not cross the
	// threshold must find nothing, or the gate would fire on every run.
	if quiet := ClassifyMove(sites, "4.0.5", "4.0.9"); len(quiet.Opened)+len(quiet.Closed)+len(quiet.Flipped) != 0 {
		t.Errorf("a move inside 4.0 touched %d sites, want 0", len(quiet.Touched()))
	}
}

// An inline site FLIPS: the example runs on both sides and asserts something
// different, which is neither an appearance nor a disappearance and has to be
// counted apart from both.
func TestAnInlineSiteFlipsRatherThanOpening(t *testing.T) {
	sites := Census("core/module/to_s_spec.rb", []byte(
		"  msg_class = ruby_version_is(\"4.0\") ? \"Module\" : \"module\"\n"))
	m := ClassifyMove(sites, "3.4.1", "4.0.9")
	if len(m.Flipped) != 1 {
		t.Fatalf("flipped %d, want 1", len(m.Flipped))
	}
	if len(m.Opened) != 0 || len(m.Closed) != 0 {
		t.Errorf("an inline was counted as opened/closed: %+v", m)
	}
}
