package main

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
		want  shape
		from  string
		to    string
		inAnc bool // a `do$` anchor would have found it
	}{
		{
			name:  "the plain block guard",
			line:  `  ruby_version_is "4.0" do`,
			want:  block,
			from:  "4.0",
			inAnc: true,
		},
		{
			name: "a range block guard",
			line: `  ruby_version_is ""..."4.0" do`,
			want: block, from: "", to: "4.0", inAnc: true,
		},
		{
			// FOUND BY PRINTING THE SITES: core/data/initialize_spec.rb:83
			name: "a block guard with a trailing comment",
			line: `  ruby_version_is "4.0" do # https://bugs.ruby-lang.org/issues/21844`,
			want: block, from: "4.0", inAnc: false,
		},
		{
			// FOUND BY PRINTING THE SITES: core/set/pretty_print_cycle_spec.rb:9
			name: "a block guard that is parenthesised",
			line: `  ruby_version_is("4.0") do`,
			want: block, from: "4.0", inAnc: false,
		},
		{
			name: "a brace block in an argument list",
			line: `    *ruby_version_is("4.0") { :linux },`,
			want: block, from: "4.0", inAnc: false,
		},
		{
			name: "the ternary, which RUNS either way",
			line: `    msg_class = ruby_version_is("4.0") ? "Module" : "module"`,
			want: inline, from: "4.0", inAnc: false,
		},
		{
			name: "a ternary on the 3.4 boundary",
			line: `    expected = ruby_version_is("3.4") ? "{a: 1}" : "{:a=>1}"`,
			want: inline, from: "3.4", inAnc: false,
		},
		{
			name: "inside a lambda guard, which is neither",
			line: `  guard -> { ruby_version_is "3.4" and !"test".equal?("test") } do`,
			want: block, from: "3.4", inAnc: false,
		},
		{
			name: "a line that never mentions it",
			line: `    it "returns a String" do`,
			want: unknown,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, ok := Classify(c.line)
			if c.want == unknown && c.line == `    it "returns a String" do` {
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
	if sites[0].Shape != inline {
		t.Errorf("shape %s, want inline", sites[0].Shape)
	}
	if !sites[0].InIt {
		t.Error("the site is inside an `it` and was not marked so")
	}
}
