package vm

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
)

// TestFnmatchMatcher exercises the self-contained fnmatch matcher directly, one
// truth-table row per metacharacter and flag combination. Every expected result
// was captured from ruby 4.0.5 (File.fnmatch?), so this table doubles as a
// conformance oracle and as branch coverage for the matcher.
func TestFnmatchMatcher(t *testing.T) {
	cases := []struct {
		pat, name string
		flags     int
		want      bool
	}{
		// literals, ?, *
		{"cat", "cat", 0, true}, {"cat", "category", 0, false}, {"c?t", "cat", 0, true},
		{"c*", "cat", 0, true}, {"c*", "cats/x", 0, true}, {"c*", "cats/x", fnmPathname, false},
		{"*", "", 0, true}, {"", "", 0, true}, {"", "a", 0, false}, {"?", "", 0, false},
		{"a*b*c", "axbyc", 0, true}, {"a*b*c", "axbyd", 0, false},
		// bracket expressions
		{"[a-c]at", "bat", 0, true}, {"[^a-c]at", "bat", 0, false}, {"[!a-c]at", "bat", 0, false},
		{"[?]", "?", 0, true}, {"[/]", "/", 0, true}, {"[/]", "/", fnmPathname, false},
		{"[a-]", "-", 0, true}, {"[a-]", "a", 0, true},
		{"a[b", "a[b", 0, false}, {"a[b", "ab", 0, false}, // unterminated bracket never matches
		{"[]a]", "a", 0, false}, {"[]a]", "]", 0, false}, // ']' closes an empty class
		{"[\\]]", "]", 0, true}, {"[\\]]", "]", fnmNoEscape, false},
		{"[[:alpha:]]", "a", 0, false}, // POSIX classes are not supported (as MRI)
		{"[c-a]", "b", 0, false}, {"[c-a]", "a", 0, true}, {"[c-a]", "c", 0, true},
		{"[A-Z]", "b", fnmCaseFold, true}, {"[a-z]", "B", fnmCaseFold, true}, {"[a-c]", "B", fnmCaseFold, true},
		// escapes
		{"\\?", "?", 0, true}, {"\\?", "?", fnmNoEscape, false},
		{"a\\,b", "a,b", 0, true}, {"\\.foo", ".foo", 0, true},
		// leading-dot rule
		{".foo", ".foo", 0, true}, {"*", ".foo", 0, false}, {"*", ".foo", fnmDotMatch, true},
		{"[.]", ".", 0, false}, {"[.]", ".", fnmDotMatch, true}, {"?", ".", 0, false},
		{".", ".", 0, true}, {".*", ".foo", 0, true}, {"*", "a/.b", 0, true}, {"*", "a/b", 0, true},
		// case fold
		{"CAT", "cat", fnmCaseFold, true}, {"cat", "CAT", 0, false},
		// PATHNAME + ** recursion
		{"**", "a/b", 0, true}, {"**", "a/b", fnmPathname, false}, {"*", "a/b", fnmPathname, false},
		{"**", "a/b/c", fnmPathname, false}, {"**", "ab", fnmPathname, true},
		{"**b", "a/b", fnmPathname, false}, {"x/**", "x/a/b", fnmPathname, false},
		{"a**b", "axxb", 0, true}, {"a**b", "axxb", fnmPathname, true},
		{"*/*", "a/.b", fnmPathname, false}, {"*/*", "a/.b", fnmPathname | fnmDotMatch, true},
		{"**/b", "a/x/b", fnmPathname, true}, {"**/b", "b", fnmPathname, true}, {"a/**/b", "a/b", fnmPathname, true},
		{"**/c", "a/.b/c", fnmPathname, false}, {"**/c", "a/.b/c", fnmPathname | fnmDotMatch, true},
		{"a?b", "a/b", fnmPathname, false}, {"a//b", "a//b", fnmPathname, true},
		{"*/*", "a", fnmPathname, false}, // more pattern segments than path segments
		// EXTGLOB braces
		{"{a,b}", "a", 0, false}, {"{a,b}", "a", fnmExtGlob, true},
		{"{a,{b,c}}", "c", fnmExtGlob, true}, {"{a,}", "", fnmExtGlob, true},
		{"a{b,c}d", "abd", fnmExtGlob, true}, {"a{b,c}d", "aed", fnmExtGlob, false},
		{"\\{a,b\\}", "{a,b}", fnmExtGlob, true}, {"{a/b,c}", "a/b", fnmExtGlob | fnmPathname, true},
		{"a{b}c", "a{b}c", fnmExtGlob, true}, // comma-free brace stays literal
		{"{a,b}{c,d}", "bd", fnmExtGlob, true},
	}
	for _, c := range cases {
		if got := fnmatch(c.pat, c.name, c.flags); got != c.want {
			t.Errorf("fnmatch(%q, %q, %d) = %v, want %v", c.pat, c.name, c.flags, got, c.want)
		}
	}
}

