// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package guardcensus

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// TestWalkPlatformTakesSpecFilesAndNotDirectoriesNamedLikeThem gives the walk's
// filter a witness on each side. A directory whose name ends in `_spec.rb` is
// not impossible, and reading one would be an error attributed to the corpus;
// a suite the caller did not name must not be walked at all, because the gate
// sweeps exactly two and a census of three measures a different population.
func TestWalkPlatformTakesSpecFilesAndNotDirectoriesNamedLikeThem(t *testing.T) {
	root := t.TempDir()
	mkdir := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mkdir("core/io")
	mkdir("core/trap_spec.rb") // a DIRECTORY whose name ends in _spec.rb
	mkdir("language")
	mkdir("library")
	write("core/io/seek_spec.rb", "platform_is :darwin do\n  it \"x\" do\n  end\nend\n")
	write("core/io/fixtures.rb", "platform_is :linux do\nend\n")     // not a spec file
	write("library/socket_spec.rb", "platform_is :linux do\nend\n")  // suite not named
	write("language/block_spec.rb", "platform_is_not :windows do\n") // a spec file

	sites, files, err := WalkPlatform(root, []string{"language", "core"})
	if err != nil {
		t.Fatalf("WalkPlatform: %v", err)
	}
	if files != 2 {
		t.Fatalf("read %d file(s), want 2 — a directory named *_spec.rb or an unnamed suite was walked", files)
	}
	if len(sites) != 2 {
		t.Fatalf("found %d site(s), want 2: %+v", len(sites), sites)
	}
	for _, s := range sites {
		if s.File == "library/socket_spec.rb" || s.File == "core/io/fixtures.rb" {
			t.Fatalf("walked %s, which is outside the named suites or not a spec file", s.File)
		}
	}
}

// TestWalkPlatformReportsWhatItCouldNotRead holds the two error arms that stand
// between a partial read and a clean-looking census.
//
// ⛔ THIS IS THE FAILURE MODE THE WHOLE PACKAGE EXISTS TO AVOID, one layer
// down. Swallow either of these and the walk returns a SHORTER census with no
// error — and a shorter census is not wrong-looking anywhere downstream, it is
// simply a smaller number reported with the same confidence as a whole one.
// cmd/platguard would then print "0 disagreements" for a corpus it half read.
//
// The skips are about what the host can express, not about what the code does:
// Windows does not honour mode 0, and root ignores it. Both lanes that record
// the baseline run this.
func TestWalkPlatformReportsWhatItCouldNotRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a 0-mode path is not a permission denial on this platform")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores the mode this test sets")
	}

	t.Run("a directory it cannot enter", func(t *testing.T) {
		root := t.TempDir()
		locked := filepath.Join(root, "core", "locked")
		if err := os.MkdirAll(locked, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(locked, "a_spec.rb"), []byte("platform_is :linux do\nend\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		if _, err := os.ReadDir(locked); err == nil {
			t.Skip("this filesystem read the directory at mode 0; the condition could not be arranged")
		}
		if sites, _, err := WalkPlatform(root, []string{"core"}); err == nil {
			t.Fatalf("WalkPlatform returned %d site(s) and no error for a subtree it could not enter", len(sites))
		}
	})

	t.Run("a spec file it cannot open", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "core")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		spec := filepath.Join(dir, "a_spec.rb")
		if err := os.WriteFile(spec, []byte("platform_is :linux do\nend\n"), 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(spec, 0o644) })
		if _, err := os.ReadFile(spec); err == nil {
			t.Skip("this filesystem read the file at mode 0; the condition could not be arranged")
		}
		if sites, _, err := WalkPlatform(root, []string{"core"}); err == nil {
			t.Fatalf("WalkPlatform returned %d site(s) and no error for a file it could not open", len(sites))
		}
	})
}

// TestWalkPlatformSaysItReadNothingRatherThanReportingAnEmptyCensus keeps the
// two apart: `files == 0` is what lets a caller refuse, instead of printing a
// clean census of a tree it never opened.
func TestWalkPlatformSaysItReadNothingRatherThanReportingAnEmptyCensus(t *testing.T) {
	sites, files, err := WalkPlatform(t.TempDir(), []string{"language", "core"})
	if err != nil {
		t.Fatalf("WalkPlatform on an empty tree: %v", err)
	}
	if files != 0 || len(sites) != 0 {
		t.Fatalf("got %d file(s) and %d site(s) from an empty tree", files, len(sites))
	}
}

// The shim's constants as they stood when #837 was filed. The tests drive
// Literal with these rather than with whatever the shim holds today, because
// Literal exists to say what the DEFECT cost, and a defect whose inputs move
// with the fix cannot be costed.
var (
	oldPlatforms = []string{"darwin", "bsd", "unix"}
	oldWordsize  = 64
)

