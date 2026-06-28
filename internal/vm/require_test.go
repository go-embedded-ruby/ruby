package vm_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// runInDir runs src on a fresh VM whose script path is dir/main.rb, so
// require/require_relative resolve against dir. Returns stdout and any error.
func runInDir(t *testing.T, dir, src string) (string, error) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	m := vm.New(&buf)
	m.SetScriptPath(filepath.Join(dir, "main.rb"))
	_, rerr := m.Run(iseq)
	return buf.String(), rerr
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRequire covers require / require_relative: loading a file once (and
// returning false the second time), the LOAD_PATH-style and absolute forms,
// built-in features, and nested require_relative resolving against each file.
func TestRequire(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lib.rb", "X = 42\ndef libf; 7; end\n")

	// require_relative loads the file; constants/methods become available; a
	// second require_relative returns false (already loaded).
	out, err := runInDir(t, dir, "require_relative \"lib\"\nputs X\nputs libf\np require_relative(\"lib\")")
	if err != nil {
		t.Fatalf("require_relative: %v", err)
	}
	if out != "42\n7\nfalse\n" {
		t.Errorf("require_relative got %q", out)
	}

	// plain require searches the script directory; built-in feature -> false.
	out, err = runInDir(t, dir, "p require(\"lib\")\np require(\"set\")")
	if err != nil || out != "true\nfalse\n" {
		t.Errorf("require search/built-in got %q err=%v", out, err)
	}

	// absolute path.
	abs := filepath.Join(dir, "lib.rb")
	out, _ = runInDir(t, dir, "p require("+strconvQuote(abs)+")")
	if out != "true\n" {
		t.Errorf("require abs got %q", out)
	}

	// nested require_relative: a requires b, both resolve against their own dir.
	write(t, dir, "a.rb", "require_relative \"b\"\nAVAL = BVAL + 1\n")
	write(t, dir, "b.rb", "BVAL = 10\n")
	out, err = runInDir(t, dir, "require_relative \"a\"\np AVAL")
	if err != nil || out != "11\n" {
		t.Errorf("nested require got %q err=%v", out, err)
	}

	// No script path set: resolution falls back to the process CWD.
	t.Run("cwd fallback", func(t *testing.T) {
		d2 := t.TempDir()
		write(t, d2, "cwdlib.rb", "CWDV = 5\n")
		t.Chdir(d2)
		if got := eval(t, "require_relative \"cwdlib\"\np CWDV"); got != "5\n" {
			t.Errorf("cwd fallback got %q", got)
		}
	})
}

// TestRequireErrors covers the raising paths: a missing file (LoadError), a
// non-String argument (TypeError), and parse/compile errors in a required file
// (SyntaxError).
func TestRequireErrors(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "bad_parse.rb", "1 +\n")
	write(t, dir, "bad_compile.rb", "break\n")

	for _, c := range []struct{ src, want string }{
		{`require_relative "nope"`, "LoadError"},
		{`require "definitely_missing_xyz"`, "LoadError"},
		{`require 123`, "TypeError"},
		{`require_relative "bad_parse"`, "SyntaxError"},
		{`require_relative "bad_compile"`, "SyntaxError"},
	} {
		if _, err := runInDir(t, dir, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// strconvQuote is strconv.Quote without importing strconv just for one call.
func strconvQuote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

// TestNonLocalReturnAcrossFile covers a non-local return (and a method return
// through an ensure) unwinding out of a required file's frame — the frame that
// pushed a source file, exercising the file-stack restore branch on that path.
func TestNonLocalReturnAcrossFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lib.rb", `
def from_block
  [1, 2, 3].each { |v| return "blk-#{v}" if v == 2 }
  "fell"
end
def from_ensure
  begin
    return "ens-ret"
  ensure
    $ran = "yes"
  end
end
`)
	out, err := runInDir(t, dir, `require "lib"
p from_block
p from_ensure
p $ran`)
	if err != nil {
		t.Fatalf("err=%v out=%q", err, out)
	}
	want := "\"blk-2\"\n\"ens-ret\"\n\"yes\"\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}