// TestFnmatchHelpers covers the standalone pattern helpers directly, pinning the
// branches (nested/empty/escaped braces, dedup+sort ordering) that the truth
// table does not exercise on its own.
func TestFnmatchHelpers(t *testing.T) {
	if got := braceExpand("a{b,c}d", true); !reflect.DeepEqual(got, []string{"abd", "acd"}) {
		t.Errorf("braceExpand nested: %v", got)
	}
	if got := braceExpand("{a,{b,c}}", true); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("braceExpand recursive: %v", got)
	}
	if got := braceExpand("x{,y}", true); !reflect.DeepEqual(got, []string{"x", "xy"}) {
		t.Errorf("braceExpand empty alt: %v", got)
	}
	if got := braceExpand("\\{a,b}", true); !reflect.DeepEqual(got, []string{"\\{a,b}"}) {
		t.Errorf("braceExpand escaped brace: %v", got)
	}
	if got := braceExpand("noBrace", true); !reflect.DeepEqual(got, []string{"noBrace"}) {
		t.Errorf("braceExpand none: %v", got)
	}
	if got := splitAlternatives("a,{b,c},", true); !reflect.DeepEqual(got, []string{"a", "{b,c}", ""}) {
		t.Errorf("splitAlternatives: %v", got)
	}
	// An escaped delimiter inside a brace is not a split point (escape-skip branch
	// of both findBrace and splitAlternatives).
	if got := splitAlternatives("a\\,b,c", true); !reflect.DeepEqual(got, []string{"a\\,b", "c"}) {
		t.Errorf("splitAlternatives escaped: %v", got)
	}
	if got := braceExpand("{a,b\\}c}", true); !reflect.DeepEqual(got, []string{"a", "b\\}c"}) {
		t.Errorf("braceExpand escaped close: %v", got)
	}
	if got := sortedUnique([]string{"b", "a", "b", "c"}, true); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("sortedUnique sorted: %v", got)
	}
	if got := sortedUnique([]string{"b", "a", "b"}, false); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("sortedUnique unsorted: %v", got)
	}
	// matchBracket signalling an unterminated class (term=false).
	if _, _, term := matchBracket("[abc", 'a', true, false); term {
		t.Errorf("matchBracket unterminated: term should be false")
	}
	if lit, ok := literalSegment("a\\*b", true); !ok || lit != "a*b" {
		t.Errorf("literalSegment escaped: %q ok=%v", lit, ok)
	}
	if _, ok := literalSegment("a*b", true); ok {
		t.Errorf("literalSegment metachar should not be literal")
	}
	if got := fsJoin("", "n") + "|" + fsJoin("/", "n") + "|" + fsJoin("d", "n"); got != "n|/n|d/n" {
		t.Errorf("fsJoin: %q", got)
	}
	if readDirNames("/no/such/dir/here") != nil {
		t.Errorf("readDirNames of missing dir should be nil")
	}
	if isDirFS("/no/such/dir") || !isDirFS(os.TempDir()) {
		t.Errorf("isDirFS wrong")
	}
	if fsExists("/no/such/path/xyz") {
		t.Errorf("fsExists false positive")
	}
}

// TestFileFnmatchRuby drives File.fnmatch? / File.fnmatch through the interpreter:
// constant values, the boolean result, the alias, flag arguments and arity.
func TestFileFnmatchRuby(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [File::FNM_NOESCAPE, File::FNM_PATHNAME, File::FNM_DOTMATCH, File::FNM_CASEFOLD, File::FNM_EXTGLOB, File::FNM_SYSCASE]`, "[1, 2, 4, 8, 16, 0]\n"},
		{`p File.fnmatch?("c?t", "cat")`, "true\n"},
		{`p File.fnmatch?("c?t", "cot")`, "true\n"},
		{`p File.fnmatch?("cat", "dog")`, "false\n"},
		{`p File.fnmatch("*.rb", "main.rb")`, "true\n"}, // alias, no '?'
		{`p File.fnmatch?("*", ".x", File::FNM_DOTMATCH)`, "true\n"},
		{`p File.fnmatch?("{a,b}", "b", File::FNM_EXTGLOB)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if class, _ := evalErr(t, `File.fnmatch?("a")`); class != "ArgumentError" {
		t.Errorf("fnmatch arity(1): got %s", class)
	}
	if class, _ := evalErr(t, `File.fnmatch?("a","b","c","d")`); class != "ArgumentError" {
		t.Errorf("fnmatch arity(4): got %s", class)
	}
}

