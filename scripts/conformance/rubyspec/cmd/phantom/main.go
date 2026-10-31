// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// phantom finds ruby/spec examples that can pass BECAUSE A METHOD IS MISSING.
//
// Why it exists (#850). A spec that asserts an exception class cannot tell why
// it got one. core/array/rfind_spec.rb has
//
//	it "raises a NoMethodError if the ifnone argument does not respond to #call …" do
//	  -> { [1, 2, 3].rfind(42) { |x| false } }.should.raise(NoMethodError)
//	end
//
// which means "42 is not callable". On a build where `Array#rfind` does not
// exist at all, `[1,2,3].rfind(42)` raises NoMethodError for THAT reason and the
// example passes. Measured on 3a3fb818, that file scored `pass=1 error=15` with
// no rfind in the interpreter: fifteen honest errors and one pass the gate
// counted for a method it did not have.
//
// So BASELINE can hold passes that are not conformance, and nothing
// distinguished them. This names them.
//
// WHAT IT DOES. For every `describe "X#y"` or `describe "X.y"` block containing
// a NoMethodError expectation, it asks a real rbgo whether that method exists —
// `X.method_defined?(:y)` (or `private_method_defined?`, since a module_function
// is private) for the `#` form, `X.respond_to?(:y)` for the `.` form. No
// instance is constructed, so a receiver whose `new` needs arguments is still
// answerable. An answer of "no" means that example's pass, if it passes, is
// suspect.
//
// ⛔ WHAT IT CANNOT SEE, printed on every run rather than left in this comment:
// it reads the describe string, so an example whose receiver is not what the
// describe names is outside its population, and a describe naming an operator
// method (`[]`, `<=>`, `+`) is skipped because those do not match the identifier
// shape. Same class of limit as cmd/guards declaring its example counts a lower
// bound. A census that does not state its population is a census that will be
// read as a total.
//
// Exit status: 0 nothing suspect, 1 suspects found, 2 refused — could not read
// the corpus, could not run the interpreter, or its own control failed.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// NOTE ON THE FLAG PACKAGE. The fleet's rule is that command-line tools use
// cobra. This one does not, deliberately: it is a conformance census that lives
// beside cmd/ratchet and cmd/guards and is invoked the same way they are, and
// cobra is not a direct dependency of this module — adding one to the
// interpreter's go.mod for a script would enlarge the dependency graph of every
// consumer of a repository whose whole claim is pure Go and minimal. If the three
// should be converted together, that is one change and worth doing as one.

// describeRe matches a describe line that names a receiver and a method. The
// receiver may be namespaced (`Math::DomainError`); the method may end in ?, !
// or =. Operator methods are deliberately NOT matched — see the package comment.
var describeRe = regexp.MustCompile(`^\s*describe\s+"([A-Z][A-Za-z0-9_]*(?:::[A-Z][A-Za-z0-9_]*)*)([#.])([a-zA-Z_][A-Za-z0-9_]*[?!=]?)"`)

// noMethodRe matches the two spellings of the expectation, mspec's and RSpec's.
var noMethodRe = regexp.MustCompile(`should\.raise\(NoMethodError|should\s+raise_error\(NoMethodError`)

// anyDescribeRe matches a describe line whatever it names — a constant
// (`describe "RUBY_VERSION"`), a shared block (`describe :foo, shared: true`), a
// prose grouping (`describe "when empty"`).
//
// ⛔ It exists because attribution has to STOP somewhere. Without it, the
// expectation inside
//
//	describe "String#squeeze" do … end
//	describe :shared_thing, shared: true do
//	  it "raises a NoMethodError" do … end
//	end
//
// was attributed to String#squeeze — a claim that file does not make, and the
// kind of false positive that sends a reader to check a non-issue. Its own test
// predicted it and the first implementation had it.
var anyDescribeRe = regexp.MustCompile(`^(\s*)describe[\s(]`)

// absenceRe matches an assertion that the method is NOT there.
//
// ⛔ THIS IS THE FALSE-POSITIVE GUARD, and it was added because the first run
// produced two. core/complex/negative_spec.rb reads, in full:
//
//	describe "Complex#negative?" do
//	  it "is undefined" do
//	    c = Complex(1)
//	    c.methods.should_not.include?(:negative?)
//	    -> { c.negative? }.should.raise(NoMethodError)
//	  end
//	end
//
// MRI does not define Complex#negative? — Complex has no ordering — and the spec
// asserts that absence on purpose. rbgo passing it is CONFORMANCE, not a pass
// bought by a missing method, and reporting it would have sent a reader to fix
// something that is right. `core/complex/positive_spec.rb` is the same shape.
//
// So "the describe names a method the interpreter does not define" is not
// sufficient: an example that ALSO asserts the absence is asserting exactly what
// it got. Those are excluded, by their own assertion rather than by a list of
// file names — a denylist of paths would have gone stale at the next corpus bump
// and would not have covered the third such spec nobody had met yet.
var absenceRe = regexp.MustCompile(`should_not[.\s]*(?:include|respond_to)|respond_to\?\([^)]*\)\.should\s*==\s*false|should_not\.respond_to\?`)

