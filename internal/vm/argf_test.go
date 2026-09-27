// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestARGF covers the ARGF reading protocol over a fresh ARGF.class.new(*files)
// instance (files created under Dir.mktmpdir for portability) and the singleton
// ARGF drawing from ARGV. Asserted against MRI Ruby 4.0.6.
func TestARGF(t *testing.T) {
	// setup builds two files "l1\nl2\n" and "l3\n" and binds them into `code`.
	with := func(code string) string {
		return `require "tmpdir"
Dir.mktmpdir do |d|
  f1 = File.join(d, "a"); f2 = File.join(d, "b")
  File.write(f1, "l1\nl2\n"); File.write(f2, "l3\n")
  ` + code + `
end`
	}
	cases := []struct{ src, want string }{
		// read: whole stream, a length, then nil past EOF.
		{with(`a = ARGF.class.new(f1, f2); p a.read`), "\"l1\\nl2\\nl3\\n\"\n"},
		{with(`a = ARGF.class.new(f1, f2); p [a.read(3), a.read(3)]`), "[\"l1\\n\", \"l2\\n\"]\n"},
		{with(`a = ARGF.class.new(f1); a.read; p a.read(5)`), "nil\n"},
		{with(`a = ARGF.class.new(f1); a.read; p a.read`), "nil\n"},
		// gets / lineno / readline / readlines / each_line.
		{with(`a = ARGF.class.new(f1, f2); ls = []; while l = a.gets; ls << l; end; p [ls, a.lineno]`), "[[\"l1\\n\", \"l2\\n\", \"l3\\n\"], 3]\n"},
		{with(`a = ARGF.class.new(f1, f2); p a.readlines`), "[\"l1\\n\", \"l2\\n\", \"l3\\n\"]\n"},
		{with(`a = ARGF.class.new(f1); p a.to_a`), "[\"l1\\n\", \"l2\\n\"]\n"},
		{with(`a = ARGF.class.new(f1); out = []; a.each_line { |l| out << l.chomp }; p out`), "[\"l1\", \"l2\"]\n"},
		{with(`a = ARGF.class.new(f1); p a.each_line.class`), "Enumerator\n"},
		{with(`a = ARGF.class.new(f2); p [a.readline, (a.readline rescue :eof)]`), "[\"l3\\n\", :eof]\n"},
		// getc / readchar.
		{with(`a = ARGF.class.new(f2); p [a.getc, a.getc]`), "[\"l\", \"3\"]\n"},
		{with(`a = ARGF.class.new(f2); a.read; p (a.readchar rescue :eof)`), ":eof\n"},
		// eof?, filename/path, to_io, lineno=, skip, close, binmode, inspect.
		{with(`a = ARGF.class.new(f1); a.read; p((a.eof? rescue $!.class))`), "IOError\n"},
		{with(`a = ARGF.class.new(f1, f2); a.gets; p [a.filename == f1, a.path == f1]`), "[true, true]\n"},
		{with(`a = ARGF.class.new(f1); p a.to_io.is_a?(IO)`), "true\n"},
		{with(`a = ARGF.class.new(f1); a.lineno = 10; p a.lineno`), "10\n"},
		{with(`a = ARGF.class.new(f1, f2); a.gets; a.skip; p a.gets`), "\"l3\\n\"\n"},
		{with(`a = ARGF.class.new(f1); a.close; p((a.eof? rescue $!.class))`), "IOError\n"},
		{with(`a = ARGF.class.new(f1); p a.binmode.equal?(a)`), "true\n"},
		{with(`a = ARGF.class.new(f1); p [a.inspect, a.to_s]`), "[\"ARGF\", \"ARGF\"]\n"},
		// The singleton ARGF draws from ARGV, and #argv returns it.
		{with(`ARGV.replace([f1, f2]); p [ARGF.read, ARGF.argv]`), "[\"l1\\nl2\\nl3\\n\", []]\n"},
		{`p ARGF.class.new.class.name`, "\"ARGF.class\"\n"},
		// each_char / each_byte / each_codepoint, with a block and as Enumerators.
		{with(`a = ARGF.class.new(f2); p a.each_char.to_a`), "[\"l\", \"3\", \"\\n\"]\n"},
		{with(`a = ARGF.class.new(f2); out = []; a.each_char { |c| out << c }; p out`), "[\"l\", \"3\", \"\\n\"]\n"},
		{with(`a = ARGF.class.new(f2); p a.each_byte.to_a`), "[108, 51, 10]\n"},
		{with(`a = ARGF.class.new(f2); p a.each_codepoint.to_a`), "[108, 51, 10]\n"},
		// getbyte / readbyte.
		{with(`a = ARGF.class.new(f2); p [a.getbyte, a.getbyte]`), "[108, 51]\n"},
		{with(`a = ARGF.class.new(f2); a.read; p (a.readbyte rescue :eof)`), "nil\n"},
		// file: the current file's IO, nil once every input is consumed.
		{with(`a = ARGF.class.new(f1); p a.file.is_a?(IO)`), "true\n"},
		{with(`a = ARGF.class.new(f1); a.close; p [a.file.class, a.file.closed?]`), "[File, true]\n"},
		{with(`p ARGF.class.new(f2).readbyte`), "108\n"},
		// eof / path / to_a / each are true aliases of eof? / filename / readlines /
		// each_line.
		{`p [ARGF.class.instance_method(:eof) == ARGF.class.instance_method(:eof?), ARGF.class.instance_method(:path) == ARGF.class.instance_method(:filename), ARGF.class.instance_method(:to_a) == ARGF.class.instance_method(:readlines)]`, "[true, true, true]\n"},
		// Native display / truthiness (ToS via interpolation, Inspect via Array, Truthy).
		{with(`p "x#{ARGF.class.new(f1)}"`), "\"xARGF\"\n"},
		{with(`p [ARGF.class.new(f1)]`), "[ARGF]\n"},
		{`p(ARGF ? :y : :n)`, ":y\n"},
		// A non-String filename is coerced with #to_s.
		{with(`class P; def initialize(p); @p = p; end; def to_s; @p; end; end; p ARGF.class.new(P.new(f1)).read`), "\"l1\\nl2\\n\"\n"},
		// The singleton falling back to (empty, in-test) $stdin when ARGV is empty.
		{`ARGV.replace([]); p ARGF.gets`, "nil\n"},
		// read(len) spanning a file boundary; readchar returning a char; to_io and
		// filename at their edges.
		{with(`a = ARGF.class.new(f2, f1); p a.read(5)`), "\"l3\\nl1\"\n"},
		{with(`p ARGF.class.new(f2).readchar`), "\"l\"\n"},
		{with(`a = ARGF.class.new(f1); a.read; p [a.to_io.class, a.to_io.closed?]`), "[File, true]\n"},
		{with(`p ARGF.class.new(f1).filename == f1`), "true\n"},
		// pos / tell / seek / rewind / fileno / encodings delegate to the current file.
		{with(`a = ARGF.class.new(f1); a.gets; p [a.pos, a.tell]`), "[3, 3]\n"},
		{with(`a = ARGF.class.new(f1); a.gets; a.rewind; p [a.gets, a.lineno]`), "[\"l1\\n\", 1]\n"},
		{with(`a = ARGF.class.new(f2); a.seek(1); p a.read`), "\"3\\n\"\n"},
		{with(`a = ARGF.class.new(f1); p a.readpartial(3)`), "\"l1\\n\"\n"},
		{with(`a = ARGF.class.new(f2); a.pos = 2; p a.read`), "\"\\n\"\n"},
		{with(`a = ARGF.class.new(f1); p [a.fileno.is_a?(Integer), a.to_i.is_a?(Integer)]`), "[true, true]\n"},
		{with(`a = ARGF.class.new(f1); a.set_encoding("UTF-8", "ISO-8859-1"); p [a.external_encoding.to_s, a.internal_encoding.to_s]`), "[\"UTF-8\", \"ISO-8859-1\"]\n"},
		// --- MRI's file-switching state, which rbgo did not model before. Every
		// `want` below was measured against MRI 4.0.5 (ruby 4.0.5 +PRISM) with the
		// identical snippet; the six expectations corrected above were pinning
		// rbgo's own divergence, not MRI.
		//
		// #eof? is about the CURRENT FILE, so it is true at the end of each in turn.
		{with(`a = ARGF.class.new(f1); p [a.gets, a.eof?, a.gets, a.eof?]`), "[\"l1\\n\", false, \"l2\\n\", true]\n"},
		// #filename names the file being read, and only moves when a read does.
		{with(`a = ARGF.class.new(f1, f2); p [a.gets, a.filename == f1, a.gets, a.gets, a.filename == f2]`), "[\"l1\\n\", true, \"l2\\n\", \"l3\\n\", true]\n"},
		// #closed? and the IO #close actually closes.
		{with(`a = ARGF.class.new(f1); p [a.closed?, (a.read; a.closed?)]`), "[false, true]\n"},
		{with(`a = ARGF.class.new(f1); io = a.to_io; a.close; p io.closed?`), "true\n"},
		{with(`a = ARGF.class.new(f1); a.close; a.close; p :ok`), ":ok\n"},
		{with(`ARGF.class.new(f1).skip; p :ok`), ":ok\n"},
		// #binmode? and binmode reaching the stream's encoding.
		{with(`a = ARGF.class.new(f1); p a.binmode?`), "false\n"},
		{with(`a = ARGF.class.new(f1); a.binmode; p [a.binmode?, a.gets.encoding.to_s]`), "[true, \"ASCII-8BIT\"]\n"},
		// #readpartial stops at each file boundary: "" there, EOFError on the last.
		{with(`a = ARGF.class.new(f1, f2); p [a.readpartial(6), a.readpartial(1), a.readpartial(3)]`), "[\"l1\\nl2\\n\", \"\", \"l3\\n\"]\n"},
		{with(`a = ARGF.class.new(f1, f2); a.readpartial(6); a.readpartial(1); a.readpartial(3); p((a.readpartial(1) rescue $!.class))`), "EOFError\n"},
		{with(`a = ARGF.class.new(f1); a.read; b = +"zz"; (a.readpartial(1, b) rescue nil); p b`), "\"\"\n"},
		// #read_nonblock, all four outcomes plus the ARGF-only "" at a boundary.
		{with(`a = ARGF.class.new(f1); p a.read_nonblock(3)`), "\"l1\\n\"\n"},
		{with(`a = ARGF.class.new(f1, f2); a.read_nonblock(6); p a.read_nonblock(2)`), "\"\"\n"},
		{with(`a = ARGF.class.new(f1); a.read_nonblock(6); p((a.read_nonblock(1) rescue $!.class))`), "EOFError\n"},
		{with(`a = ARGF.class.new(f1); a.read_nonblock(6); p a.read_nonblock(1, nil, exception: false)`), "nil\n"},
		{with(`a = ARGF.class.new(f1); p((a.read_nonblock(-1) rescue $!.class))`), "ArgumentError\n"},
		// A pipe with its write end open is "no data yet", NOT end of file — and
		// exception: false answers :wait_readable there while answering nil at EOF.
		{with(`r, w = IO.pipe; $stdin = r; p((ARGF.class.new("-").read_nonblock(4) rescue $!.class))`), "IO::EAGAINWaitReadable\n"},
		{with(`r, w = IO.pipe; $stdin = r; p ARGF.class.new("-").read_nonblock(4, nil, exception: false)`), ":wait_readable\n"},
		{with(`r, w = IO.pipe; $stdin = r; w.write("abcd"); p ARGF.class.new("-").read_nonblock(4)`), "\"abcd\"\n"},
		// A lone "-" names $stdin rather than a path.
		{`p ARGF.class.new("-").gets`, "nil\n"},
		// Output buffers on #read and #read_nonblock.
		{with(`a = ARGF.class.new(f1); b = +"zz"; a.read(3, b); p b`), "\"l1\\n\"\n"},
		{with(`a = ARGF.class.new(f1); b = +"zz"; a.read_nonblock(3, b); p b`), "\"l1\\n\"\n"},
		// $_ is set by #gets and left alone by #readlines (argf_gets calls
		// rb_lastline_set; argf_getline, which #readlines uses, does not).
		{with(`a = ARGF.class.new(f1, f2); p [a.gets, $_]`), "[\"l1\\n\", \"l1\\n\"]\n"},
		{with(`a = ARGF.class.new(f1); a.readlines; p $_`), "nil\n"},
		// Enumerable, and enumerators with no size.
		{with(`a = ARGF.class.new(f1); p a.is_a?(Enumerable)`), "true\n"},
		{with(`a = ARGF.class.new(f1); p [a.each_line.size, a.each_byte.size, a.each_char.size, a.each_codepoint.size]`), "[nil, nil, nil, nil]\n"},
		// tell/pos, to_i/fileno and inspect/to_s are the SAME method, not two with
		// the same body.
		{`p [ARGF.class.instance_method(:tell) == ARGF.class.instance_method(:pos), ARGF.class.instance_method(:to_i) == ARGF.class.instance_method(:fileno), ARGF.class.instance_method(:inspect) == ARGF.class.instance_method(:to_s)]`, "[true, true, true]\n"},
		// #readbyte is not the byte twin of #readchar: with no stream at all it
		// returns nil where #readchar raises (NEXT_ARGF_FORWARD vs rb_eof_error).
		{with(`a = ARGF.class.new(f1); a.read; p [(a.readchar rescue :eof), a.readbyte]`), "[:eof, nil]\n"},
		// After close there is no stream, so a delegating call raises.
		{with(`a = ARGF.class.new(f1); a.close; p((a.pos rescue :err))`), ":err\n"},
		{with(`a = ARGF.class.new(f1); a.close; p((a.rewind rescue :err))`), ":err\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
