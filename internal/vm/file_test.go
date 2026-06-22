package vm_test

import (
	"strings"
	"testing"
)

// TestFilePath covers File's pure path helpers, asserted against MRI Ruby 4.0.5.
func TestFilePath(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p File.basename("/a/b/c.txt")`, "\"c.txt\"\n"},
		{`p File.basename("/a/b/c.txt", ".txt")`, "\"c\"\n"},
		{`p File.basename("/a/b/c.txt", ".*")`, "\"c\"\n"},
		{`p File.basename("foo", ".*")`, "\"foo\"\n"},      // no extension to strip
		{`p File.basename(".txt", ".txt")`, "\".txt\"\n"},  // would empty -> kept
		{`p File.basename("foo")`, "\"foo\"\n"},
		{`p File.dirname("/a/b/c.txt")`, "\"/a/b\"\n"},
		{`p File.dirname("foo")`, "\".\"\n"},
		{`p File.dirname("/")`, "\"/\"\n"},
		{`p File.extname("/a/b/c.txt")`, "\".txt\"\n"},
		{`p File.extname("foo")`, "\"\"\n"},
		{`p File.extname(".bashrc")`, "\"\"\n"},  // leading-dot dotfile
		{`p File.extname("a.")`, "\".\"\n"},      // trailing dot
		{`p File.extname("a.b.c")`, "\".c\"\n"},
		{`p File.join("a", "b", "c")`, "\"a/b/c\"\n"},
		{`p File.join("a/", "/b")`, "\"a/b\"\n"},   // both slashes -> collapse
		{`p File.join("a/", "b")`, "\"a/b\"\n"},    // one slash -> keep
		{`p File.join("/a", "b/")`, "\"/a/b/\"\n"}, // trailing kept
		{`p File.split("/a/b/c.txt")`, "[\"/a/b\", \"c.txt\"]\n"},
		{`p File.expand_path("/a/../b")`, "\"/b\"\n"},
		{`p File.expand_path("a", "/base")`, "\"/base/a\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// expand_path with no base resolves against the working directory (the result
	// ends with the joined component; checked this way to stay portable to the
	// Windows drive-letter form C:/…/x).
	if got := eval(t, `p File.expand_path("x").end_with?("/x")`); got != "true\n" {
		t.Errorf("expand_path cwd: got %q", got)
	}
	// expand_path("~") expands the tilde away to a longer absolute path.
	if got := eval(t, `p(File.expand_path("~") != "~" && File.expand_path("~").include?("/"))`); got != "true\n" {
		t.Errorf("expand_path ~: got %q", got)
	}
	// A non-String path raises TypeError.
	if err := runErr(t, `File.basename(123)`); err == nil || !strings.Contains(err.Error(), "into String") {
		t.Errorf("basename(123): got %v want TypeError", err)
	}
}

// TestFileIO covers File's filesystem operations against a temp directory, and
// the Errno::ENOENT raised for missing paths (MRI Ruby 4.0.5 semantics).
func TestFileIO(t *testing.T) {
	dir := t.TempDir()
	cases := []struct{ src, want string }{
		{`p File.write("a.txt", "hello\nworld")`, "11\n"},
		{`File.write("a.txt", "hello\nworld"); p File.read("a.txt")`, "\"hello\\nworld\"\n"},
		{`File.write("a.txt", "hello\nworld"); p File.size("a.txt")`, "11\n"},
		{`File.write("a.txt", "x"); p File.exist?("a.txt")`, "true\n"},
		{`p File.exist?("missing.txt")`, "false\n"},
		{`File.write("a.txt", "x"); p File.file?("a.txt")`, "true\n"},
		{`p File.directory?(".")`, "true\n"},
		{`p File.file?(".")`, "false\n"},
		{`File.write("g.txt", "x"); p File.delete("g.txt"); p File.exist?("g.txt")`, "1\nfalse\n"},
		{`File.write("h.txt", "x"); p File.unlink("h.txt")`, "1\n"},
	}
	for _, c := range cases {
		got, err := runInDir(t, dir, c.src)
		if err != nil {
			t.Errorf("src=%q error: %v", c.src, err)
		} else if got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Missing-path operations raise Errno::ENOENT; writing into a missing
	// directory does too.
	for _, src := range []string{
		`File.read("/no_such_file_xyz")`,
		`File.size("/no_such_file_xyz")`,
		`File.delete("/no_such_file_xyz")`,
		`File.write("/no_such_dir_xyz/f", "x")`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "Errno::ENOENT") {
			t.Errorf("src=%q got %v want Errno::ENOENT", src, err)
		}
	}
}
