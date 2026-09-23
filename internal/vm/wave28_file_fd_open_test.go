// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWave28FileOpenDescriptor pins rb_file_initialize (io.c): with FEWER THAN
// THREE positional arguments, a first argument that converts through #to_int is
// a DESCRIPTOR and the call becomes io_initialize on it, not an open of a path.
// The permission argument is what rules that out — it is meaningless for an
// already-open descriptor, so File.new(fd, mode, perm) is the ordinary TypeError
// about the path instead. Every want string is MRI 4.0.5's stdout.
func TestWave28FileOpenDescriptor(t *testing.T) {
	cases := []struct{ src, want string }{
		{`fh = File.open("f.txt"); c = File.open(fh.fileno); c.autoclose = false; [c.class, c.fileno == fh.fileno]`, "[File, true]\n"},
		{`fh = File.open("f.txt"); File.new(fh.fileno).read`, "\"hello file\\n\"\n"},
		// Three positional arguments mean the first one is a path again.
		{`fh = File.open("f.txt"); File.new(fh.fileno, "r", 0644)`, "[TypeError, \"no implicit conversion of Integer into String\"]\n"},
		// An object that answers #to_int is a descriptor too.
		{`fh = File.open("f.txt"); o = Object.new; n = fh.fileno; o.define_singleton_method(:to_int) { n }; File.open(o).read`, "\"hello file\\n\"\n"},
		// ...while one that answers only #to_path is a path.
		{`o = Object.new; o.define_singleton_method(:to_path) { "f.txt" }; File.open(o).read`, "\"hello file\\n\"\n"},
		// A descriptor that was never handed out is EBADF, which is the
		// SystemCallError core/file/open_spec.rb asks for. Only the class is
		// asserted: MRI says "Bad file descriptor" where this VM names the
		// synthetic descriptor it looked for (see ioFd).
		{`begin; File.open(-1); rescue SystemCallError => e; e.class; end`, "Errno::EBADF\n"},
		{`fh = File.open("f.txt"); fh.close; File.open(fh.fileno)`, "[IOError, \"closed stream\"]\n"},
		// An explicit mode incompatible with the descriptor's is EINVAL.
		{`fh = File.open("f.txt", "r"); File.new(fh.fileno, "w")`, "[Errno::EINVAL, \"Invalid argument\"]\n"},
	}
	for _, c := range cases {
		dir := fdOpenScratch(t)
		src := "Dir.chdir(" + rubyString(dir) + ")\nbegin\n  p(begin\n" + c.src + "\nend)\nrescue => e\n  p [e.class, e.message]\nend\n"
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28FileOpenBlockClose pins the block form against rb_io_s_open and the
// io_close in its ensure: the stream is closed through the RUBY-level #close, so
// an override runs and an exception it raises propagates; a block that closed the
// file itself leaves an IOError "closed stream" that MRI swallows; and when both
// both raise, the one from #close escapes (an ensure's exception supersedes).
// File.open and File.new also instantiate the class they were CALLED on, so a
// subclass yields its own instances. MRI 4.0.5.
func TestWave28FileOpenBlockClose(t *testing.T) {
	cases := []struct{ src, want string }{
		{`File.open("f.txt") { |f| 7 }`, "7\n"},
		{`File.open("f.txt") { |f| f.close; 3 }`, "3\n"},
		{`File.open("f.txt") { |f| def f.close; raise StandardError; end }`, "[StandardError, \"StandardError\"]\n"},
		{`File.open("f.txt") { |f| def f.close; raise Exception, "non-standard"; end }`, "[Exception, \"non-standard\"]\n"},
		// Both raise: the one from #close wins, being raised from the ensure.
		{`File.open("f.txt") { |f| def f.close; raise "from close"; end; raise "from block" }`, "[RuntimeError, \"from close\"]\n"},
		// #close really is called, and really is the Ruby one.
		{`File.open("f.txt") { |f| class << f; alias_method :oc, :close; def close; oc; REC << :closed; end; end }; REC`, "[:closed]\n"},
		{`class MyFile < File; end; MyFile.open("f.txt") { |f| f.class }`, "MyFile\n"},
		{`class MyFile2 < File; end; MyFile2.new("f.txt").class`, "MyFile2\n"},
	}
	for _, c := range cases {
		dir := fdOpenScratch(t)
		// REC is the scratch array the "really is called" case records into; it is
		// defined for every case so the source stays uniform.
		src := "REC = []\nDir.chdir(" + rubyString(dir) + ")\nbegin\n  p(begin\n" +
			c.src + "\nend)\nrescue Exception => e\n  p [e.class, e.message]\nend\n"
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28FileNewWarnsOnBlock pins rb_io_s_new's rb_warn: File.new always
// returns the open stream, so a block handed to it would never be called and MRI
// says so. rb_warn is silent while $VERBOSE is nil, which is how this VM starts,
// so the case sets it. MRI 4.0.5 prints the same line (with its own file:line
// prefix, which is not part of what the spec matches).
func TestWave28FileNewWarnsOnBlock(t *testing.T) {
	dir := fdOpenScratch(t)
	src := "Dir.chdir(" + rubyString(dir) + ")\n$VERBOSE = true\n" +
		"$stderr = $stdout\nFile.new(\"f.txt\") { |f| 1 }\n"
	const want = "warning: File::new() does not take block; use File::open() instead\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// fdOpenScratch builds a directory holding "f.txt" with "hello file\n".
func fdOpenScratch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello file\n"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return dir
}
