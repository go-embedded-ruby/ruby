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

// TestIOWave19Inspect covers IO#inspect (io.c rb_io_inspect) across the four
// pathv/closed states — an open and a closed path stream, an open plain fd
// wrapper and a closed one — and the standard-stream forms. Asserted against MRI
// Ruby 4.0.5.
func TestIOWave19Inspect(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "inspect.txt"))
	cases := []struct{ src, want string }{
		{fmt.Sprintf("f = File.open(%q, \"w\"); s = f.inspect; f.close; p s", p), fmt.Sprintf("\"#<File:%s>\"\n", p)},
		{fmt.Sprintf("f = File.open(%q, \"w\"); f.close; p f.inspect", p), fmt.Sprintf("\"#<File:%s (closed)>\"\n", p)},
		{`r, w = IO.pipe; fd = r.fileno; s = r.inspect; r.close; w.close; p s == "#<IO:fd #{fd}>"`, "true\n"},
		{`r, w = IO.pipe; r.close; s = r.inspect; w.close; p s`, "\"#<IO:(closed)>\"\n"},
		{`p $stdout.inspect`, "\"#<IO:<STDOUT>>\"\n"},
		{`p $stderr.inspect`, "\"#<IO:<STDERR>>\"\n"},
		{`p STDIN.inspect`, "\"#<IO:<STDIN>>\"\n"},
		{`p IO.instance_method(:inspect).owner`, "IO\n"},
		{`p STDIN.fileno`, "0\n"},
		{`p $stderr.fileno`, "2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestIOWave19CloseOnExec covers IO#close_on_exec? / #close_on_exec= (io.c
// rb_io_close_on_exec_p / _set): the default-set flag, clearing with false/nil,
// setting with a truthy value, the assignment's return value, and the closed-
// stream IOError on both. Asserted against MRI Ruby 4.0.5.
func TestIOWave19CloseOnExec(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "coe.txt"))
	setup := fmt.Sprintf("f = File.open(%q, \"w\"); ", p)
	cases := []struct{ src, want string }{
		{setup + `p f.close_on_exec?`, "true\n"}, // default set
		{setup + `f.close_on_exec = false; p f.close_on_exec?`, "false\n"},
		{setup + `f.close_on_exec = false; f.close_on_exec = :yes; p f.close_on_exec?`, "true\n"},
		{setup + `f.close_on_exec = nil; p f.close_on_exec?`, "false\n"},
		{setup + `p(f.close_on_exec = true)`, "true\n"}, // assignment returns its argument
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	for _, src := range []string{setup + `f.close; f.close_on_exec?`, setup + `f.close; f.close_on_exec = true`} {
		if cls, msg := evalErr(t, src); cls != "IOError" || msg != "closed stream" {
			t.Errorf("src=%q: got %s: %q", src, cls, msg)
		}
	}
}

// TestIOWave19Dup covers IO#dup (io.c rb_io_dup): a new descriptor distinct from
// the receiver, independent open/close state in both directions, autoclose and
// close-on-exec always set on the copy, and the closed-stream IOError. Asserted
// against MRI Ruby 4.0.5.
func TestIOWave19Dup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "dup.txt"))
	setup := fmt.Sprintf("f = File.open(%q, \"w+\"); ", p)
	cases := []struct{ src, want string }{
		{setup + `i = f.dup; p [i.class == f.class, i.fileno != f.fileno]`, "[true, true]\n"},
		{setup + `i = f.dup; i.close; p [i.closed?, f.closed?]`, "[true, false]\n"},
		{setup + `i = f.dup; f.close; p [i.closed?, f.closed?]`, "[false, true]\n"},
		{setup + `f.close_on_exec = false; d = f.dup; p d.close_on_exec?`, "true\n"},
		{setup + `f.autoclose = false; d = f.dup; p d.autoclose?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	if cls, msg := evalErr(t, setup+`f.close; f.dup`); cls != "IOError" || msg != "closed stream" {
		t.Errorf("dup on closed: got %s: %q", cls, msg)
	}
}

// TestIOWave19Initialize covers IO#initialize (io.c rb_io_initialize) and the
// shared ioAdoptDescriptor: Integer and #to_int fds reassociate the receiver;
// an explicit mode is honoured or rejected (EINVAL) against the descriptor's
// access half; append mode positions at end; and the arity / TypeError / EBADF /
// closed-stream errors. Asserted against MRI Ruby 4.0.5.
func TestIOWave19Initialize(t *testing.T) {
	dir := t.TempDir()
	c := filepath.ToSlash(filepath.Join(dir, "content.txt"))  // holds "abcd"
	tgt := filepath.ToSlash(filepath.Join(dir, "target.txt")) // the target IO's own file
	// A target IO plus a source read fd over the content file. The target opens
	// its own file so its "w" truncation never touches the content.
	base := fmt.Sprintf("File.write(%q, \"abcd\"); io = IO.new(File.open(%q, \"w\").fileno); rfd = File.open(%q, \"r\").fileno; ", c, tgt, c)
	cases := []struct{ src, want string }{
		{base + `io.send(:initialize, rfd, "r"); p io.fileno == rfd`, "true\n"},
		{base + `o = Object.new; def o.to_int; @f; end; o.instance_variable_set(:@f, rfd); io.send(:initialize, o, "r"); p io.fileno == rfd`, "true\n"},
		// no explicit mode inherits the descriptor's access half.
		{base + `io.send(:initialize, rfd); p io.read`, "\"abcd\"\n"},
		// append mode positions the cursor at end of the adopted buffer.
		{fmt.Sprintf("File.write(%q, \"abcd\"); io = IO.new(File.open(%q, \"w\").fileno); afd = File.open(%q, \"a\").fileno; io.send(:initialize, afd, \"a\"); p io.pos", c, tgt, c), "4\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	errCases := []struct{ src, class string }{
		{base + `io.send(:initialize)`, "ArgumentError"},                // too few
		{base + `io.send(:initialize, rfd, "r", "x")`, "ArgumentError"}, // too many positional
		{base + `io.send(:initialize, nil, "r")`, "TypeError"},
		{base + `io.send(:initialize, "4", "r")`, "TypeError"},
		{base + `io.send(:initialize, STDOUT, "r")`, "TypeError"},
		{base + `io.send(:initialize, -1, "r")`, "Errno::EBADF"},
		// an explicit write mode is incompatible with a read-only descriptor.
		{base + `io.send(:initialize, rfd, "w")`, "Errno::EINVAL"},
	}
	for _, c := range errCases {
		if cls, _ := evalErr(t, c.src); cls != c.class {
			t.Errorf("src=%q: got %s, want %s", c.src, cls, c.class)
		}
	}
	// A closed descriptor cannot be adopted.
	closedSrc := fmt.Sprintf("io = IO.new(File.open(%q, \"w\").fileno); g = File.open(%q, \"r\"); gfd = g.fileno; g.close; io.send(:initialize, gfd)", tgt, c)
	if cls, _ := evalErr(t, closedSrc); cls != "IOError" {
		t.Errorf("initialize on closed src: got %s", cls)
	}
}

// TestIOWave19SetEncoding covers IO#set_encoding (io.c io_encoding_set): a single
// Encoding, a "ext:int" String, a #to_str-coerced argument, the equal-external/
// internal drop (enc2=NULL), a separated internal argument, a trailing econv
// options Hash, and the 1..2 arity. Asserted against MRI Ruby 4.0.5.
func TestIOWave19SetEncoding(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "se.txt"))
	setup := fmt.Sprintf("File.write(%q, \"\"); f = File.open(%q, \"r+\"); ", p, p)
	cases := []struct{ src, want string }{
		{setup + `f.set_encoding(Encoding::UTF_8); p [f.external_encoding.name, f.internal_encoding]`, "[\"UTF-8\", nil]\n"},
		{setup + `f.set_encoding("utf-8:utf-16be"); p [f.external_encoding.name, f.internal_encoding.name]`, "[\"UTF-8\", \"UTF-16BE\"]\n"},
		{setup + `f.set_encoding("utf-8", "utf-16be"); p [f.external_encoding.name, f.internal_encoding.name]`, "[\"UTF-8\", \"UTF-16BE\"]\n"},
		// equal external/internal drops the internal encoding.
		{setup + `f.set_encoding(Encoding::UTF_8, Encoding::UTF_8); p [f.external_encoding.name, f.internal_encoding]`, "[\"UTF-8\", nil]\n"},
		{setup + `f.set_encoding("utf-8:utf-8"); p [f.external_encoding.name, f.internal_encoding]`, "[\"UTF-8\", nil]\n"},
		// a #to_str object naming "ext:int".
		{setup + `o = Object.new; def o.to_str; "utf-8:utf-16be"; end; f.set_encoding(o); p [f.external_encoding.name, f.internal_encoding.name]`, "[\"UTF-8\", \"UTF-16BE\"]\n"},
		// a trailing econv options Hash is accepted (and not counted as an encoding).
		{setup + `f.set_encoding(Encoding::EUC_JP, Encoding::SHIFT_JIS, invalid: :replace, replace: "."); p f.external_encoding.name`, "\"EUC-JP\"\n"},
		{setup + `p f.set_encoding(Encoding::UTF_8).equal?(f)`, "true\n"},
		{setup + `f.set_encoding(nil, nil); p [f.external_encoding, f.internal_encoding]`, "[nil, nil]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	errCases := []struct{ src, class string }{
		{setup + `f.set_encoding()`, "ArgumentError"},
		{setup + `f.set_encoding(1, 2, 3)`, "ArgumentError"},
		{setup + `f.set_encoding(Object.new)`, "TypeError"}, // no #to_str, not an Encoding
	}
	for _, c := range errCases {
		if cls, _ := evalErr(t, c.src); cls != c.class {
			t.Errorf("src=%q: got %s, want %s", c.src, cls, c.class)
		}
	}
}

// TestIOWave19StreamModes covers the standard streams' access halves (a read of
// $stdout / a write to $stdin raises IOError), IO#to_i's alias identity with
// #fileno, IO mixing in Enumerable, and IO#ungetbyte's Bignum-modulo-256 and
// #to_str coercion. Asserted against MRI Ruby 4.0.5.
func TestIOWave19StreamModes(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p IO.instance_method(:to_i) == IO.instance_method(:fileno)`, "true\n"},
		{`p IO.include?(Enumerable)`, "true\n"},
		// ungetbyte reduces a Bignum modulo 256 and coerces a #to_str object.
		{`require "stringio"; s = StringIO.new("a"); s.ungetbyte(0x4f7574206f6620636861722072616e67ff); p s.getbyte`, "255\n"},
		{`require "stringio"; s = StringIO.new("a"); o = Object.new; def o.to_str; "dog"; end; s.ungetbyte(o); p [s.getbyte, s.getbyte, s.getbyte]`, "[100, 111, 103]\n"},
		// a negative Integer also reduces into a byte (Ruby modulo).
		{`require "stringio"; s = StringIO.new("a"); s.ungetbyte(-1); p s.getbyte`, "255\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	errCases := []struct{ src, class, msg string }{
		{`STDOUT.read`, "IOError", "not opened for reading"},
		{`STDERR.readbyte`, "IOError", "not opened for reading"},
		{`STDIN.write("x")`, "IOError", "not opened for writing"},
		{`STDOUT.ungetbyte(42)`, "IOError", "not opened for reading"},
	}
	for _, c := range errCases {
		if cls, msg := evalErr(t, c.src); cls != c.class || msg != c.msg {
			t.Errorf("src=%q: got %s: %q, want %s: %q", c.src, cls, msg, c.class, c.msg)
		}
	}
}
