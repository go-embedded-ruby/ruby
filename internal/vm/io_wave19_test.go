// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestIOWave19Pread covers IO#pread coercion and boundary branches (io.c
// rb_io_pread): #to_int on maxlen/offset, #to_str on the buffer, the maxlen==0
// early return (with and without a buffer), and the negative-size / negative-
// offset / EOF error paths. Asserted against MRI Ruby 4.0.5.
func TestIOWave19Pread(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "pread.txt"))
	setup := fmt.Sprintf("File.write(%q, \"1234567890\"); f = File.open(%q, \"r+\"); ", p, p)
	cases := []struct{ src, want string }{
		{setup + `p f.pread(4, 0)`, "\"1234\"\n"},
		{setup + `p f.pread(3, 4)`, "\"567\"\n"},
		{setup + `b = +"foo"; r = f.pread(3, 4, b); p [r, b, r.equal?(b)]`, "[\"567\", \"567\", true]\n"},
		// maxlen==0: no buffer yields "", a buffer is returned untouched.
		{setup + `p f.pread(0, 4)`, "\"\"\n"},
		{setup + `b = +"foo"; r = f.pread(0, 4, b); p [r, b]`, "[\"foo\", \"foo\"]\n"},
		{setup + `b = +"foo"; f.pread(0, 400, b); p b`, "\"foo\"\n"},
		// #to_int coercion of maxlen and offset.
		{setup + `o = Object.new; def o.to_int; 4; end; p f.pread(o, 0)`, "\"1234\"\n"},
		{setup + `o = Object.new; def o.to_int; 0; end; p f.pread(4, o)`, "\"1234\"\n"},
		// #to_str coercion of the buffer.
		{setup + `o = Object.new; def o.to_str; @s ||= +"x"; end; f.pread(4, 0, o); p o.to_str`, "\"1234\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	errCases := []struct{ src, class, msg string }{
		{setup + `f.pread(-4, 0)`, "ArgumentError", "negative string size (or size too big)"},
		{setup + `f.pread(4, -1)`, "Errno::EINVAL", "Invalid argument - pread"},
		{setup + `f.pread(1, 10)`, "EOFError", "end of file reached"},
		{setup + `f.pread(Object.new, 0)`, "TypeError", "no implicit conversion of Object into Integer"},
		{setup + `f.pread(4, Object.new)`, "TypeError", "no implicit conversion of Object into Integer"},
		{setup + `f.pread(4, 0, Object.new)`, "TypeError", "no implicit conversion of Object into String"},
		{setup + `f.close; f.pread(1, 0)`, "IOError", "closed stream"},
	}
	for _, c := range errCases {
		cls, msg := evalErr(t, c.src)
		if cls != c.class || msg != c.msg {
			t.Errorf("src=%q\n got=%s: %q\nwant=%s: %q", c.src, cls, msg, c.class, c.msg)
		}
	}
	// A write-only stream cannot be pread.
	cls, msg := evalErr(t, fmt.Sprintf("f = File.open(%q, \"w\"); f.pread(1, 0)", p))
	if cls != "IOError" || msg != "not opened for reading" {
		t.Errorf("pread on write-only: got %s: %q", cls, msg)
	}
}