func TestClassifyPlatformReadsTheShapesTheCorpusActuallyUses(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want PlatformSite
	}{
		{
			"the commonest site in the corpus",
			"  platform_is_not :windows do",
			PlatformSite{Negated: true, OSes: []string{"windows"}, Block: true},
		},
		{
			"several names",
			"  platform_is :linux, :darwin do",
			PlatformSite{OSes: []string{"linux", "darwin"}, Block: true},
		},
		{
			"a size option and no name",
			"  platform_is pointer_size: 64 do",
			PlatformSite{PointerSize: 64, Block: true},
		},
		{
			"c_long_size, which the shim already answered correctly",
			"  platform_is c_long_size: 32 do",
			PlatformSite{CLongSize: 32, Block: true},
		},
		{
			"parenthesised",
			"      platform_is_not(:windows) do",
			PlatformSite{Negated: true, OSes: []string{"windows"}, Block: true},
		},
		{
			// core/process/clock_gettime_spec.rb:140 writes a whole triple.
			// mspec matches it as a substring like any other name, so a shim
			// holding a list of symbols can never match it at all.
			"a full platform triple as a string",
			`    guard -> { platform_is "x86_64-linux" and kernel_version_is '3.0' } do`,
			PlatformSite{OSes: []string{"x86_64-linux"}},
		},
		{
			"used as a value, not as a block",
			`    cmd = platform_is(:windows) ? 'rem' : 'cat'`,
			PlatformSite{OSes: []string{"windows"}},
		},
		{
			"a trailing comment does not swallow the block",
			"  platform_is :windows do #|| VMS",
			PlatformSite{OSes: []string{"windows"}, Block: true},
		},
		{
			// ⛔ core/file/stat/birthtime_spec.rb:3 opens its argument list and
			// closes it on the NEXT line. Reading the first line's names would
			// report a smaller guard than the source holds, so the site is
			// refused instead. "Could not read it" is not "it names nothing".
			"an argument list that does not close on its line is REFUSED",
			"platform_is(:windows, :darwin, :freebsd, :netbsd,",
			PlatformSite{Unclassified: true},
		},
		{
			// The call is mentioned but never made: no `(` and no space after
			// the name, so there is nothing to read as an argument list. A
			// reader that fell through here would report a guard with no names,
			// which means "every platform" — the most permissive answer there
			// is, invented out of a line it could not parse.
			"a bare mention with nothing after it is REFUSED",
			"  x = platform_is",
			PlatformSite{Unclassified: true},
		},
		{
			// `FOO` is neither a symbol, nor a string, nor `key: value`. It is
			// a constant the census cannot resolve, and guessing would put the
			// site on one side of the comparison on no evidence.
			// Block stays true: the reader did establish that the line opens
			// one, and only then failed on the arguments. Harmless, because an
			// Unclassified site is never judged by either side.
			"an argument this reader cannot resolve is REFUSED",
			"  platform_is FOO do",
			PlatformSite{Block: true, Unclassified: true},
		},
		{
			// An option name nothing implements. mspec's match? ignores an
			// unknown key, but ignoring it here would silently widen the guard,
			// so the site is refused and counted apart instead.
			"an option this reader does not know is REFUSED",
			"  platform_is endian: :big do",
			PlatformSite{Block: true, Unclassified: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClassifyPlatform(tc.line)
			if !ok {
				t.Fatalf("ClassifyPlatform(%q) found no guard", tc.line)
			}
			got.Text, got.File, got.Line = "", "", 0
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ClassifyPlatform(%q)\n got %+v\nwant %+v", tc.line, got, tc.want)
			}
		})
	}
}

// TestALineWithNoPlatformGuardIsNotASite is the accepting side of the reader's
// boundary: a gate that only ever finds something is not a measurement.
func TestALineWithNoPlatformGuardIsNotASite(t *testing.T) {
	for _, line := range []string{
		`  ruby_version_is "4.0" do`,
		`    it "works" do`,
		`    @platform = "linux"`,
	} {
		if _, ok := ClassifyPlatform(line); ok {
			t.Fatalf("ClassifyPlatform(%q) claimed a guard where there is none", line)
		}
	}
}

// TestACommentedOutGuardIsNotCounted puts the comment rule where it actually
// lives. ClassifyPlatform answers about a line of code; CensusPlatform is what
// decides a line is code. The corpus contains at least one commented mention
// (core/io/close_on_exec_spec.rb carries a TODO naming `platform_is_not`), and
// counting it would inflate the census with a guard that does not exist.
func TestACommentedOutGuardIsNotCounted(t *testing.T) {
	src := []byte("# TODO: remove this `platform_is_not` guard.\nplatform_is :linux do\n  it \"x\" do\n  end\nend\n")
	sites := CensusPlatform("probe_spec.rb", src)
	if len(sites) != 1 {
		t.Fatalf("got %d site(s), want 1 — the commented line must not be counted: %+v", len(sites), sites)
	}
	if sites[0].Line != 2 {
		t.Fatalf("the site is at line %d, want 2", sites[0].Line)
	}
	if sites[0].Examples != 1 {
		t.Fatalf("the guard holds %d example(s), want 1", sites[0].Examples)
	}
}