// globTree builds a fixed directory tree under a fresh temp dir and returns its
// slash-form absolute path: a/x.rb, a/b/y.rb, a/b/c/z.rb, a/.dot, top.rb, .hidden.
func globTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"a/b/c"} {
		if err := os.MkdirAll(dir+"/"+d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"a/x.rb", "a/b/y.rb", "a/b/c/z.rb", "a/.dot", "top.rb", ".hidden"} {
		if err := os.WriteFile(dir+"/"+f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestDirGlobRuby drives Dir.glob / Dir.[] through the interpreter over globTree,
// asserting sorted results against ruby 4.0.5 for recursion, braces, char
// classes, the dotfile rule, base:/sort:/flags: keywords, arrays, blocks and the
// directory-only trailing slash.
func TestDirGlobRuby(t *testing.T) {
	dir := globTree(t)
	// chdir into the tree for the duration so relative patterns resolve there,
	// then restore — the interpreter shares the process working directory.
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	cases := []struct{ src, want string }{
		{`p Dir.glob("**/*.rb").sort`, `["a/b/c/z.rb", "a/b/y.rb", "a/x.rb", "top.rb"]` + "\n"},
		{`p Dir.glob("*").sort`, `["a", "top.rb"]` + "\n"},
		{`p Dir.glob("*", File::FNM_DOTMATCH).sort`, `[".", ".hidden", "a", "top.rb"]` + "\n"},
		{`p Dir.glob("a/**/").sort`, `["a/", "a/b/", "a/b/c/"]` + "\n"},
		{`p Dir.glob("**/").sort`, `["a/", "a/b/", "a/b/c/"]` + "\n"},
		{`p Dir.glob("a/**").sort`, `["a/b", "a/x.rb"]` + "\n"},
		{`p Dir.glob("{top,a/x}.rb").sort`, `["a/x.rb", "top.rb"]` + "\n"},
		{`p Dir.glob("a/*.rb")`, `["a/x.rb"]` + "\n"},
		{`p Dir.glob("a/[xy].rb")`, `["a/x.rb"]` + "\n"},
		{`p Dir.glob("nonexist/**/*")`, "[]\n"},
		{`p Dir.glob("nope")`, "[]\n"},
		{`p Dir.glob("a/x.rb")`, `["a/x.rb"]` + "\n"},
		{`p Dir.glob(".*").sort`, `[".", ".hidden"]` + "\n"},
		{`p Dir.glob("*/")`, `["a/"]` + "\n"},
		{`p Dir.glob("top.rb/")`, "[]\n"}, // trailing slash on a file
		{`p Dir.glob(["*.rb", "a/*.rb"]).sort`, `["a/x.rb", "top.rb"]` + "\n"},
		{`p Dir["*.rb"].sort`, `["top.rb"]` + "\n"},
		{`p Dir["top.rb", "a/x.rb"].sort`, `["a/x.rb", "top.rb"]` + "\n"},
		{`p Dir.glob("*", base: "a").sort`, `["b", "x.rb"]` + "\n"},
		{`p Dir.glob("**/*", base: "a").sort`, `["b", "b/c", "b/c/z.rb", "b/y.rb", "x.rb"]` + "\n"},
		{`p Dir.glob("*", base: "nonexistent")`, "[]\n"},
		{`p Dir.glob("*.RB", File::FNM_CASEFOLD).sort`, `["top.rb"]` + "\n"},
		{`p Dir.glob("*", flags: File::FNM_DOTMATCH).sort`, `[".", ".hidden", "a", "top.rb"]` + "\n"},
		{`p Dir.glob("*.rb", sort: false).class`, "Array\n"},
		{`p Dir.glob("*.rb", sort: false).sort`, `["top.rb"]` + "\n"},
		{`r=[]; Dir.glob("*.rb") { |f| r << f }; p r.sort`, `["top.rb"]` + "\n"},
		{`p(Dir.glob("*.rb") { |f| })`, "nil\n"}, // block form returns nil
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Absolute-path patterns (independent of cwd), including the root and a
	// directory-only trailing slash.
	abs := []struct{ src, want string }{
		{fmt.Sprintf(`p Dir.glob(%q).map { |f| File.basename(f) }.sort`, dir+"/*"), `["a", "top.rb"]` + "\n"},
		{fmt.Sprintf(`p Dir.glob(%q).size`, dir+"/**/*.rb"), "4\n"},
		{`p Dir.glob("/") == ["/"]`, "true\n"},
	}
	for _, c := range abs {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Dir.glob arity: zero patterns and too many positional args both raise.
	for _, src := range []string{`Dir.glob()`, `Dir.glob("a", 0, "extra")`} {
		if class, _ := evalErr(t, src); class != "ArgumentError" {
			t.Errorf("Dir.glob arity %q: got %s", src, class)
		}
	}
}

// TestGlobPatternDirect covers globPattern branches that are awkward to reach
// through the interpreter: an unreadable base directory and multi-pattern dedup.
func TestGlobPatternDirect(t *testing.T) {
	dir := globTree(t)
	got := globPattern("**/*.rb", dir, 0) // base set => results are relative to it
	sort.Strings(got)
	want := []string{"a/b/c/z.rb", "a/b/y.rb", "a/x.rb", "top.rb"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("globPattern absolute: %v", got)
	}
	if got := globPattern("*", dir+"/missing", 0); got != nil {
		t.Errorf("globPattern unreadable base: %v", got)
	}
}
