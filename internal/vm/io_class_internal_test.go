// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestIOReadClass covers IO.read / File.read / IO.binread: full/length/offset
// reads, the EOF-nil and empty-string cases, the BINARY tag when a size is passed,
// an explicit :encoding, and the ENOENT / EISDIR / TypeError / ArgumentError
// error branches.
func TestIOReadClass(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/data.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		{`p IO.read("` + f + `")`, "\"hello world\"\n"},
		{`p File.read("` + f + `")`, "\"hello world\"\n"},
		{`p IO.read("` + f + `", 5)`, "\"hello\"\n"},
		{`p IO.read("` + f + `", 5, 6)`, "\"world\"\n"},
		{`p IO.read("` + f + `", nil, 6)`, "\"world\"\n"},
		{`p IO.read("` + f + `", 1, 100)`, "nil\n"},    // length read past EOF
		{`p IO.read("` + f + `", nil, 100)`, "\"\"\n"}, // no-length read past EOF
		{`p IO.read("` + f + `", 0)`, "\"\"\n"},        // zero-length read
		{`p IO.read("` + f + `", 5).encoding.to_s`, "\"ASCII-8BIT\"\n"},
		{`p IO.binread("` + f + `").encoding.to_s`, "\"ASCII-8BIT\"\n"},
		{`p IO.binread("` + f + `", 5, 6)`, "\"world\"\n"},
		{`p IO.read("` + f + `", encoding: Encoding::ISO_8859_1).encoding.to_s`, "\"ISO-8859-1\"\n"},
		{`p IO.read("` + f + `", external_encoding: Encoding::ISO_8859_1).encoding.to_s`, "\"ISO-8859-1\"\n"},
		// options: a readable :mode reads; :open_args supersedes and carries encoding.
		{`p IO.read("` + f + `", mode: "r+")`, "\"hello world\"\n"},
		{`p IO.read("` + f + `", open_args: ["r"])`, "\"hello world\"\n"},
		{`p IO.read("` + f + `", open_args: [{encoding: Encoding::US_ASCII}]).encoding.to_s`, "\"US-ASCII\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Error branches.
	errCases := map[string]string{
		`IO.read("` + dir + `/missing")`:         "Errno::ENOENT",
		`IO.read("` + dir + `")`:                 "Errno::EISDIR", // a directory
		`IO.read(nil)`:                           "TypeError",
		`IO.read(mode: "r")`:                     "ArgumentError", // no path
		`IO.read("` + f + `", -1)`:               "ArgumentError",
		`IO.read("` + f + `", 0, -1)`:            "ArgumentError",
		`IO.read("` + f + `", mode: "w")`:        "IOError",
		`IO.read("` + f + `", open_args: ["w"])`: "IOError",
	}
	for src, want := range errCases {
		if got := runFSErr(t, src); got != want {
			t.Errorf("%s: got %q want %q", src, got, want)
		}
	}
}

// TestIOWriteClass covers IO.write / File.write / IO.binwrite: truncating and
// in-place (offset) writes, append and truncate-with-offset modes, :perm, to_s
// coercion of a non-String argument, and the IOError / ArgumentError guards.
func TestIOWriteClass(t *testing.T) {
	dir := slash(t.TempDir())
	seed := func(name, content string) string {
		p := dir + "/" + name
		if err := os.WriteFile(filepath.FromSlash(p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct{ src, want string }{
		// truncating write returns the byte count and replaces the content.
		func() struct{ src, want string } {
			p := seed("w1", "0123456789")
			return struct{ src, want string }{`n = IO.write("` + p + `", "abc"); p [n, File.read("` + p + `")]`, "[3, \"abc\"]\n"}
		}(),
		// in-place write at an offset does not truncate.
		func() struct{ src, want string } {
			p := seed("w2", "0123456789")
			return struct{ src, want string }{`IO.binwrite("` + p + `", "AB", 2); p File.read("` + p + `")`, "\"01AB456789\"\n"}
		}(),
		// append mode.
		func() struct{ src, want string } {
			p := seed("w3", "abc")
			return struct{ src, want string }{`IO.write("` + p + `", "de", mode: "a"); p File.read("` + p + `")`, "\"abcde\"\n"}
		}(),
		// truncate + offset ("w" with an offset) NUL-pads up to the offset.
		func() struct{ src, want string } {
			p := seed("w4", "0123456789")
			return struct{ src, want string }{`IO.write("` + p + `", "hi", 2, mode: "w"); p File.read("` + p + `").bytes`, "[0, 0, 104, 105]\n"}
		}(),
		// :perm sets create permissions on a new file.
		{`n = IO.write("` + dir + `/w5", "perm!", perm: 0o600); p n`, "5\n"},
		// a non-String argument is coerced with to_s.
		func() struct{ src, want string } {
			p := seed("w6", "x")
			return struct{ src, want string }{`IO.write("` + p + `", 123); p File.read("` + p + `")`, "\"123\"\n"}
		}(),
		// offset beyond EOF creates the file (offset given, missing file).
		{`IO.write("` + dir + `/w7", "test", 0); p File.exist?("` + dir + `/w7")`, "true\n"},
		// :open_args with a mode writes; a hash-only open_args carrying :mode works.
		func() struct{ src, want string } {
			p := seed("w8", "zzzz")
			return struct{ src, want string }{`IO.write("` + p + `", "hi", open_args: [{mode: "w"}]); p File.read("` + p + `")`, "\"hi\"\n"}
		}(),
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	p := seed("werr", "data")
	errCases := map[string]string{
		`IO.write("` + p + `")`:                                                     "ArgumentError", // arity
		`IO.write("` + p + `", "x", mode: "r")`:                                     "IOError",       // read-only
		`IO.write("` + p + `", "x", open_args: [{encoding: Encoding::UTF_8}])`:      "IOError",       // no mode in open_args
		`IO.write("` + p + `", "x", mode: "w:UTF-16LE", encoding: Encoding::UTF_8)`: "ArgumentError", // encoding twice
		`IO.write("` + dir + `/no/such/dir/f", "x")`:                                "Errno::ENOENT", // unwritable path
	}
	for src, want := range errCases {
		if got := runFSErr(t, src); got != want {
			t.Errorf("%s: got %q want %q", src, got, want)
		}
	}
}

// TestIOForeachReadlines covers IO.foreach / IO.readlines (and the File forms):
// the block form, the array form without a block, an explicit separator, and the
// no-argument ArgumentError.
func TestIOForeachReadlines(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/lines.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		{`out = ""; IO.foreach("` + f + `") { |l| out << l.chomp << "." }; p out`, "\"a.b.c.\"\n"},
		{`p IO.foreach("` + f + `").length`, "3\n"}, // no block ⇒ line array
		{`p IO.readlines("` + f + `")`, "[\"a\\n\", \"b\\n\", \"c\\n\"]\n"},
		{`p File.readlines("` + f + `").length`, "3\n"},
		{`p IO.readlines("` + f + `", "b")`, "[\"a\\nb\", \"\\nc\\n\"]\n"}, // explicit separator
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, src := range []string{`IO.foreach`, `IO.readlines`} {
		if got := runFSErr(t, src); got != "ArgumentError" {
			t.Errorf("%s: got %q want ArgumentError", src, got)
		}
	}
}

// TestIOGetsParagraphNil covers gets/readlines paragraph mode (empty separator)
// and the nil-separator whole-remainder read via StringIO.
func TestIOGetsParagraphNil(t *testing.T) {
	cases := []struct{ src, want string }{
		// paragraph mode: blank-line-separated chunks including their newline run.
		{"io = StringIO.new(\"p1a\\np1b\\n\\np2\\n\"); p [io.gets(\"\"), io.gets(\"\"), io.gets(\"\")]",
			"[\"p1a\\np1b\\n\\n\", \"p2\\n\", nil]\n"},
		// leading blank lines are skipped in paragraph mode.
		{"io = StringIO.new(\"\\n\\nx\\n\"); p io.gets(\"\")", "\"x\\n\"\n"},
		// a nil separator reads the whole remainder as one line.
		{"io = StringIO.new(\"a\\nb\\nc\"); p io.gets(nil)", "\"a\\nb\\nc\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOClassHelpers unit-tests the option helpers directly for the branches not
// otherwise reached through the surface (non-array / mode-less open_args, the
// mode readability/writability predicates, and modeBase).
func TestIOClassHelpers(t *testing.T) {
	if ioOpenArgsMode(object.IntValue(1)) != "" { // non-array
		t.Error("ioOpenArgsMode(non-array) should be empty")
	}
	if openArgsHash(object.IntValue(1)) != nil { // non-array
		t.Error("openArgsHash(non-array) should be nil")
	}
	if !ioModeReadable("r") || ioModeReadable("w") || !ioModeReadable("w+") {
		t.Error("ioModeReadable predicate wrong")
	}
	if ioModeWritable("r") || !ioModeWritable("w") || !ioModeWritable("r+") {
		t.Error("ioModeWritable predicate wrong")
	}
	if modeBase("w:UTF-8") != "w" || modeBase("r") != "r" {
		t.Error("modeBase wrong")
	}
}

// TestIOCopyStream covers IO.copy_stream across path and StringIO sources and
// destinations, the copy_length and src_offset arguments, and the
// src_offset-for-non-IO ArgumentError. Paths use Dir.mktmpdir so the test is
// cross-platform. Asserted against MRI Ruby 4.0.6.
func TestIOCopyStream(t *testing.T) {
	pre := `require "tmpdir"; require "stringio"; `
	cases := []struct{ src, want string }{
		// StringIO → StringIO, with and without a copy length.
		{pre + `a = StringIO.new("abcdef"); b = StringIO.new; n = IO.copy_stream(a, b); p [n, b.string]`, "[6, \"abcdef\"]\n"},
		{pre + `a = StringIO.new("abcdef"); b = StringIO.new; n = IO.copy_stream(a, b, 3); p [n, b.string]`, "[3, \"abc\"]\n"},
		// path → path, path → StringIO, StringIO → path, and length+offset on a path.
		{pre + `Dir.mktmpdir { |d| s = File.join(d, "s"); t = File.join(d, "t"); File.write(s, "hello"); n = IO.copy_stream(s, t); p [n, File.read(t)] }`, "[5, \"hello\"]\n"},
		{pre + `Dir.mktmpdir { |d| s = File.join(d, "s"); File.write(s, "world"); b = StringIO.new; n = IO.copy_stream(s, b); p [n, b.string] }`, "[5, \"world\"]\n"},
		{pre + `Dir.mktmpdir { |d| t = File.join(d, "t"); a = StringIO.new("data"); n = IO.copy_stream(a, t); p [n, File.read(t)] }`, "[4, \"data\"]\n"},
		{pre + `Dir.mktmpdir { |d| s = File.join(d, "s"); t = File.join(d, "t"); File.write(s, "abcdef"); n = IO.copy_stream(s, t, 3, 2); p [n, File.read(t)] }`, "[3, \"cde\"]\n"},
		// A length read past EOF copies nothing.
		{pre + `a = StringIO.new(""); b = StringIO.new; n = IO.copy_stream(a, b, 5); p [n, b.string]`, "[0, \"\"]\n"},
		// src_offset is rejected for a non-IO (StringIO) source.
		{pre + `a = StringIO.new("abcdef"); b = StringIO.new; begin; IO.copy_stream(a, b, 3, 2); rescue ArgumentError => e; p e.message; end`, "\"cannot specify src_offset for non-IO\"\n"},
		// Fewer than two arguments is an ArgumentError.
		{pre + `begin; IO.copy_stream(StringIO.new("x")); rescue ArgumentError => e; p e.class; end`, "ArgumentError\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOForFd covers IO.new / IO.for_fd wrapping a synthetic descriptor: an IO
// instance is created for a live fd, #to_int coerces the argument, the mode's
// read/write intent and encoding (from the mode string or options) are applied,
// an unknown fd raises Errno::EBADF, and the arity is 1..2. Files are created
// under Dir.mktmpdir for portability. Asserted against MRI Ruby 4.0.6.
func TestIOForFd(t *testing.T) {
	fd := `require "tmpdir"
Dir.mktmpdir do |d|
  fd = File.open(File.join(d, "f"), "w").fileno
  ` + "%s\nend"
	wrap := func(body string) string { return fmt.Sprintf(fd, body) }
	cases := []struct{ src, want string }{
		{wrap(`p IO.new(fd, "w").instance_of?(IO)`), "true\n"},
		{wrap(`p IO.for_fd(fd, "w").instance_of?(IO)`), "true\n"},
		{wrap(`p IO.for_fd(fd, "w").write("foo")`), "3\n"},
		{wrap(`io = IO.new(fd, "w:utf-8:ISO-8859-1"); p [io.external_encoding.to_s, io.internal_encoding.to_s]`), "[\"UTF-8\", \"ISO-8859-1\"]\n"},
		{wrap(`io = IO.new(fd, "w", external_encoding: "utf-8", internal_encoding: "ibm866"); p [io.external_encoding.to_s, io.internal_encoding.to_s]`), "[\"UTF-8\", \"IBM866\"]\n"},
		{wrap(`io = IO.new(fd, mode: "w:utf-8"); p io.external_encoding.to_s`), "\"UTF-8\"\n"},
		{wrap(`io = IO.new(fd, "w", encoding: "utf-8"); p io.external_encoding.to_s`), "\"UTF-8\"\n"},
		{wrap(`io = IO.new(fd, "w", encoding: "utf-8:ibm866"); p [io.external_encoding.to_s, io.internal_encoding.to_s]`), "[\"UTF-8\", \"IBM866\"]\n"},
		{wrap(`obj = Object.new; d2 = fd; obj.define_singleton_method(:to_int) { d2 }; p IO.new(obj, "w").instance_of?(IO)`), "true\n"},
		{wrap(`begin; IO.new(-999, "r"); rescue Errno::EBADF => e; p e.class; end`), "Errno::EBADF\n"},
		{wrap(`begin; IO.new; rescue ArgumentError => e; p e.class; end`), "ArgumentError\n"},
		{wrap(`begin; IO.new(fd, "r", "x"); rescue ArgumentError => e; p e.class; end`), "ArgumentError\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
