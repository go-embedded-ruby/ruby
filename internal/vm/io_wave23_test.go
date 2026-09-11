// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// w23dir lays out the small fixture tree the reopen/gets tests read: a two-line
// file, a one-line file, and a UTF-8 file whose characters straddle any byte
// limit. It returns the forward-slashed directory so paths can be embedded in
// Ruby source directly.
func w23dir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("two.txt", []byte("Line 1\nLine 2\n"))
	write("one.txt", []byte("XX\n"))
	// "朝日" followed by 100 truncated three-byte leads — the fixture
	// core/io/gets_spec.rb builds to exercise the extra_limit relaxation.
	data := []byte("\xE6\x9C\x9D\xE6\x97\xA5")
	for i := 0; i < 100; i++ {
		data = append(data, 0xE3, 0x81)
	}
	write("kanji.txt", data)
	return slash(dir)
}

// TestIOReopenIOFormWave23 covers IO#reopen's other-IO form (io.c io_reopen):
// the class and path adopted from the target, reading from the target's current
// position, the #to_io conversion and its TypeError, the closed-stream IOErrors
// on either side, and reopening a stream onto itself. Every expected value was
// produced by MRI Ruby 4.0.5 on this host.
func TestIOReopenIOFormWave23(t *testing.T) {
	d := w23dir(t)
	cases := []struct{ src, want string }{
		// The receiver takes the target's class, path and content.
		{`f=File.open("` + d + `/one.txt"); g=File.open("` + d + `/two.txt")
p [f.reopen(g).class.to_s, f.path == g.path, f.gets]`,
			"[\"File\", true, \"Line 1\\n\"]\n"},
		// It starts at the target's current position, not at the beginning.
		{`f=File.open("` + d + `/one.txt"); g=File.open("` + d + `/two.txt"); g.gets
p f.reopen(g).gets`, "\"Line 2\\n\"\n"},
		// Reopening a stream onto itself is a no-op returning self.
		{`f=File.open("` + d + `/one.txt"); p f.reopen(f).equal?(f)`, "true\n"},
		// #to_io is called, and its IO adopted.
		{`f=File.open("` + d + `/one.txt"); g=File.open("` + d + `/two.txt")
o=Object.new; o.define_singleton_method(:to_io) { g }
p f.reopen(o).gets`, "\"Line 1\\n\"\n"},
		// A #to_io that answers something other than an IO is a TypeError.
		{`f=File.open("` + d + `/one.txt"); o=Object.new; def o.to_io; "s"; end
begin; f.reopen(o); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: can't convert Object to IO (Object#to_io gives String)\n"},
		// A #to_io answering nil is "not an IO": the argument becomes a path, and
		// an object that is no path either is the String TypeError.
		{`f=File.open("` + d + `/one.txt"); o=Object.new; def o.to_io; nil; end
begin; f.reopen(o); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: no implicit conversion of Object into String\n"},
		// Both a closed receiver and a closed target raise IOError.
		{`f=File.open("` + d + `/one.txt"); g=File.open("` + d + `/two.txt"); g.close
begin; f.reopen(g); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"IOError: closed stream\n"},
		{`f=File.open("` + d + `/one.txt"); f.close
begin; f.reopen(File.open("` + d + `/two.txt")); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"IOError: closed stream\n"},
		// A writable receiver flushes to its own file before the descriptor moves.
		{`f=File.open("` + d + `/w1.txt","w"); f.print "kept"
g=File.open("` + d + `/w2.txt","w"); f.reopen(g); f.print "moved"; f.flush
p [File.read("` + d + `/w1.txt"), File.read("` + d + `/w2.txt")]`,
			"[\"kept\", \"moved\"]\n"},
		// close-on-exec is set afresh on the reopened stream.
		{`f=File.open("` + d + `/one.txt"); f.close_on_exec=false
p f.reopen(File.open("` + d + `/two.txt")).close_on_exec?`, "true\n"},
		// No positional argument at all.
		{`f=File.open("` + d + `/one.txt")
begin; f.reopen; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: wrong number of arguments (given 0, expected 1..2)\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOReopenStandardStreamWave23 covers the standard-stream branch of
// io_reopen: MRI dup2()s onto descriptor 1 and leaves the FILE* in place, so
// STDOUT keeps its identity and only its destination moves — the shape
// Kernel#fork's snapshot of the redirection depends on.
func TestIOReopenStandardStreamWave23(t *testing.T) {
	src := `r, w = IO.pipe
$stdout.reopen(w)
print "redirected"
$stdout.reopen(STDERR)
p r.read_nonblock(20)`
	if got := runFS(t, src); got != "\"redirected\"\n" {
		t.Errorf("got %q", got)
	}
}

// TestIOReopenPathFormWave23 covers IO#reopen's path form (the freopen() half of
// rb_io_reopen): the mode carried over from the original open, an explicit mode
// argument, the same mode given as a :mode option, #to_path conversion,
// reopening a closed stream, and Errno::ENOENT for a read-only stream pointed at
// a file that does not exist. Values verified against MRI Ruby 4.0.5.
func TestIOReopenPathFormWave23(t *testing.T) {
	d := w23dir(t)
	cases := []struct{ src, want string }{
		// The write mode is kept, so the old file holds what was flushed to it and
		// the new one is created and written.
		{`f=File.open("` + d + `/p1.txt","w"); f.print "original data"
f.reopen("` + d + `/p2.txt"); f.print "new data"; f.flush
p [File.read("` + d + `/p1.txt"), File.read("` + d + `/p2.txt")]`,
			"[\"original data\", \"new data\"]\n"},
		// An explicit mode replaces it; "ab" shows through fcntl(F_GETFL).
		{`require "fcntl"
f=File.open("` + d + `/one.txt"); f.reopen("` + d + `/p3.txt","ab")
p (f.fcntl(Fcntl::F_GETFL) & File::APPEND) == File::APPEND`, "true\n"},
		// The mode may arrive as a :mode option instead.
		{`f=File.open("` + d + `/one.txt"); p f.reopen("` + d + `/two.txt", mode: "r").gets`,
			"\"Line 1\\n\"\n"},
		// A non-String argument goes through #to_path.
		{`f=File.open("` + d + `/one.txt")
o=Object.new; o.define_singleton_method(:to_path) { "` + d + `/two.txt" }
p f.reopen(o).gets`, "\"Line 1\\n\"\n"},
		// A closed stream reopens, and reads from the beginning.
		{`f=File.open("` + d + `/one.txt"); f.close; f.reopen("` + d + `/two.txt","r")
p [f.closed?, f.gets]`, "[false, \"Line 1\\n\"]\n"},
		// A read-only stream pointed at a missing file raises, as the open does.
		{`f=File.open("` + d + `/one.txt","r")
begin; f.reopen("` + d + `/absent.txt"); rescue => e; puts e.class; end`,
			"Errno::ENOENT\n"},
		// The encoding options of an explicit mode are adopted.
		{`f=File.open("` + d + `/one.txt")
f.reopen("` + d + `/two.txt", "r:ISO-8859-1")
p f.external_encoding.name`, "\"ISO-8859-1\"\n"},
		// close-on-exec is set afresh here too.
		{`f=File.open("` + d + `/one.txt"); f.close_on_exec=false
p f.reopen("` + d + `/two.txt").close_on_exec?`, "true\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOFcntlWave23 covers IO#fcntl and the Fcntl constant module: F_GETFL for
// each access mode, F_GETFD reading the close-on-exec flag, the two setters, an
// unsupported command and a closed stream. MRI Ruby 4.0.5 on this host answers
// 0 / 9 / 2 to the three F_GETFL calls below and 1 / 0 to the two F_GETFD calls.
//
// A mode carrying O_CREAT is deliberately not asserted: a real fcntl(F_GETFL) on
// a freshly created file returns host status bits on top of the access mode
// (65537 for "w" here) that a synthetic answer cannot reproduce. The access mode
// and O_APPEND — all the open-mode flags a caller can act on, and all
// core/io/reopen_spec.rb compares — are exact.
func TestIOFcntlWave23(t *testing.T) {
	d := w23dir(t)
	cases := []struct{ src, want string }{
		{`require "fcntl"
p [File.open("` + d + `/two.txt","r").fcntl(Fcntl::F_GETFL),
   File.open("` + d + `/f1.txt","ab").fcntl(Fcntl::F_GETFL),
   File.open("` + d + `/f1.txt","r+").fcntl(Fcntl::F_GETFL)]`,
			"[0, 9, 2]\n"},
		// The write-only access bit still shows through, whatever a host would add.
		{`require "fcntl"
p File.open("` + d + `/f2.txt","w").fcntl(Fcntl::F_GETFL) & 3`, "1\n"},
		{`require "fcntl"
f=File.open("` + d + `/two.txt")
a=f.fcntl(Fcntl::F_GETFD); f.close_on_exec=false
p [a, f.fcntl(Fcntl::F_GETFD), Fcntl::FD_CLOEXEC]`, "[1, 0, 1]\n"},
		{`require "fcntl"
f=File.open("` + d + `/two.txt")
p [f.fcntl(Fcntl::F_SETFD, 0), f.fcntl(Fcntl::F_SETFL, 0)]`, "[0, 0]\n"},
		// An unsupported command is refused. MRI's errno here is whatever the host
		// kernel picks for the command (EPERM on this macOS), which rbgo cannot
		// reproduce without a descriptor; it answers the canonical EINVAL instead.
		{`require "fcntl"
begin; File.open("` + d + `/two.txt").fcntl(99); rescue => e; puts e.class; end`,
			"Errno::EINVAL\n"},
		{`require "fcntl"
f=File.open("` + d + `/two.txt"); f.close
begin; f.fcntl(Fcntl::F_GETFL); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"IOError: closed stream\n"},
		{`require "fcntl"
begin; File.open("` + d + `/two.txt").fcntl; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: wrong number of arguments (given 0, expected 1..2)\n"},
		// The open-flag mirrors are present, and the flock operations are not.
		{`require "fcntl"
p [Fcntl::O_APPEND == File::APPEND, Fcntl::O_NONBLOCK == File::NONBLOCK,
   Fcntl.const_defined?(:O_LOCK_EX)]`, "[true, true, false]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFileOpenAppendCreatesWave23 covers the O_CREAT that FMODE_APPEND carries
// (io.c rb_io_fmode_oflags): File.open(path, "a") materialises the file before
// any write, which MRI Ruby 4.0.5 reports as File.exist? => true.
func TestFileOpenAppendCreatesWave23(t *testing.T) {
	d := w23dir(t)
	src := `p [File.exist?("` + d + `/ap.txt"),
   (File.open("` + d + `/ap.txt","a"){}; File.exist?("` + d + `/ap.txt"))]`
	if got := runFS(t, src); got != "[false, true]\n" {
		t.Errorf("got %q", got)
	}
	// A path whose parent does not exist cannot be created, and reports the
	// same Errno::ENOENT the read side of the open would.
	if got := runFSErr(t, `File.open("`+d+`/nodir/ap.txt","a")`); got != "Errno::ENOENT" {
		t.Errorf("append open in a missing directory: got %q", got)
	}
}

// TestGetsLimitCharBoundaryWave23 covers relaxGetsLimit / lastCharStart: a limit
// that lands inside a character is relaxed until the character is complete
// (io.c rb_io_getline_0), bounded by extra_limit = 16 and by the separator; a
// single-byte encoding is never relaxed; and a zero limit still returns "".
// Values verified against MRI Ruby 4.0.5.
func TestGetsLimitCharBoundaryWave23(t *testing.T) {
	d := w23dir(t)
	cases := []struct{ src, want string }{
		// One byte of a three-byte character reads the whole character.
		{`f=File.open("` + d + `/kanji.txt","r:utf-8"); p [f.gets(1), f.gets(1)]`,
			"[\"朝\", \"日\"]\n"},
		// A run of truncated leads consumes exactly the 16 extra bytes allowed.
		{`f=File.open("` + d + `/kanji.txt","r:utf-8"); p f.gets(7).bytesize`, "23\n"},
		// A limit falling on a boundary is left alone.
		{`f=File.open("` + d + `/kanji.txt","r:utf-8"); p f.gets(6).bytesize`, "6\n"},
		// A zero limit returns the empty string without relaxing anything.
		{`f=File.open("` + d + `/kanji.txt","r:utf-8"); p f.gets(0)`, "\"\"\n"},
		// A single-byte encoding has no character to split.
		{`f=File.open("` + d + `/kanji.txt","rb"); p f.gets(1).bytes`, "[230]\n"},
		// The relaxation stops at the separator rather than crossing it.
		{`f=File.open("` + d + `/two.txt"); p [f.gets(10), f.gets(10)]`,
			"[\"Line 1\\n\", \"Line 2\\n\"]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestForeachOpenOptionsWave23 covers ioOpenForeach / ioForeachMode: IO.foreach
// and IO.readlines honour :mode and :open_args rather than forcing "r"
// (io.c open_key_args), so a write mode creates the file and the read that
// follows raises IOError — MRI Ruby 4.0.5 raises IOError "not opened for
// reading" and leaves the file in place.
func TestForeachOpenOptionsWave23(t *testing.T) {
	d := w23dir(t)
	cases := []struct{ src, want string }{
		// No options at all: the plain read.
		{`p IO.readlines("` + d + `/two.txt")`, "[\"Line 1\\n\", \"Line 2\\n\"]\n"},
		// :mode "w" opens for writing, so the file appears and the read raises.
		{`begin; IO.readlines("` + d + `/m1.txt", mode: "w"); rescue => e; puts "#{e.class}: #{e.message}"; end
p File.exist?("` + d + `/m1.txt")`,
			"IOError: not opened for reading\ntrue\n"},
		{`begin; IO.foreach("` + d + `/m2.txt", 10, mode: "w") {}; rescue => e; puts e.class; end`,
			"IOError\n"},
		// :open_args supplies the mode instead.
		{`begin; IO.readlines("` + d + `/m3.txt", open_args: ["w"]); rescue => e; puts e.class; end`,
			"IOError\n"},
		// :open_args carrying the mode inside its options Hash.
		{`begin; IO.readlines("` + d + `/m4.txt", open_args: [{mode: "w"}]); rescue => e; puts e.class; end`,
			"IOError\n"},
		// :open_args with no mode at all falls back to reading.
		{`p IO.readlines("` + d + `/two.txt", open_args: [{}])`,
			"[\"Line 1\\n\", \"Line 2\\n\"]\n"},
		// A :mode option that is not a String is ignored, as ioOptMode reports none.
		{`p IO.readlines("` + d + `/two.txt", mode: nil)`,
			"[\"Line 1\\n\", \"Line 2\\n\"]\n"},
		// The limit relaxation reaches IO.readlines too.
		{`p IO.readlines("` + d + `/kanji.txt", 1)[0, 2]`, "[\"朝\", \"日\"]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOSelectChecksWave23 covers IO.select's argument handling (io.c
// rb_f_select / select_internal): the timeout is converted first, each set must
// be an Array, and each element goes through rb_io_get_io. Every message below
// is the one MRI Ruby 4.0.5 prints on this host.
func TestIOSelectChecksWave23(t *testing.T) {
	cases := []struct{ src, want string }{
		{`begin; IO.select; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: wrong number of arguments (given 0, expected 1..4)\n"},
		{`begin; IO.select([Object.new]); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: no implicit conversion of Object into IO\n"},
		{`o=Object.new; def o.to_io; nil; end
begin; IO.select([o]); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: can't convert Object to IO (Object#to_io gives NilClass)\n"},
		{`begin; IO.select(Object.new); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: wrong argument type Object (expected Array)\n"},
		{`begin; IO.select(nil, Object.new); rescue => e; puts e.class; end`, "TypeError\n"},
		{`begin; IO.select(nil, nil, Object.new); rescue => e; puts e.class; end`, "TypeError\n"},
		{`begin; IO.select(nil, nil, nil, Object.new); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"TypeError: can't convert Object into time interval\n"},
		{`begin; IO.select(nil, nil, nil, -5); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: time interval must not be negative\n"},
		{`begin; IO.select(nil, nil, nil, Float::NAN); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"RangeError: NaN out of Time range\n"},
		// The supplied object comes back, not its #to_io conversion.
		{`r,w=IO.pipe; w.write("x")
o=Object.new; o.define_singleton_method(:to_io) { r }
p IO.select([o]) == [[o], [], []]`, "true\n"},
		// An exception set is type-checked but never reported ready.
		{`r,w=IO.pipe; w.write("x"); p IO.select([r], nil, [r]) == [[r], [], []]`, "true\n"},
		// A pipe write end is not read-ready.
		{`r,w=IO.pipe; p IO.select([w], nil, nil, 0)`, "nil\n"},
		// Nothing ready with a zero timeout is nil.
		{`r,w=IO.pipe; p IO.select([r], nil, nil, 0)`, "nil\n"},
		// A nil timeout is accepted. MRI would block here forever waiting for the
		// writer; this VM has no concurrent writer to wait for, so it answers at
		// once — the one place where IO.select cannot mirror MRI.
		{`r,w=IO.pipe; p IO.select([r], nil, nil, nil)`, "nil\n"},
		// A pipe reader whose write end has closed is ready (the read sees EOF).
		{`r,w=IO.pipe; w.close; p IO.select([r]) == [[r], [], []]`, "true\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOSelectRegularFileWave23 covers ioSelectReadable's non-pipe branch: a
// regular file is always reported readable and writable, the way select(2)
// reports one. MRI Ruby 4.0.5 answers [[io], [io], []] for the same program.
func TestIOSelectRegularFileWave23(t *testing.T) {
	d := w23dir(t)
	src := `io = File.open("` + d + `/sel.txt", "w+")
r, w, e = IO.select([io], [io], nil, 0)
p [r == [io], w == [io], e]`
	if got := runFS(t, src); got != "[true, true, []]\n" {
		t.Errorf("got %q", got)
	}
}

// TestIONonblockWave23 covers `require "io/nonblock"` and the accessors it
// installs (ext/io/nonblock/nonblock.c). MRI Ruby 4.0.5 on this host reports
// [false, true, true] for a File and the two ends of IO.pipe.
func TestIONonblockWave23(t *testing.T) {
	d := w23dir(t)
	cases := []struct{ src, want string }{
		{`p require("io/nonblock")`, "true\n"},
		{`require "io/nonblock"
r,w=IO.pipe; f=File.open("` + d + `/two.txt")
p [f.nonblock?, r.nonblock?, w.nonblock?]`, "[false, true, true]\n"},
		{`require "io/nonblock"
f=File.open("` + d + `/two.txt")
f.nonblock = true; a = f.nonblock?
f.nonblock = false
p [a, f.nonblock?]`, "[true, false]\n"},
		// A non-blocking read or write leaves the descriptor non-blocking.
		{`require "io/nonblock"
r,w=IO.pipe; w.write("abc"); r.nonblock = false; r.read_nonblock(1)
p r.nonblock?`, "true\n"},
		{`require "io/nonblock"
r,w=IO.pipe; w.nonblock = false; w.write_nonblock("a")
p w.nonblock?`, "true\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOReadNonblockWave23 covers IO#read_nonblock in the order io.c
// io_read_nonblock fixes: the negative length first, then the output buffer,
// then the readability check, then the zero-length shortcut, and finally the
// would-block and end-of-file answers in both their raising and their
// exception: false forms. Values verified against MRI Ruby 4.0.5.
func TestIOReadNonblockWave23(t *testing.T) {
	cases := []struct{ src, want string }{
		{`r,w=IO.pipe
begin; r.read_nonblock(-1); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: negative length -1 given\n"},
		{`r,w=IO.pipe
begin; r.read_nonblock; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: wrong number of arguments (given 0, expected 1..2)\n"},
		{`r,w=IO.pipe; r.close
begin; r.read_nonblock(5); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"IOError: closed stream\n"},
		{`r,w=IO.pipe; p r.read_nonblock(0)`, "\"\"\n"},
		{`r,w=IO.pipe
begin; r.read_nonblock(5); rescue => e
  puts "#{e.class}: #{e.message}"; p [e.is_a?(IO::WaitReadable), e.is_a?(Errno::EAGAIN)]
end`,
			"IO::EAGAINWaitReadable: Resource temporarily unavailable - read would block\n[true, true]\n"},
		{`r,w=IO.pipe; p r.read_nonblock(5, exception: false)`, ":wait_readable\n"},
		{`r,w=IO.pipe; w.write("hello"); w.close
r.read_nonblock(5)
p r.read_nonblock(5, exception: false)`, "nil\n"},
		{`r,w=IO.pipe; w.write("hello"); w.close
r.read_nonblock(5)
begin; r.read_nonblock(5); rescue => e; puts e.class; end`, "EOFError\n"},
		// The output buffer is filled and returned, and emptied on the EOF path.
		{`r,w=IO.pipe; w.write("hello world"); w.close
buf = +"existing content"
out = r.read_nonblock(11, buf)
p [buf, out.equal?(buf)]`, "[\"hello world\", true]\n"},
		{`r,w=IO.pipe; w.close
buf = +"existing content"
begin; r.read_nonblock(1, buf); rescue => e; puts e.class; end
p buf`, "EOFError\n\"\"\n"},
		// A nil buffer argument is simply no buffer.
		{`r,w=IO.pipe; w.write("ab"); p r.read_nonblock(5, nil)`, "\"ab\"\n"},
		// IO::EWOULDBLOCKWaitReadable is the same class under a second name.
		{`p IO::EWOULDBLOCKWaitReadable.equal?(IO::EAGAINWaitReadable)`, "true\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOWriteNonblockWaitWritableWave23 covers the would-block answer of
// IO#write_nonblock: once the modelled pipe capacity is full the write raises
// IO::EAGAINWaitWritable (or answers :wait_writable), which is what MRI Ruby
// 4.0.5 raises when a real pipe is full.
func TestIOWriteNonblockWaitWritableWave23(t *testing.T) {
	cases := []struct{ src, want string }{
		{`r,w=IO.pipe
begin
  loop { w.write_nonblock("a"*10_000) }
rescue => e
  puts e.class; p [e.is_a?(IO::WaitWritable), e.is_a?(Errno::EAGAIN)]
end`, "IO::EAGAINWaitWritable\n[true, true]\n"},
		{`r,w=IO.pipe
loop { break if w.write_nonblock("a"*10_000, exception: false) == :wait_writable }
p w.write_nonblock("a"*10_000, exception: false)`, ":wait_writable\n"},
		{`p IO::EWOULDBLOCKWaitWritable.equal?(IO::EAGAINWaitWritable)`, "true\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSetEncodingNilResolvesDefaultsWave23 covers extIntToEnc / ioFmodeWritable:
// `set_encoding nil, nil` resolves through io.c rb_io_ext_int_to_enc against the
// defaults in force AT THAT MOMENT and freezes the answer, so a later change to
// Encoding.default_external cannot move it. Which answer it freezes depends on
// whether a default internal encoding was in force: with one, the pair is
// recorded and both accessors report it; without one, a defaulted external
// records nothing and a writable stream reports nil for both. Every expected
// line below was produced by MRI Ruby 4.0.5 on this host.
func TestSetEncodingNilResolvesDefaultsWave23(t *testing.T) {
	d := w23dir(t)
	reset := `Encoding.default_external = Encoding::UTF_8; Encoding.default_internal = nil
`
	bump := `Encoding.default_external = Encoding::IBM437; Encoding.default_internal = Encoding::IBM866
`
	cases := []struct{ src, want string }{
		// Defaults changed BEFORE the reset: the new pair is captured.
		{reset + `io = File.open("` + d + `/e1.txt", "w:utf-8:us-ascii")
` + bump + `io.set_encoding nil, nil
p [io.external_encoding, io.internal_encoding]`,
			"[#<Encoding:IBM437>, #<Encoding:IBM866>]\n"},
		// Defaults changed AFTER the reset: the stream keeps the nil it recorded.
		{reset + `io = File.open("` + d + `/e2.txt", "w:utf-8:us-ascii")
io.set_encoding nil, nil
` + bump + `p [io.external_encoding, io.internal_encoding]`,
			"[nil, nil]\n"},
		// Same for a stream that never named an encoding.
		{reset + `io = File.open("` + d + `/e3.txt", "w")
io.set_encoding nil, nil
` + bump + `p [io.external_encoding, io.internal_encoding]`,
			"[nil, nil]\n"},
		// A READABLE stream reports the external encoding rather than nil.
		{reset + `io = File.open("` + d + `/two.txt", "r")
` + bump + `io.set_encoding nil, nil
p [io.external_encoding, io.internal_encoding]`,
			"[#<Encoding:IBM437>, #<Encoding:IBM866>]\n"},
		// $stdout is writable even though it is not a file, so it resets to nil.
		{reset + `STDOUT.set_encoding(Encoding::US_ASCII, Encoding::ISO_8859_1)
STDOUT.set_encoding(nil, nil)
p [STDOUT.external_encoding, STDOUT.internal_encoding]`,
			"[nil, nil]\n"},
		// An internal encoding equal to the external needs no converter, so only
		// the external is recorded.
		{`Encoding.default_external = Encoding::IBM437; Encoding.default_internal = Encoding::IBM437
io = File.open("` + d + `/e4.txt", "w")
io.set_encoding nil, nil
p [io.external_encoding, io.internal_encoding]`,
			"[#<Encoding:IBM437>, nil]\n"},
		// A named internal encoding with a nil external takes the default external.
		{reset + `io = File.open("` + d + `/e5.txt", "w")
io.set_encoding nil, Encoding::IBM866
p [io.external_encoding.name, io.internal_encoding.name]`,
			"[\"UTF-8\", \"IBM866\"]\n"},
		// A BINARY default external suppresses the internal side entirely.
		{`Encoding.default_external = Encoding::BINARY; Encoding.default_internal = Encoding::IBM866
io = File.open("` + d + `/e6.txt", "w")
io.set_encoding nil, nil
p [io.external_encoding, io.internal_encoding]`,
			"[nil, nil]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOPopenWave23 covers IO.popen (io.c rb_io_s_popen / pipe_open): the stream
// it returns, the access halves the mode gives it, the block form that closes
// afterwards, $? and #pid, and the argument splitting. Every expected value was
// produced by MRI Ruby 4.0.5 on this host, EXCEPT what a child would read on its
// standard input: the child has already finished by the time the parent holds
// the stream (this VM's process model is synchronous), so a write cannot reach
// it. That limit is asserted here as rbgo's behaviour, not as MRI's.
func TestIOPopenWave23(t *testing.T) {
	if runtimeIsWasm() {
		t.Skip("no subprocesses under wasm")
	}
	cases := []struct {
		src, want string
		// childText marks a case whose expected value carries text the CHILD
		// process wrote. See the Windows note above the loop.
		childText bool
	}{
		{`io = IO.popen("echo foo", "r"); p [io.closed?, io.read]`,
			"[false, \"foo\\n\"]\n", true},
		// A read-only stream cannot be written, and the read still works after.
		{`io = IO.popen("echo foo", "r")
begin; io.write("bar"); rescue => e; puts e.class; end
p io.read`, "IOError\n\"foo\\n\"\n", true},
		// A write-only stream cannot be read.
		{`io = IO.popen("cat", "w")
begin; io.read; rescue => e; puts e.class; end
p io.write("bar")`, "IOError\n3\n", false},
		// A "+" mode is duplex: each half closes on its own, and the stream stays
		// open until both are shut.
		{`io = IO.popen("cat", "r+")
p [io.closed?, io.close_read, io.closed?, io.close_write, io.closed?]`,
			"[false, nil, false, nil, true]\n", false},
		// The block form yields the stream and closes it afterwards.
		{`v = IO.popen("echo blk", "r") { |io| io.read }
p v`, "\"blk\\n\"\n", true},
		// $? carries the child's status, and #pid reports it.
		{`io = IO.popen("echo hi", "r"); p [$?.class.to_s, io.pid.class.to_s, $?.exitstatus]`,
			"[\"Process::Status\", \"Integer\", 0]\n", false},
		// A leading environment Hash and a trailing options Hash are both peeled off
		// before the command is read.
		{`p IO.popen({"FOO" => "bar"}, "echo one").read`, "\"one\\n\"\n", true},
		{`p IO.popen("echo two", "r", err: [:child, :out]).read`, "\"two\\n\"\n", true},
		// "-" asks for a forked interpreter, which needs a working fork.
		{`begin; IO.popen("-"); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"NotImplementedError: fork() function is unimplemented on this machine\n", false},
		{`begin; IO.popen; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: wrong number of arguments (given 0, expected 1..2)", false},
		// An explicitly nil mode is the "r" default.
		{`p IO.popen("echo nil", nil).read`, "\"nil\\n\"\n", true},
		// A subclass receiver produces an instance of that subclass (popen_finish
		// does RBASIC_SET_CLASS(port, klass)).
		{`class MyIO < IO; end
p MyIO.popen("echo sub", "r").class.to_s`, "\"MyIO\"\n", false},
	}
	// On Windows the shell's own `echo` terminates its line with CRLF, and rbgo
	// hands those bytes to the reader untranslated: it has no text-mode newline
	// conversion on the read path, which is what would turn "foo\r\n" back into
	// "foo\n" the way MRI does there. That gap is real but unverified — there is
	// no Windows MRI on this host to compare against — so the cases whose expected
	// value carries the child's own line ending are left to the POSIX lanes rather
	// than asserted one way or the other here.
	for _, c := range cases {
		if c.childText && runtime.GOOS == "windows" {
			continue
		}
		got := runFS(t, c.src)
		if c.want[len(c.want)-1] != '\n' { // an arity message, compared by prefix
			if len(got) < len(c.want) || got[:len(c.want)] != c.want {
				t.Errorf("src=%q got=%q want prefix %q", c.src, got, c.want)
			}
			continue
		}
		if got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// The documented limit: bytes written to a duplex popen stream are counted but
	// cannot reach a child that has already run, so `cat` echoes nothing back.
	// MRI answers "12345\n" here; this asserts rbgo's synchronous model instead.
	if got := runFS(t, `io = IO.popen("cat", "r+"); io.puts "12345"; io.close_write; p io.read`); got != "\"\"\n" {
		t.Errorf("popen duplex write: got %q", got)
	}
}

// TestIOCloseHalfNonDuplexWave23 covers the non-duplex rule of #close_read /
// #close_write (io.c rb_io_close_read / rb_io_close_write): on a stream that is
// not duplexed, closing the read half of a WRITABLE stream — or the write half
// of a READABLE one — is an IOError, and closing the other half closes the whole
// stream. The pipe ends carry one half of the access mode each. Values verified
// against MRI Ruby 4.0.5.
func TestIOCloseHalfNonDuplexWave23(t *testing.T) {
	d := w23dir(t)
	cases := []struct{ src, want string }{
		{`io = File.open("` + d + `/c1.txt","w")
begin; io.close_read; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"IOError: closing non-duplex IO for reading\n"},
		{`io = File.open("` + d + `/c2.txt","w+")
begin; io.close_write; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"IOError: closing non-duplex IO for writing\n"},
		{`io = File.open("` + d + `/c3.txt","w"); io.close_write; p io.closed?`, "true\n"},
		{`io = File.open("` + d + `/two.txt","r"); io.close_read; p io.closed?`, "true\n"},
		// An already-closed stream answers nil to both, before any of that.
		{`io = IO.popen("cat","r+"); io.close; p [io.close_read, io.close_write]`,
			"[nil, nil]\n"},
		// Each pipe end carries one half of the mode.
		{`r, w = IO.pipe
begin; r.write("x"); rescue => e; puts e.class; end
begin; w.read; rescue => e; puts e.class; end
r.close_read; w.close_write
p [r.closed?, w.closed?]`, "IOError\nIOError\n[true, true]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// runtimeIsWasm reports whether the test binary runs on a target with no
// subprocesses, where IO.popen raises instead of running anything.
func runtimeIsWasm() bool { return runtime.GOARCH == "wasm" }

// TestPipeBrokenPipeWave23 covers pipeEndClosed and the Errno::EPIPE a write to
// a pipe whose read end has gone reports. core/io/shared/write.rb asserts it for
// #write, #syswrite and #write_nonblock alike; MRI 4.0.5 on this host raises
// Errno::EPIPE "Broken pipe" for all three, and does not die from SIGPIPE.
func TestPipeBrokenPipeWave23(t *testing.T) {
	cases := []struct{ src, want string }{
		{`r,w=IO.pipe; r.close
begin; w.write("x"); rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"Errno::EPIPE: Broken pipe\n"},
		{`r,w=IO.pipe; r.close
begin; w.syswrite("x"); rescue => e; puts e.class; end`, "Errno::EPIPE\n"},
		{`r,w=IO.pipe; r.close
begin; w.write_nonblock("x"); rescue => e; puts e.class; end`, "Errno::EPIPE\n"},
		// #close_read on the read end breaks the pipe just as #close does.
		{`r,w=IO.pipe; r.close_read
begin; w.write("x"); rescue => e; puts e.class; end`, "Errno::EPIPE\n"},
		// Closing the WRITE end is end-of-file for the reader, not a broken pipe.
		{`r,w=IO.pipe; w.write("ok"); w.close; p r.read`, "\"ok\"\n"},
		{`r,w=IO.pipe; w.write("ok"); w.close_write; p r.read`, "\"ok\"\n"},
		// A block-form pipe closes both ends behind it.
		{`r = nil; w = nil
IO.pipe { |a, b| r, w = a, b }
p [r.closed?, w.closed?]`, "[true, true]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOReopenNoRecordedModeWave23 covers ioReopenPath's two branches for a
// stream that was never opened from a path, which io.c rb_io_reopen treats
// differently: a standard stream has a FILE* and is freopen()ed with the modestr
// its fmode maps to (so $stdout.reopen CREATES the file and $stdin.reopen reads
// it), while a pipe end or bare descriptor wrapper is rb_sysopen()ed with the raw
// oflags — the access half only, no O_CREAT and no O_TRUNC. Every value below was
// produced by MRI Ruby 4.0.5 on this host.
func TestIOReopenNoRecordedModeWave23(t *testing.T) {
	d := w23dir(t)
	// A pipe write end keeps its writable half and does NOT truncate: writing two
	// bytes over "0123456789" leaves "AB23456789".
	if got := runFS(t, `File.write("`+d+`/ex.txt", "0123456789")
r, w = IO.pipe
w.reopen("`+d+`/ex.txt"); w.print "AB"; w.flush
p File.read("`+d+`/ex.txt")`); got != "\"AB23456789\"\n" {
		t.Errorf("pipe writer reopen: got %q", got)
	}
	// A pipe read end keeps its readable half.
	if got := runFS(t, `r, w = IO.pipe; r.reopen("`+d+`/two.txt"); p r.gets`); got != "\"Line 1\\n\"\n" {
		t.Errorf("pipe reader reopen: got %q", got)
	}
	// Neither carries O_CREAT, so a path that does not exist is Errno::ENOENT.
	if got := runFSErr(t, `r, w = IO.pipe; w.reopen("`+d+`/absent.txt")`); got != "Errno::ENOENT" {
		t.Errorf("pipe writer reopen onto a missing path: got %q", got)
	}
	// $stdin is read-only, and reopening it reads the file.
	if got := runFS(t, `$stdin.reopen("`+d+`/two.txt"); p $stdin.gets`); got != "\"Line 1\\n\"\n" {
		t.Errorf("$stdin reopen: got %q", got)
	}
	// $stdout is write-only, and reopening it onto a path that does not exist
	// still succeeds — freopen's "w" creates. Its output no longer reaches the
	// captured stream, so the file is read back through a second run's value.
	if got := runFS(t, `$stdout.reopen("`+d+`/so.txt")
print "via reopen"
$stdout.flush
$stdout.reopen(STDERR)
File.write("`+d+`/so_echo.txt", File.read("`+d+`/so.txt"))`); got != "" {
		t.Errorf("$stdout reopen: captured stdout should be empty, got %q", got)
	}
	if b, err := os.ReadFile(filepath.Join(filepath.FromSlash(d), "so_echo.txt")); err != nil || string(b) != "via reopen" {
		t.Errorf("$stdout reopen wrote %q (err %v), want %q", b, err, "via reopen")
	}
}

// TestIOClosedHalfClosesAndPidWave23 covers the remaining guards of the
// descriptor surface: #close_read / #close_write answer nil on an already-closed
// NON-duplex stream (io.c returns at fptr->fd < 0, before the non-duplex
// refusal), and IO#pid is nil for a stream with no child behind it and IOError
// once it is closed. MRI Ruby 4.0.5 on this host prints exactly these lines.
func TestIOClosedHalfClosesAndPidWave23(t *testing.T) {
	d := w23dir(t)
	cases := []struct{ src, want string }{
		{`f = File.open("` + d + `/two.txt"); f.close; p [f.close_read, f.close_write]`,
			"[nil, nil]\n"},
		{`f = File.open("` + d + `/cw.txt","w"); f.close; p [f.close_read, f.close_write]`,
			"[nil, nil]\n"},
		{`p File.open("` + d + `/two.txt").pid`, "nil\n"},
		{`f = File.open("` + d + `/two.txt"); f.close
begin; f.pid; rescue => e; puts "#{e.class}: #{e.message}"; end`,
			"IOError: closed stream\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