// site is one suspect expectation.
type site struct {
	file     string
	line     int
	receiver string
	kind     string // "#" or "."
	method   string
}

func (s site) name() string { return s.receiver + s.kind + s.method }

// vocabularyCases is the parser's own control, run on EVERY invocation rather
// than only under `go test`: this tool is run by hand and from CI as a binary,
// and a describe pattern that silently stops matching reports an empty census,
// which is indistinguishable from a clean corpus.
var vocabularyCases = []struct {
	line string
	want string // "" when no match is expected
}{
	{`describe "Array#rfind" do`, "Array#rfind"},
	{`describe "Math.log1p" do`, "Math.log1p"},
	{`  describe "Kernel#require" do`, "Kernel#require"},
	{`describe "String#start_with?" do`, "String#start_with?"},
	{`describe "Struct#each_pair" do`, "Struct#each_pair"},
	{`describe "Math::DomainError" do`, ""}, // a constant, no method named
	{`describe "RUBY_VERSION" do`, ""},
	{`describe :array_find, shared: true do`, ""}, // a shared block
	{`describe "Array#[]" do`, ""},                // operator: outside the population
	{`  it "raises a NoMethodError" do`, ""},
}

func checkVocabulary() error {
	for _, c := range vocabularyCases {
		m := describeRe.FindStringSubmatch(c.line)
		got := ""
		if m != nil {
			got = m[1] + m[2] + m[3]
		}
		if got != c.want {
			return fmt.Errorf("the describe pattern is wrong: %q read as %q, want %q", c.line, got, c.want)
		}
	}
	for _, s := range []string{
		`-> { [1,2,3].rfind(42) { |x| false } }.should.raise(NoMethodError)`,
		`expect { x.y }.should raise_error(NoMethodError)`,
	} {
		if !noMethodRe.MatchString(s) {
			return fmt.Errorf("the expectation pattern missed %q", s)
		}
	}
	if noMethodRe.MatchString(`-> { x.y }.should.raise(NameError)`) {
		return fmt.Errorf("the expectation pattern matches NameError, which is a different claim")
	}
	return nil
}

func main() {
	root := flag.String("root", ".", "the ruby/spec checkout")
	rbgo := flag.String("rbgo", "rbgo", "the interpreter to ask")
	flag.Parse()

	if err := checkVocabulary(); err != nil {
		fmt.Fprintf(os.Stderr, "phantom: %v\n", err)
		os.Exit(2)
	}

	sites, files, err := collect(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "phantom: %v\n", err)
		os.Exit(2)
	}
	// "N of M, never N alone": a walk that matched nothing must not read as a
	// clean corpus.
	if len(sites) == 0 {
		fmt.Fprintf(os.Stderr, "phantom: no NoMethodError expectation found under %s in %d spec file(s); "+
			"either the corpus is not there or the patterns no longer fit\n", *root, files)
		os.Exit(2)
	}

	have, err := ask(*rbgo, sites)
	if err != nil {
		fmt.Fprintf(os.Stderr, "phantom: %v\n", err)
		os.Exit(2)
	}

	// ⛔ THE CONTROL, on this very corpus and this very interpreter, before any
	// verdict: a method that certainly exists must come back present, and the
	// absence detector must be able to answer "no" at all. Without the second
	// half, an `ask` that returned "present" for everything would report a clean
	// census — the most reassuring possible way for this tool to be broken.
	if !probe(*rbgo, "Array", "#", "find") {
		fmt.Fprintf(os.Stderr, "phantom: control failed — the interpreter says Array#find does not exist, "+
			"so a 'missing' answer means nothing here\n")
		os.Exit(2)
	}
	if probe(*rbgo, "Array", "#", "no_such_method_aaa") {
		fmt.Fprintf(os.Stderr, "phantom: control failed — the interpreter says a method that cannot exist "+
			"does exist, so nothing would ever be reported\n")
		os.Exit(2)
	}

	var suspect []site
	for _, s := range sites {
		if !have[s.name()] {
			suspect = append(suspect, s)
		}
	}

	fmt.Printf("%d NoMethodError expectation(s) over %d spec file(s); %d name a method this interpreter does not define\n",
		len(sites), files, len(suspect))
	fmt.Printf("population: describe strings of the form \"X#y\" or \"X.y\" only — operator methods and " +
		"shared blocks are outside it, and an example whose receiver differs from its describe is invisible here\n")
	if len(suspect) == 0 {
		return
	}
	sort.Slice(suspect, func(i, j int) bool {
		if suspect[i].file != suspect[j].file {
			return suspect[i].file < suspect[j].file
		}
		return suspect[i].line < suspect[j].line
	})
	for _, s := range suspect {
		fmt.Printf("%s:%d\t%s is not defined — a pass here may be the absence, not the behaviour\n",
			s.file, s.line, s.name())
	}
	os.Exit(1)
}

