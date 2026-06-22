package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDir covers the Dir class against a temp directory, asserted against MRI
// Ruby 4.0.5 semantics. The directory holds a.txt, b.txt, c.log, .hidden and a
// sub/ subdirectory.
func TestDir(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	for _, f := range []string{"a.txt", "b.txt", "c.log", ".hidden"} {
		if err := os.WriteFile(filepath.FromSlash(dir+"/"+f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.FromSlash(dir+"/sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ src, want string }{
		{fmt.Sprintf(`p Dir.entries(%q).sort`, dir), "[\".\", \"..\", \".hidden\", \"a.txt\", \"b.txt\", \"c.log\", \"sub\"]\n"},
		{fmt.Sprintf(`p Dir.children(%q).sort`, dir), "[\".hidden\", \"a.txt\", \"b.txt\", \"c.log\", \"sub\"]\n"},
		{fmt.Sprintf(`p Dir.glob(%q).sort.map { |f| File.basename(f) }`, dir+"/*"), "[\"a.txt\", \"b.txt\", \"c.log\", \"sub\"]\n"},
		{fmt.Sprintf(`p Dir[%q].size`, dir+"/*.txt"), "2\n"},
		{fmt.Sprintf(`p Dir.glob(%q)`, dir+"/nope/*"), "[]\n"}, // unreadable dir -> []
		{fmt.Sprintf(`p [Dir.exist?(%q), Dir.exists?(%q), Dir.exist?(%q)]`, dir, dir+"/sub", dir+"/a.txt"), "[true, true, false]\n"},
		{fmt.Sprintf(`p [Dir.empty?(%q), Dir.empty?(%q), Dir.empty?(%q)]`, dir, dir+"/sub", dir+"/a.txt"), "[false, true, false]\n"},
		{fmt.Sprintf(`Dir.mkdir(%q); p Dir.exist?(%q); p Dir.rmdir(%q)`, dir+"/new", dir+"/new", dir+"/new"), "true\n0\n"},
		{fmt.Sprintf(`Dir.mkdir(%q); p Dir.delete(%q)`, dir+"/d2", dir+"/d2"), "0\n"},
		{fmt.Sprintf(`Dir.mkdir(%q); p Dir.unlink(%q)`, dir+"/d3", dir+"/d3"), "0\n"},
		{fmt.Sprintf(`r=[]; Dir.each_child(%q) { |c| r << c }; p r.sort`, dir), "[\".hidden\", \"a.txt\", \"b.txt\", \"c.log\", \"sub\"]\n"},
		{fmt.Sprintf(`r=[]; Dir.foreach(%q) { |c| r << c }; p r.sort`, dir), "[\".\", \"..\", \".hidden\", \"a.txt\", \"b.txt\", \"c.log\", \"sub\"]\n"},
		{fmt.Sprintf(`p Dir.chdir(%q) { 42 }`, dir), "42\n"},
		// chdir with no arg changes to HOME for the block, then restores.
		{`p Dir.chdir { Dir.pwd.is_a?(String) }`, "true\n"},
		// glob with a relative pattern (no directory prefix) reads the cwd.
		{fmt.Sprintf(`p Dir.chdir(%q) { Dir.glob("*.txt").sort }`, dir), "[\"a.txt\", \"b.txt\"]\n"},
		{`p [Dir.pwd.is_a?(String), Dir.getwd.is_a?(String), Dir.home.is_a?(String)]`, "[true, true, true]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Non-block chdir returns 0; restore the cwd around it so later tests are
	// unaffected by the process-wide working-directory change.
	if got := eval(t, fmt.Sprintf(`o = Dir.pwd; r = Dir.chdir(%q); Dir.chdir(o); p r`, dir)); got != "0\n" {
		t.Errorf("non-block chdir = %q, want 0", got)
	}

	errs := []struct{ src, want string }{
		{fmt.Sprintf(`Dir.entries(%q)`, dir+"/nope"), "No such file"},
		{fmt.Sprintf(`Dir.empty?(%q)`, dir+"/nope"), "No such file"},
		{fmt.Sprintf(`Dir.mkdir(%q)`, dir), "File exists"},
		{fmt.Sprintf(`Dir.mkdir(%q)`, dir+"/nope/deep"), "No such file"},
		{fmt.Sprintf(`Dir.rmdir(%q)`, dir+"/nope"), "No such file"},
		{fmt.Sprintf(`Dir.chdir(%q)`, dir+"/nope"), "No such file"},
		{`Dir.entries(123)`, "into String"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}

	// Errno::EEXIST is rescuable by its scoped name.
	if got := eval(t, fmt.Sprintf(`begin; Dir.mkdir(%q); rescue Errno::EEXIST; p :exists; end`, dir)); got != ":exists\n" {
		t.Errorf("rescue Errno::EEXIST: got %q", got)
	}
}
