// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWave28FileOpenModeContract pins File.open's mode / flags / permission
// contract against the C it is ported from:
//
//   - io.c rb_io_modestr_fmode — 'r' is FMODE_READABLE, 'w' is
//     WRITABLE|TRUNC|CREATE, 'a' is WRITABLE|APPEND|CREATE, and 'x' is FMODE_EXCL
//     but ONLY when modestr[0] is 'w', which is why "rx" and "ax" are an invalid
//     access mode rather than an exclusive open;
//   - io.c rb_io_oflags_fmode / rb_io_fmode_oflags — the two directions between
//     an fmode and an open(2) flag set. O_APPEND does not imply writability, so
//     File::RDONLY|File::APPEND is a read-only stream;
//   - io.c rb_io_extract_modeenc — :mode ("mode specified twice"), :flags
//     (OR-ed into the open flags, then the fmode re-derived), :perm ("perm
//     specified twice") and the rb_scan_args "12:" arity;
//   - io.c validate_enc_binmode — "newline decorator with binary mode";
//   - file.c rb_file_open_generic — what open(2) then does with those flags.
//
// Every want string is the raw stdout of the same source under MRI ruby 4.0.5,
// compared byte for byte in a scratch directory holding a file "f.txt" with the
// contents "hello file\n" and an empty directory "adir".
func TestWave28FileOpenModeContract(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- the 'x' flag: FMODE_EXCL, and only on a 'w' base.
		{`File.open("f.txt", "wx")`, "[Errno::EEXIST, \"File exists @ rb_sysopen - f.txt\"]\n"},
		{`File.open("f.txt", "rx")`, "[ArgumentError, \"invalid access mode rx\"]\n"},
		{`File.open("f.txt", "ax")`, "[ArgumentError, \"invalid access mode ax\"]\n"},
		{`File.open("n1.txt", "wx") { |f| f.write "c" }; File.read("n1.txt")`, "\"c\"\n"},

		// --- integer modes go through open(2)'s flags, not a mode string.
		{`File.open("nope.txt", File::WRONLY)`, "[Errno::ENOENT, \"No such file or directory @ rb_sysopen - nope.txt\"]\n"},
		{`File.open("f.txt", File::CREAT | File::EXCL)`, "[Errno::EEXIST, \"File exists @ rb_sysopen - f.txt\"]\n"},
		{`f = File.open("n2.txt", File::CREAT); [File.exist?("n2.txt"), f.class]`, "[true, File]\n"},
		{`File.open("f.txt", File::TRUNC | File::WRONLY) {}; File.read("f.txt")`, "\"\"\n"},
		// O_APPEND alone leaves the stream read-only.
		{`File.open("f.txt", File::RDONLY | File::APPEND) { |f| f.write "x" }`, "[IOError, \"not opened for writing\"]\n"},
		{`File.open("f.txt", File::RDONLY | File::APPEND) { |f| f.read }`, "\"hello file\\n\"\n"},

		// --- the :flags option merges into the open flags and re-derives the fmode.
		{`File.open("f.txt", "w", flags: File::EXCL) {}`, "[Errno::EEXIST, \"File exists @ rb_sysopen - f.txt\"]\n"},
		{`File.open("f.txt", mode: "w", flags: File::EXCL) {}`, "[Errno::EEXIST, \"File exists @ rb_sysopen - f.txt\"]\n"},
		{`File.open("f.txt", File::WRONLY | File::CREAT, flags: File::EXCL) {}`, "[Errno::EEXIST, \"File exists @ rb_sysopen - f.txt\"]\n"},
		// The encoding the mode string named survives the re-derivation, because
		// MRI resolves it before reaching the :flags branch.
		{`File.open("f.txt", "rb", flags: File::RDONLY) { |f| f.external_encoding.to_s }`, "\"ASCII-8BIT\"\n"},

		// --- arity and the "specified twice" conflicts.
		{`File.open("f.txt", File::CREAT, 0755, "test")`, "[ArgumentError, \"wrong number of arguments (given 4, expected 1..3)\"]\n"},
		{`File.open`, "[ArgumentError, \"wrong number of arguments (given 0, expected 1..3)\"]\n"},
		{`File.open("f.txt", "r", mode: "w")`, "[ArgumentError, \"mode specified twice\"]\n"},
		{`File.open("p3.txt", "w", 0744, perm: 0755) {}`, "[ArgumentError, \"perm specified twice\"]\n"},

		// --- the permission argument reaches the creation, in both spellings.
		{`File.open("p2.txt", File::CREAT | File::RDWR, 0744) { |f| f.write "z" }; File.stat("p2.txt").mode.to_s(8)`, "\"100744\"\n"},
		{`File.open("p4.txt", "w", perm: 0745) {}; File.stat("p4.txt").mode.to_s(8)`, "\"100745\"\n"},

		// --- the :newline decorator.
		{`File.open("f.txt", "rb", newline: :universal) {}`, "[ArgumentError, \"newline decorator with binary mode\"]\n"},
		{`File.open("f.txt", "r", newline: :bogus) {}`, "[ArgumentError, \"unexpected value for newline option: bogus\"]\n"},
		{`File.open("f.txt", "r", newline: nil) { |f| f.read }`, "\"hello file\\n\"\n"},
		{`File.open("f.txt", "r", newline: :universal) { |f| f.read }`, "\"hello file\\n\"\n"},

		// --- a directory opens for reading and refuses to be written.
		{`File.open("adir").class`, "File\n"},
		{`File.open("adir", "w")`, "[Errno::EISDIR, \"Is a directory @ rb_sysopen - adir\"]\n"},

		// --- the access halves the fmode closes.
		{`File.open("f.txt", "w") { |f| f.read }`, "[IOError, \"not opened for reading\"]\n"},
		{`File.open("f.txt", "r") { |f| f.write "q" }`, "[IOError, \"not opened for writing\"]\n"},

		// --- O_APPEND moves WRITES to the end, and only writes: a freshly opened
		// append stream reads from the beginning and reports position 0.
		{`File.open("f.txt", "a") { |f| f.pos }`, "0\n"},
		{`File.open("f.txt", "a+") { |f| f.read }`, "\"hello file\\n\"\n"},
		{`File.open("f.txt", "a") { |f| f.write "CD" }; File.read("f.txt")`, "\"hello file\\nCD\"\n"},
	}
	for _, c := range cases {
		dir := openModeScratch(t)
		src := "Dir.chdir(" + rubyString(dir) + ")\nbegin\n  p(begin\n" + c.src + "\nend)\nrescue => e\n  p [e.class, e.message]\nend\n"
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28FileOpenPermissionGrant covers the one place a buffer-backed stream
// has to work to look like a descriptor: the permission to write is granted when
// the file is OPENED, so a chmod afterwards — including the read-only file
// File.new(path, "w", 0444) itself creates — cannot take it back, while a fresh
// open of the same path is refused with EACCES. Both halves asserted against MRI
// 4.0.5.
func TestWave28FileOpenPermissionGrant(t *testing.T) {
	dir := openModeScratch(t)
	src := "Dir.chdir(" + rubyString(dir) + ")\n" + `
fh = File.open("ro.txt", "w")
fh.chmod(0444)
begin
  File.open("ro.txt", "w")
  puts "reopened"
rescue => e
  p [e.class, e.message]
end
fh.close
f = File.new("wro.txt", "w", 0444)
f.puts("test")
f.close
p File.read("wro.txt")
`
	const want = "[Errno::EACCES, \"Permission denied @ rb_sysopen - ro.txt\"]\n\"test\\n\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
	// Leave nothing unwritable behind for the test framework's cleanup.
	for _, n := range []string{"ro.txt", "wro.txt"} {
		_ = os.Chmod(filepath.Join(dir, n), 0o644)
	}
}

// openModeScratch builds the fixture the cases above assume: a fresh directory
// holding "f.txt" (contents "hello file\n") and an empty directory "adir".
func openModeScratch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello file\n"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return dir
}