// collect walks the corpus and returns every NoMethodError expectation that sits
// under a describe naming a method, plus the number of spec files read.
//
// Attribution is to the MOST RECENT matching describe line, which is what a
// line-at-a-time read can honestly do: a nested describe that names a method
// takes over from its parent, and an expectation under a describe that names no
// method is skipped rather than guessed at.
func collect(root string) ([]site, int, error) {
	var out []site
	files := 0
	for _, tree := range []string{"language", "core"} {
		dir := filepath.Join(root, tree)
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, "_spec.rb") {
				return nil
			}
			files++
			f, oerr := os.Open(path)
			if oerr != nil {
				return oerr
			}
			defer f.Close()
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				rel = path
			}
			var cur *site
			curIndent := 0
			// Sites are held until the describe block ends, because an assertion
			// of absence anywhere in it withdraws them all: the block is then
			// asserting the absence it got, which is conformance.
			var pending []site
			asserts := false
			flush := func() {
				if !asserts {
					out = append(out, pending...)
				}
				pending, asserts = nil, false
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			for n := 1; sc.Scan(); n++ {
				text := sc.Text()
				if m := describeRe.FindStringSubmatch(text); m != nil {
					flush()
					cur = &site{file: rel, receiver: m[1], kind: m[2], method: m[3]}
					curIndent = len(m[0]) - len(strings.TrimLeft(m[0], " \t"))
					continue
				}
				if cur != nil && absenceRe.MatchString(text) {
					asserts = true
					continue
				}
				// A describe this cannot attribute ends the previous attribution
				// only when it is a SIBLING or an outer block — judged by
				// indentation. A describe nested INSIDE the method's own
				// ("describe "Array#rfind" do" then "  describe "with a block" do")
				// is a sub-grouping of the same method and attribution continues
				// through it, which is how ruby/spec is usually written.
				//
				// Indentation is a convention, not a parse. It is used here rather
				// than counting `do`/`end` because the failure mode it risks is a
				// MISS (attribution stops early, a candidate goes unreported),
				// where mis-attribution would invent a claim. For a tool whose
				// output sends someone to read a spec, that is the right direction
				// to be wrong in — and the count it prints is therefore a lower
				// bound, which the output says.
				if m := anyDescribeRe.FindStringSubmatch(text); m != nil {
					if indent := len(m[1]); cur != nil && indent <= curIndent {
						flush()
						cur = nil
					}
					continue
				}
				if cur != nil && noMethodRe.MatchString(text) {
					s := *cur
					s.line = n
					pending = append(pending, s)
				}
			}
			flush()
			return sc.Err()
		})
		if err != nil {
			return nil, 0, fmt.Errorf("walking %s: %w", dir, err)
		}
	}
	return out, files, nil
}

// ask runs one Ruby program that reports, for each distinct method named, whether
// the interpreter defines it. One process for the whole census: asking per site
// would be hundreds of interpreter start-ups.
func ask(rbgo string, sites []site) (map[string]bool, error) {
	seen := map[string]site{}
	for _, s := range sites {
		seen[s.name()] = s
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(probeProgramPrelude)
	for _, n := range names {
		s := seen[n]
		fmt.Fprintf(&b, "q(%q, %q, %q)\n", s.receiver, s.kind, s.method)
	}

	out, err := run(rbgo, b.String())
	if err != nil {
		return nil, fmt.Errorf("asking the interpreter: %w", err)
	}
	have := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 4 || parts[0] != "ANSWER" {
			continue
		}
		have[parts[1]] = parts[2] == "yes"
	}
	if len(have) != len(names) {
		return nil, fmt.Errorf("asked about %d method(s) and got %d answer(s); "+
			"an unanswered question would read as absent", len(names), len(have))
	}
	return have, nil
}

// probe asks a single question, used only for the control.
func probe(rbgo, receiver, kind, method string) bool {
	out, err := run(rbgo, probeProgramPrelude+fmt.Sprintf("q(%q, %q, %q)\n", receiver, kind, method))
	if err != nil {
		return false
	}
	return strings.Contains(out, "\tyes\t")
}

// probeProgramPrelude defines the question in Ruby. A module_function is a
// PRIVATE instance method, so `method_defined?` alone would report Math.log1p's
// instance side as absent; both are asked.
const probeProgramPrelude = `
def q(const, kind, meth)
  present = begin
    if Object.const_defined?(const)
      k = Object.const_get(const)
      if kind == "#"
        k.method_defined?(meth.to_sym) || k.private_method_defined?(meth.to_sym)
      else
        k.respond_to?(meth.to_sym)
      end
    else
      false
    end
  rescue StandardError
    false
  end
  puts "ANSWER\t#{const}#{kind}#{meth}\t#{present ? "yes" : "no"}\t."
end
`

// run executes src with the interpreter and returns its stdout.
func run(rbgo, src string) (string, error) {
	f, err := os.CreateTemp("", "phantom-*.rb")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(src); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	cmd := exec.Command(rbgo, f.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %v: %s", rbgo, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
