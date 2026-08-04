// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKernelOpen covers Kernel#open: the plain path form (returning a readable
// File stream), the block form (which yields the stream and closes it), the
// "|command" form mapped to Errno::ENOENT, and the no-argument ArgumentError.
func TestKernelOpen(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/k.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		{`io = open("` + f + `"); s = io.read; io.close; p s`, "\"hello\"\n"},
		{`open("` + f + `", "r") { |io| p io.read }`, "\"hello\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if got := runFSErr(t, `open("|echo hi")`); got != "Errno::ENOENT" {
		t.Errorf("pipe open: got %q want Errno::ENOENT", got)
	}
	if got := runFSErr(t, `open`); got != "ArgumentError" {
		t.Errorf("open no-args: got %q want ArgumentError", got)
	}
}

// TestIOReadBuffer covers IO#read's optional output-buffer argument and the
// length-error branches: consecutive length reads, a nil length reading the
// remainder, a negative length ArgumentError, filling a buffer, clearing the
// buffer (and returning nil) at EOF, and the FrozenError on a frozen buffer.
func TestIOReadBuffer(t *testing.T) {
	cases := []struct{ src, want string }{
		{`io = StringIO.new("1234567890"); p [io.read(1), io.read(2), io.read(3)]`, "[\"1\", \"23\", \"456\"]\n"},
		{`io = StringIO.new("abcde"); io.read(1); p io.read(nil)`, "\"bcde\"\n"},
		// output buffer is filled and returned in place of a fresh String.
		{`io = StringIO.new("abcde"); buf = +""; r = io.read(3, buf); p [r.equal?(buf), buf]`, "[true, \"abc\"]\n"},
		// at EOF a length read clears the buffer and returns nil.
		{`io = StringIO.new("ab"); io.read; buf = +"xyz"; r = io.read(3, buf); p [r, buf]`, "[nil, \"\"]\n"},
		// a nil-length read at EOF returns "" (into the buffer).
		{`io = StringIO.new("ab"); io.read; buf = +"xyz"; p io.read(nil, buf)`, "\"\"\n"},
		// a zero-length read at EOF is "" rather than nil.
		{`io = StringIO.new("ab"); io.read; p io.read(0)`, "\"\"\n"},
		// a positive-length read at EOF (no buffer) yields nil.
		{`io = StringIO.new("ab"); io.read; p io.read(3)`, "nil\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if got := runFSErr(t, `StringIO.new("abc").read(-1)`); got != "ArgumentError" {
		t.Errorf("negative length: got %q want ArgumentError", got)
	}
	if got := runFSErr(t, `io = StringIO.new("abc"); io.read(3, "frozen".freeze)`); got != "FrozenError" {
		t.Errorf("frozen buffer: got %q want FrozenError", got)
	}
}