// TestTheLiteralAndMspecDisagreeExactlyWhereTheySaidTheyWould is the heart of
// #837: each row is a site, and it carries BOTH answers. The rows where they
// agree are as load-bearing as the rows where they differ — a fix justified
// only by the failing rows could be a rule that is simply broader.
func TestTheLiteralAndMspecDisagreeExactlyWhereTheySaidTheyWould(t *testing.T) {
	for _, tc := range []struct {
		name        string
		line        string
		machine     Machine
		literalRuns bool
		mspecRuns   bool
	}{
		{
			"platform_is :linux on the gate's own runner — the headline of #837",
			"platform_is :linux do",
			Linux64, false, true,
		},
		{
			"and the same site on darwin, where both say no",
			"platform_is :linux do",
			Darwin64, false, false,
		},
		{
			"platform_is :darwin runs on LINUX because the literal contains it",
			"platform_is :darwin do",
			Linux64, true, false,
		},
		{
			"platform_is :darwin on darwin — both say yes",
			"platform_is :darwin do",
			Darwin64, true, true,
		},
		{
			// The literal's :bsd matches no RUBY_PLATFORM this project runs on.
			// Measured: it was asserting a BSD-only expectation on a mac, and
			// core/file/sticky_spec.rb went pass=4 fail=1 -> pass=4 fail=0.
			"platform_is :bsd ran on darwin and should not",
			"platform_is :bsd do",
			Darwin64, true, false,
		},
		{
			// ⛔ The bits/bytes defect. The shim compared the corpus's 64
			// against WORDSIZE/8 = 8, so BOTH branches of every pointer_size
			// guard were false and neither ran, on every machine.
			"pointer_size: 64 never ran anywhere",
			"platform_is pointer_size: 64 do",
			Darwin64, false, true,
		},
		{
			"...nor on linux",
			"platform_is pointer_size: 64 do",
			Linux64, false, true,
		},
		{
			"and the 32-bit branch correctly stays shut, which is why nothing looked wrong",
			"platform_is pointer_size: 32 do",
			Linux64, false, false,
		},
		{
			// The separate negation path. mspec is run_unless(match?) over ONE
			// predicate; the old shim OR-ed the parts, which is not its dual.
			"platform_is_not with a name AND a size",
			"platform_is_not :linux, c_long_size: 32 do",
			Linux64, false, true,
		},
		{
			"c_long_size alone, which the shim already answered correctly",
			"platform_is c_long_size: 64 do",
			Linux64, true, true,
		},
		{
			"the corpus's commonest site, right by accident on both",
			"platform_is_not :windows do",
			Linux64, true, true,
		},
		{
			"a name neither side has heard of is simply false",
			"platform_is :haiku do",
			Linux64, false, false,
		},
		{
			"a full triple matches as a substring upstream and can never be in a list",
			`platform_is "x86_64-linux" do`,
			Linux64, false, true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ok := ClassifyPlatform(tc.line)
			if !ok || s.Unclassified {
				t.Fatalf("ClassifyPlatform(%q) could not read the site", tc.line)
			}
			if got := Literal(s, oldPlatforms, oldWordsize); got != tc.literalRuns {
				t.Errorf("the OLD shim ran it = %v, want %v", got, tc.literalRuns)
			}
			if got := tc.machine.Runs(s); got != tc.mspecRuns {
				t.Errorf("mspec on %s runs it = %v, want %v", tc.machine.RubyPlatform, got, tc.mspecRuns)
			}
		})
	}
}

// TestDisagreementsSkipsWhatItCouldNotRead holds the three states apart: a site
// the reader refused is neither an agreement nor a disagreement.
func TestDisagreementsSkipsWhatItCouldNotRead(t *testing.T) {
	sites := []PlatformSite{
		{Unclassified: true, File: "unreadable.rb"},
		{OSes: []string{"linux"}, Block: true, File: "linux.rb"},
		{OSes: []string{"windows"}, Negated: true, Block: true, File: "agrees.rb"},
	}
	ds := Disagreements(sites, Linux64, oldPlatforms, oldWordsize)
	if len(ds) != 1 {
		t.Fatalf("got %d disagreement(s), want exactly the one readable site that differs: %+v", len(ds), ds)
	}
	if ds[0].Site.File != "linux.rb" {
		t.Fatalf("the disagreement is in %s, want linux.rb", ds[0].Site.File)
	}
	if !ds[0].Lost() || ds[0].Gained() {
		t.Fatalf("platform_is :linux on linux was SKIPPED and should run: Lost=%v Gained=%v", ds[0].Lost(), ds[0].Gained())
	}
}
