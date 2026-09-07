// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// q renders an OS path as a Ruby double-quoted string literal (paths here are
// plain ASCII temp dirs, so Go's quoting matches Ruby's).
func rq(p string) string { return strconv.Quote(slash(p)) }

// --- File.ftype -----------------------------------------------------------

// TestFileFtype covers File.ftype's arity guard, ENOENT branch and the type
// strings for a regular file, a directory and a symlink, against MRI 4.0.5.
func TestFileFtype(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "reg")
	if err := os.WriteFile(reg, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFS(t, `p File.ftype(`+rq(reg)+`)`); got != "\"file\"\n" {
		t.Errorf("ftype(file) = %q", got)
	}
	if got := runFS(t, `p File.ftype(`+rq(dir)+`)`); got != "\"directory\"\n" {
		t.Errorf("ftype(dir) = %q", got)
	}
	// #to_path argument is accepted (rb_get_path).
	if got := runFS(t, `class P;def to_path;`+rq(reg)+`;end;end; p File.ftype(P.new)`); got != "\"file\"\n" {
		t.Errorf("ftype(to_path) = %q", got)
	}
	// Arity: zero and two arguments both raise ArgumentError.
	if got := runFSErr(t, `File.ftype`); got != "ArgumentError" {
		t.Errorf("ftype() err = %q", got)
	}
	if got := runFSErr(t, `File.ftype("a","b")`); got != "ArgumentError" {
		t.Errorf("ftype(2) err = %q", got)
	}
	// Missing path raises Errno::ENOENT.
	if got := runFSErr(t, `File.ftype(`+rq(filepath.Join(dir, "nope"))+`)`); got != "Errno::ENOENT" {
		t.Errorf("ftype(missing) err = %q", got)
	}
	if runtime.GOOS == "windows" {
		return
	}
	link := filepath.Join(dir, "lnk")
	if err := os.Symlink(reg, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if got := runFS(t, `p File.ftype(`+rq(link)+`)`); got != "\"link\"\n" {
		t.Errorf("ftype(symlink) = %q", got)
	}
}

// --- File::Separator + aliases -------------------------------------------

// TestFileSeparatorAndAliases covers the File::Separator constant and the
// shared-Method aliases (File.unlink==delete, File.empty?==zero?), matching MRI.
func TestFileSeparatorAndAliases(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p File::Separator`, "\"/\"\n"},
		{`p File.method(:unlink) == File.method(:delete)`, "true\n"},
		{`p File.method(:empty?) == File.method(:zero?)`, "true\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
}

// --- num2mode (File.chmod / File.umask coercion) -------------------------

// TestNum2ModeBranches covers every path of num2mode: a plain Integer, a Bignum
// that fits a long, a Bignum that overflows (RangeError), a #to_int coercion and
// a value with no integer conversion (TypeError).
func TestNum2ModeBranches(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "m")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Integer path: File.chmod returns the count of paths (1).
	if got := runFS(t, `p File.chmod(0o600, `+rq(f)+`)`); got != "1\n" {
		t.Errorf("chmod(int) = %q", got)
	}
	// #to_int path.
	if got := runFS(t, `class TI;def to_int;0o644;end;end; p File.chmod(TI.new, `+rq(f)+`)`); got != "1\n" {
		t.Errorf("chmod(to_int) = %q", got)
	}
	// Bignum that overflows a long -> RangeError.
	if got := runFSErr(t, `File.chmod(2**64, `+rq(f)+`)`); got != "RangeError" {
		t.Errorf("chmod(bignum) err = %q", got)
	}
	// No integer conversion -> TypeError.
	if got := runFSErr(t, `File.chmod(:sym, `+rq(f)+`)`); got != "TypeError" {
		t.Errorf("chmod(sym) err = %q", got)
	}
	// Bignum that DOES fit a long: exercised directly (a fitting Bignum is hard to
	// obtain from Ruby source, which normalises to a Fixnum).
	vm := New(&bytes.Buffer{})
	if got := vm.num2mode(&object.Bignum{I: big.NewInt(0o600)}); got != 0o600 {
		t.Errorf("num2mode(fitting bignum) = %o, want 600", got)
	}
}

// TestFileUmaskArityAndCoerce covers File.umask's arity guard (>1 argument) and
// its argument-coercing branch, driven through the setUmask seam so the process
// umask is not perturbed.
func TestFileUmaskArityAndCoerce(t *testing.T) {
	if got := runFSErr(t, `File.umask(1, 2)`); got != "ArgumentError" {
		t.Errorf("umask(1,2) err = %q", got)
	}
	old := setUmask
	var set int = -1
	setUmask = func(m int) int { set = m; return 0o22 }
	defer func() { setUmask = old }()
	if got := runFS(t, `p File.umask(0o27)`); got != "18\n" { // returns the previous (0o22 == 18)
		t.Errorf("umask(0o27) = %q", got)
	}
	if set != 0o27 {
		t.Errorf("setUmask received %o, want 27", set)
	}
}

// --- oneArg / identical? / rename arity ----------------------------------

// TestPredicateArity covers the single-argument guard (File.exist?/file?) and
// File.identical?'s two-argument guard and success/one-missing branches.
func TestPredicateArity(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFS(t, `p File.exist?(`+rq(f)+`)`); got != "true\n" {
		t.Errorf("exist?(f) = %q", got)
	}
	if got := runFS(t, `p File.file?(`+rq(f)+`)`); got != "true\n" {
		t.Errorf("file?(f) = %q", got)
	}
	for _, src := range []string{`File.exist?`, `File.exist?("a","b")`, `File.file?`, `File.file?("a","b")`} {
		if got := runFSErr(t, src); got != "ArgumentError" {
			t.Errorf("%s err = %q, want ArgumentError", src, got)
		}
	}
	// identical?: success, one-missing (false), and arity.
	if got := runFS(t, `p File.identical?(`+rq(f)+`, `+rq(f)+`)`); got != "true\n" {
		t.Errorf("identical?(f,f) = %q", got)
	}
	if got := runFS(t, `p File.identical?(`+rq(f)+`, `+rq(filepath.Join(dir, "nope"))+`)`); got != "false\n" {
		t.Errorf("identical?(f,missing) = %q", got)
	}
	if got := runFSErr(t, `File.identical?(`+rq(f)+`)`); got != "ArgumentError" {
		t.Errorf("identical?(1) err = %q", got)
	}
}

// TestFileRenameArity covers File.rename's two-argument arity guard (twoPaths)
// and a successful rename.
func TestFileRenameArity(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "from")
	to := filepath.Join(dir, "to")
	if err := os.WriteFile(from, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFS(t, `p File.rename(`+rq(from)+`, `+rq(to)+`)`); got != "0\n" {
		t.Errorf("rename = %q", got)
	}
	if got := runFSErr(t, `File.rename("only-one")`); got != "ArgumentError" {
		t.Errorf("rename(1) err = %q", got)
	}
}

// --- File.truncate --------------------------------------------------------

// TestFileTruncateErrno covers File.truncate's success, Errno::EINVAL (negative
// length) and Errno::ENOENT (missing path) branches.
func TestFileTruncateErrno(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "t")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFS(t, `File.truncate(`+rq(f)+`, 3); p File.size(`+rq(f)+`)`); got != "3\n" {
		t.Errorf("truncate(3) size = %q", got)
	}
	if got := runFSErr(t, `File.truncate(`+rq(f)+`, -1)`); got != "Errno::EINVAL" {
		t.Errorf("truncate(-1) err = %q", got)
	}
	if got := runFSErr(t, `File.truncate(`+rq(filepath.Join(dir, "nope"))+`, 0)`); got != "Errno::ENOENT" {
		t.Errorf("truncate(missing) err = %q", got)
	}
}

// --- realpath / realdirpath ELOOP ----------------------------------------

// TestRealpathELOOP covers raiseRealpathErr (Errno::ELOOP for a symlink loop,
// Errno::ENOENT otherwise) and the realdirpath ELOOP / absent-leaf / ENOENT
// branches.
func TestRealpathELOOP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink loops need POSIX symlinks")
	}
	dir := t.TempDir()
	// realpath of a present file resolves (success branch).
	reg := filepath.Join(dir, "reg")
	if err := os.WriteFile(reg, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFS(t, `p File.realpath(`+rq(reg)+`) == File.realpath(`+rq(reg)+`)`); got != "true\n" {
		t.Errorf("realpath(reg) = %q", got)
	}
	// realpath of a missing path -> ENOENT.
	if got := runFSErr(t, `File.realpath(`+rq(filepath.Join(dir, "missing"))+`)`); got != "Errno::ENOENT" {
		t.Errorf("realpath(missing) err = %q", got)
	}
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if got := runFSErr(t, `File.realpath(`+rq(loop)+`)`); got != "Errno::ELOOP" {
		t.Errorf("realpath(loop) err = %q", got)
	}
	if got := runFSErr(t, `File.realdirpath(`+rq(loop)+`)`); got != "Errno::ELOOP" {
		t.Errorf("realdirpath(loop) err = %q", got)
	}
	// realdirpath tolerates an absent leaf (success) and raises ENOENT for a
	// missing intermediate directory.
	if got := runFS(t, `p File.realdirpath(`+rq(filepath.Join(dir, "absent"))+`).end_with?("absent")`); got != "true\n" {
		t.Errorf("realdirpath(absent leaf) = %q", got)
	}
	if got := runFSErr(t, `File.realdirpath("/no/such/deep/leaf")`); got != "Errno::ENOENT" {
		t.Errorf("realdirpath(missing dir) err = %q", got)
	}
}

// --- Dir path coercion + Enumerable --------------------------------------

// TestDirPathCoercionAndEnumerable covers the rb_get_path coercion of Dir's
// path-taking singletons and the post-prelude Enumerable mix-in.
func TestDirPathCoercionAndEnumerable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// #to_path arguments are accepted rather than raising a TypeError.
	tp := `class P;def to_path;` + rq(dir) + `;end;end;`
	if got := runFS(t, tp+`p Dir.entries(P.new).sort`); got != "[\".\", \"..\", \"f\"]\n" {
		t.Errorf("Dir.entries(to_path) = %q", got)
	}
	if got := runFS(t, tp+`p Dir.children(P.new)`); got != "[\"f\"]\n" {
		t.Errorf("Dir.children(to_path) = %q", got)
	}
	if got := runFS(t, tp+`p Dir.exist?(P.new)`); got != "true\n" {
		t.Errorf("Dir.exist?(to_path) = %q", got)
	}
	// Dir includes Enumerable, so Enumerable methods work on an open Dir.
	if got := runFS(t, `p Dir.include?(Enumerable)`); got != "true\n" {
		t.Errorf("Dir.include?(Enumerable) = %q", got)
	}
	if got := runFS(t, `p Dir.open(`+rq(dir)+`) { |d| d.to_a.sort }`); got != "[\".\", \"..\", \"f\"]\n" {
		t.Errorf("Dir#to_a = %q", got)
	}
}

// TestDirEmptyBranches covers Dir.empty?: an empty directory (true), a
// non-directory (false) and a missing path (Errno::ENOENT).
func TestDirEmptyBranches(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFS(t, `p Dir.empty?(`+rq(empty)+`)`); got != "true\n" {
		t.Errorf("empty?(empty) = %q", got)
	}
	if got := runFS(t, `p Dir.empty?(`+rq(f)+`)`); got != "false\n" {
		t.Errorf("empty?(file) = %q", got)
	}
	if got := runFSErr(t, `Dir.empty?(`+rq(filepath.Join(dir, "nope"))+`)`); got != "Errno::ENOENT" {
		t.Errorf("empty?(missing) err = %q", got)
	}
}

// --- Dir.delete errno mapping --------------------------------------------

// TestDirDeleteErrno covers raiseRmdirErr and the ENOTDIR guard: a successful
// rmdir, Errno::ENOTDIR (non-directory), Errno::ENOTEMPTY (non-empty) and
// Errno::ENOENT (missing).
func TestDirDeleteErrno(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := runFS(t, `p Dir.delete(`+rq(empty)+`)`); got != "0\n" {
		t.Errorf("delete(empty) = %q", got)
	}
	f := filepath.Join(dir, "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFSErr(t, `Dir.delete(`+rq(f)+`)`); got != "Errno::ENOTDIR" {
		t.Errorf("delete(file) err = %q", got)
	}
	nonempty := filepath.Join(dir, "ne")
	if err := os.MkdirAll(filepath.Join(nonempty, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := runFSErr(t, `Dir.delete(`+rq(nonempty)+`)`); got != "Errno::ENOTEMPTY" {
		t.Errorf("delete(nonempty) err = %q", got)
	}
	if got := runFSErr(t, `Dir.delete(`+rq(filepath.Join(dir, "nope"))+`)`); got != "Errno::ENOENT" {
		t.Errorf("delete(missing) err = %q", got)
	}
}

// TestDirDeleteEACCES covers the Errno::EACCES branch of raiseRmdirErr: rmdir of
// a directory under an unsearchable parent. Skipped on Windows (chmod 0 does not
// remove permissions) and as root (which bypasses the permission check).
func TestDirDeleteEACCES(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755) // restore so t.TempDir cleanup succeeds
	if got := runFSErr(t, `Dir.delete(`+rq(child)+`)`); got != "Errno::EACCES" {
		t.Errorf("delete(noperm child) err = %q, want Errno::EACCES", got)
	}
}

// --- Dir.each_child / Dir.foreach class enumerators ----------------------

// TestDirClassIterators covers Dir.each_child / Dir.foreach in both the block
// form and the block-less Enumerator form (whose #size is nil), the previous
// nil-block crash site.
func TestDirClassIterators(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := runFS(t, `a=[]; Dir.each_child(`+rq(dir)+`){|x|a<<x}; p a.sort`); got != "[\"a\", \"b\"]\n" {
		t.Errorf("each_child block = %q", got)
	}
	if got := runFS(t, `p Dir.each_child(`+rq(dir)+`).to_a.sort`); got != "[\"a\", \"b\"]\n" {
		t.Errorf("each_child enum to_a = %q", got)
	}
	if got := runFS(t, `p Dir.each_child(`+rq(dir)+`).size`); got != "nil\n" {
		t.Errorf("each_child enum size = %q", got)
	}
	if got := runFS(t, `a=[]; Dir.foreach(`+rq(dir)+`){|x|a<<x}; p a.sort`); got != "[\".\", \"..\", \"a\", \"b\"]\n" {
		t.Errorf("foreach block = %q", got)
	}
	if got := runFS(t, `p Dir.foreach(`+rq(dir)+`).to_a.sort`); got != "[\".\", \"..\", \"a\", \"b\"]\n" {
		t.Errorf("foreach enum to_a = %q", got)
	}
	if got := runFS(t, `p Dir.foreach(`+rq(dir)+`).size`); got != "nil\n" {
		t.Errorf("foreach enum size = %q", got)
	}
}

// --- Dir instance: children, chdir, each/each_child enumerators ----------

// TestDirInstanceMethods covers Dir#children, the block-less Dir#each /
// Dir#each_child enumerators (nil #size), and Dir#chdir (0 return, block value,
// closed-handle IOError and a chdir failure).
func TestDirInstanceMethods(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	qd := rq(dir)
	if got := runFS(t, `p Dir.open(`+qd+`){|d| d.children.sort}`); got != "[\"a\", \"b\"]\n" {
		t.Errorf("Dir#children = %q", got)
	}
	if got := runFS(t, `p Dir.open(`+qd+`){|d| d.each.size}`); got != "nil\n" {
		t.Errorf("Dir#each size = %q", got)
	}
	if got := runFS(t, `p Dir.open(`+qd+`){|d| d.each.to_a.sort}`); got != "[\".\", \"..\", \"a\", \"b\"]\n" {
		t.Errorf("Dir#each to_a = %q", got)
	}
	if got := runFS(t, `p Dir.open(`+qd+`){|d| d.each_child.size}`); got != "nil\n" {
		t.Errorf("Dir#each_child size = %q", got)
	}
	if got := runFS(t, `p Dir.open(`+qd+`){|d| d.each_child.to_a.sort}`); got != "[\"a\", \"b\"]\n" {
		t.Errorf("Dir#each_child to_a = %q", got)
	}

	// Dir#chdir: save and restore the process working directory around the calls.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	if got := runFS(t, `p Dir.open(`+qd+`){|d| d.chdir}`); got != "0\n" {
		t.Errorf("Dir#chdir no-block = %q", got)
	}
	if err := os.Chdir(orig); err != nil {
		t.Fatal(err)
	}
	if got := runFS(t, `p Dir.open(`+qd+`){|d| d.chdir { 42 } }`); got != "42\n" {
		t.Errorf("Dir#chdir block = %q", got)
	}
	if err := os.Chdir(orig); err != nil {
		t.Fatal(err)
	}
	// A chdir on a closed handle raises IOError.
	if got := runFSErr(t, `d=Dir.open(`+qd+`); d.close; d.chdir`); got != "IOError" {
		t.Errorf("Dir#chdir closed err = %q", got)
	}
	if err := os.Chdir(orig); err != nil {
		t.Fatal(err)
	}
	// A chdir whose target has vanished raises Errno::ENOENT.
	gone := filepath.Join(dir, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `d=Dir.open(` + rq(gone) + `); ` + `` +
		`begin; Dir.rmdir(` + rq(gone) + `); rescue; end; d.chdir`
	if got := runFSErr(t, src); got != "Errno::ENOENT" {
		t.Errorf("Dir#chdir gone err = %q", got)
	}
	if err := os.Chdir(orig); err != nil {
		t.Fatal(err)
	}
}

// --- Dir aliases (Method identity) ---------------------------------------

// TestDirAliases covers the shared-Method aliases getwd/rmdir/unlink.
func TestDirAliases(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Dir.method(:getwd) == Dir.method(:pwd)`, "true\n"},
		{`p Dir.method(:rmdir) == Dir.method(:delete)`, "true\n"},
		{`p Dir.method(:unlink) == Dir.method(:delete)`, "true\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
}