// TestIOWave19Pwrite covers IO#pwrite (io.c rb_io_pwrite): #to_s on the object,
// #to_int on the offset, and the closed / not-writable / negative-offset error
// paths. Asserted against MRI Ruby 4.0.5.
func TestIOWave19Pwrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "pwrite.txt"))
	setup := fmt.Sprintf("f = File.open(%q, \"w+\"); ", p)
	cases := []struct{ src, want string }{
		{setup + `p f.pwrite("foo", 0)`, "3\n"},
		{setup + `f.pwrite("bar", 3); f.write("foo"); p f.pread(6, 0)`, "\"foobar\"\n"},
		{setup + `o = Object.new; def o.to_s; "foo"; end; f.pwrite(o, 0); p f.pread(3, 0)`, "\"foo\"\n"},
		{setup + `o = Object.new; def o.to_int; 2; end; f.pwrite("foo", o); p f.pread(3, 2)`, "\"foo\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	errCases := []struct{ src, class, msg string }{
		{setup + `f.pwrite("foo", Object.new)`, "TypeError", "no implicit conversion of Object into Integer"},
		{setup + `f.pwrite(BasicObject.new, 0)`, "NoMethodError", "undefined method 'to_s' for an instance of BasicObject"},
		{setup + `f.pwrite("foo", -1)`, "Errno::EINVAL", "Invalid argument - pwrite"},
		{setup + `f.close; f.pwrite("foo", 1)`, "IOError", "closed stream"},
	}
	for _, c := range errCases {
		cls, msg := evalErr(t, c.src)
		if cls != c.class || msg != c.msg {
			t.Errorf("src=%q\n got=%s: %q\nwant=%s: %q", c.src, cls, msg, c.class, c.msg)
		}
	}
	// A read-only stream cannot be pwritten.
	cls, msg := evalErr(t, fmt.Sprintf("File.write(%q, \"x\"); f = File.open(%q, \"r\"); f.pwrite(\"y\", 0)", p, p))
	if cls != "IOError" || msg != "not opened for writing" {
		t.Errorf("pwrite on read-only: got %s: %q", cls, msg)
	}
}

// TestIOWave19PathAndToIO covers IO#to_io (returns self, open or closed), IO#path
// (a File.open path, the standard-stream "<NAME>" pseudo-paths, a pipe's nil, and
// IO.new's path: option) and the IO#to_path alias. Asserted against MRI 4.0.5.
func TestIOWave19PathAndToIO(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "path.txt"))
	cases := []struct{ src, want string }{
		{`$stdout.to_io.equal?($stdout) ? (p true) : (p false)`, "true\n"},
		{fmt.Sprintf("f = File.open(%q, \"w\"); io = f.to_io; f.close; p io.equal?(f)", p), "true\n"},
		{`p $stdout.path`, "\"<STDOUT>\"\n"},
		{`p $stderr.path`, "\"<STDERR>\"\n"},
		{`p STDIN.path`, "\"<STDIN>\"\n"},
		{`r, w = IO.pipe; p r.path; r.close; w.close`, "nil\n"},
		{fmt.Sprintf("File.open(%q, \"w\") { |f| p IO.new(f.fileno, path: f.path, autoclose: false).path }", p), fmt.Sprintf("%q\n", p)},
		// path: nil falls through, so #path reports the descriptor's own (empty) path as nil.
		{fmt.Sprintf("File.open(%q, \"w\") { |f| p IO.new(f.fileno, path: nil, autoclose: false).path }", p), "nil\n"},
		{`p IO.instance_method(:to_path) == IO.instance_method(:path)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestIOWave19Sysopen covers IO.sysopen (io.c rb_io_s_sysopen): the returned
// descriptor is a non-zero Integer wrapable by IO.for_fd, #to_path coerces the
// path, an explicit or nil mode is accepted, and a bad arity raises. Asserted
// against MRI Ruby 4.0.5.
func TestIOWave19Sysopen(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "sysopen.txt"))
	cases := []struct{ src, want string }{
		{fmt.Sprintf("fd = IO.sysopen(%q, \"w\"); p [fd.is_a?(Integer), fd != 0]", p), "[true, true]\n"},
		{fmt.Sprintf("fd = IO.sysopen(%q, \"w\"); io = IO.for_fd(fd); io.write(\"hi\"); io.close; p File.read(%q)", p, p), "\"hi\"\n"},
		// #to_path coercion of the path object.
		{fmt.Sprintf("o = Object.new; def o.to_path; %q; end; fd = IO.sysopen(o, \"w\"); p fd != 0", p), "true\n"},
		// nil mode and nil permission default to read.
		{fmt.Sprintf("File.write(%q, \"z\"); fd = IO.sysopen(%q, nil, nil); p fd != 0", p, p), "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	if cls, _ := evalErr(t, `IO.sysopen`); cls != "ArgumentError" {
		t.Errorf("sysopen with no args: got %s", cls)
	}
	if cls, _ := evalErr(t, fmt.Sprintf("IO.sysopen(%q, \"r\", 0, 0)", p)); cls != "ArgumentError" {
		t.Errorf("sysopen with 4 args: got %s", cls)
	}
}
