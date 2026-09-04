package vm_test

import (
	"strings"
	"testing"
)

// TestStringIOResiduals covers the StringIO library methods brought in line with
// MRI Ruby 4.0.6 (stringio 3.2.0): allocate/#initialize/.open construction, the
// backing-string identity and in-place mutation semantics, #reopen, #string=,
// #each_codepoint, #seek/#pos=/#truncate argument coercion and bounds, the
// #sync/#binmode/#close_read/#close_write/#fcntl overrides, and the true aliases
// (#size/#tell/#eof/#isatty). Every expectation was checked against MRI 4.0.6.
func TestStringIOResiduals(t *testing.T) {
	const req = `require "stringio"; `
	cases := []struct{ src, want string }{
		// --- backing-string identity and in-place mutation ---------------------
		{req + `p StringIO.new(y = +"hi").string.equal?(y)`, "true\n"},
		{req + `s = StringIO.new(x = +"abcd"); s.write("XY"); p x`, "\"XYcd\"\n"},
		{req + `s = StringIO.new(x = +"123456789"); s.truncate(4); p x`, "\"1234\"\n"},
		// StringIO.open drops the backing string (#string ⇒ nil) after its block.
		{req + `io = nil; StringIO.open(+"z") { |s| io = s }; p io.string`, "nil\n"},
		// A real IO's #string reports its buffer view (rbgo shares the reader).
		{req + `p $stdin.string`, "\"\"\n"},

		// --- aliases (identical UnboundMethods) --------------------------------
		{req + `p StringIO.instance_method(:size) == StringIO.instance_method(:length)`, "true\n"},
		{req + `p StringIO.instance_method(:tell) == StringIO.instance_method(:pos)`, "true\n"},
		{req + `p StringIO.instance_method(:eof) == StringIO.instance_method(:eof?)`, "true\n"},
		{req + `p StringIO.instance_method(:isatty) == StringIO.instance_method(:tty?)`, "true\n"},
		{req + `s = StringIO.new("abcd"); p [s.size, s.length, s.pos, s.tell]`, "[4, 4, 0, 0]\n"},
		{req + `s = StringIO.new(""); p [s.eof?, s.eof, s.isatty]`, "[true, true, false]\n"},

		// --- pos= ---------------------------------------------------------------
		{req + `s = StringIO.new("hello"); s.pos = 2; p s.pos`, "2\n"},
		{req + `o = Object.new; def o.to_int; 3; end; s = StringIO.new("hello"); s.pos = o; p s.pos`, "3\n"},

		// --- seek ---------------------------------------------------------------
		{req + `s = StringIO.new("12345678"); a = (s.seek(2); s.pos); s.seek(1, IO::SEEK_CUR); b = s.pos; s.seek(-2, IO::SEEK_END); p [a, b, s.pos]`,
			"[2, 3, 6]\n"},
		{req + `o = Object.new; def o.to_int; 2; end; s = StringIO.new("12345678"); s.seek(o); p s.pos`, "2\n"},

		// --- rewind resets position and line number ----------------------------
		{req + `s = StringIO.new("a\nb\n"); s.gets; s.rewind; p [s.pos, s.lineno]`, "[0, 0]\n"},

		// --- truncate -----------------------------------------------------------
		{req + `s = StringIO.new(+"12345"); s.truncate(2); p s.string`, "\"12\"\n"},
		{req + `s = StringIO.new(+"12"); s.truncate(4); p s.string.bytes`, "[49, 50, 0, 0]\n"},
		{req + `o = Object.new; def o.to_int; 2; end; s = StringIO.new(+"12345"); s.truncate(o); p s.string`, "\"12\"\n"},

		// --- mode: string, integer flags, #to_str coercion ---------------------
		{req + `p StringIO.new("x", mode: "r").closed_write?`, "true\n"},
		{req + `o = Object.new; def o.to_str; "r"; end; p StringIO.new(+"x", o).closed_write?`, "true\n"},
		{req + `p StringIO.new(+"data", IO::WRONLY).closed_read?`, "true\n"},
		{req + `p StringIO.new(+"data", IO::RDWR).string`, "\"data\"\n"},
		{req + `p StringIO.new(+"data", IO::RDONLY).closed_write?`, "true\n"},
		{req + `p StringIO.new(+"data", IO::WRONLY | IO::TRUNC).string`, "\"\"\n"},
		{req + `s = StringIO.new(+"data", IO::WRONLY | IO::APPEND); s.write("X"); p s.string`, "\"dataX\"\n"},
		// binary detection: a "b" mode flag or a :binmode option sets ASCII-8BIT.
		{req + `p [StringIO.new(+"", "wb").external_encoding.to_s, StringIO.new(+"", "w", binmode: true).external_encoding.to_s]`,
			"[\"ASCII-8BIT\", \"ASCII-8BIT\"]\n"},
		// :textmode / :encoding options are accepted (no b/t or ":" conflict here).
		{req + `p [StringIO.new(+"", "w", textmode: true).closed_read?, StringIO.new(+"", "w", encoding: "UTF-8").closed_read?]`,
			"[true, true]\n"},

		// --- string backend defaults -------------------------------------------
		{req + `p StringIO.new("f".freeze).closed_write?`, "true\n"},
		{req + `p StringIO.new(+"m").closed_write?`, "false\n"},
		{req + `o = Object.new; def o.to_str; "hi"; end; p StringIO.new(o).string`, "\"hi\"\n"},
		{req + `p StringIO.new.string`, "\"\"\n"},

		// --- allocate + private #initialize ------------------------------------
		{req + `io = StringIO.allocate; io.send(:initialize, +"ex", "r"); p [io.string, io.closed_write?]`,
			"[\"ex\", true]\n"},
		{req + `p StringIO.private_instance_methods(false).include?(:initialize)`, "true\n"},

		// --- open ---------------------------------------------------------------
		{req + `p StringIO.open("x") { 42 }`, "42\n"},
		{req + `p StringIO.open("x").string`, "\"x\"\n"},
		{req + `io = nil; (StringIO.open(+"x") { |s| io = s; raise "e" } rescue nil); p [io.closed?, io.string]`,
			"[true, nil]\n"},

		// --- string= ------------------------------------------------------------
		{req + `s = StringIO.new("a\nb"); s.pos = 2; s.lineno = 1; r = (s.string = "new"); p [r, s.string, s.pos, s.lineno]`,
			"[\"new\", \"new\", 0, 0]\n"},
		{req + `o = Object.new; def o.to_str; "cvt"; end; s = StringIO.new("x"); s.string = o; p s.string`, "\"cvt\"\n"},

		// --- reopen -------------------------------------------------------------
		{req + `s = StringIO.new("orig"); s.read(2); s.reopen("new", "r"); p [s.string, s.pos, s.closed_write?]`,
			"[\"new\", 0, true]\n"},
		{req + `s = StringIO.new("x"); s.close; s.reopen; p [s.closed_read?, s.closed_write?]`, "[false, false]\n"},
		{req + `s = StringIO.new("x"); s.reopen(+"reop"); p s.string`, "\"reop\"\n"},
		{req + `a = StringIO.new("src"); b = StringIO.new("x"); b.reopen(a); p b.string`, "\"src\"\n"},
		{req + `o = Object.new; def o.to_strio; StringIO.new("conv"); end; b = StringIO.new("x"); b.reopen(o); p b.string`,
			"\"conv\"\n"},

		// --- each_codepoint -----------------------------------------------------
		{req + `r = []; StringIO.new("ab").each_codepoint { |c| r << c }; p r`, "[97, 98]\n"},
		{req + `p StringIO.new("ab").each_codepoint.to_a`, "[97, 98]\n"},
		{req + `s = StringIO.new("a"); p s.each_codepoint { |c| }.equal?(s)`, "true\n"},

		// --- sync / binmode -----------------------------------------------------
		{req + `s = StringIO.new(+""); a = s.sync; s.sync = false; p [a, s.sync]`, "[true, true]\n"},
		{req + `s = StringIO.new(+""); s.binmode; p s.external_encoding.to_s`, "\"ASCII-8BIT\"\n"},

		// --- close_read / close_write -------------------------------------------
		{req + `s = StringIO.new(+"x"); p [s.close_read, s.close_read]`, "[nil, nil]\n"},
		{req + `s = StringIO.new(+"x"); p s.close_write`, "nil\n"},
		{req + `s = StringIO.new(+"x"); s.close_read; s.close_write; p s.closed?`, "true\n"},

		// --- Enumerable mixin ---------------------------------------------------
		{req + `p StringIO.include?(Enumerable)`, "true\n"},
		{req + `p StringIO.new("a\nb\n").map { |l| l.chomp }`, "[\"a\", \"b\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Error cases: the class of the raised exception (or its message) as checked
	// against MRI 4.0.6.
	errs := []struct{ src, want string }{
		{req + `StringIO.new("x").pos = -1`, "Invalid argument"},
		{req + `StringIO.new("x").seek(0, 3)`, "Invalid argument"},
		{req + `StringIO.new("x").seek(-5)`, "Invalid argument"},
		{req + `s = StringIO.new("x"); s.close; s.seek(0)`, "closed stream"},
		{req + `StringIO.new(+"x").truncate(-1)`, "Invalid argument"},
		{req + `StringIO.new("x", "r").truncate(0)`, "not opened for writing"},
		{req + `StringIO.new(+"x", Object.new)`, "no implicit conversion"},
		{req + `StringIO.new(Object.new)`, "no implicit conversion"},
		{req + `StringIO.new("x").reopen(Object.new)`, "no implicit conversion of Object into StringIO"},
		{req + `StringIO.new("f".freeze, "w")`, "Permission denied"},
		{req + `StringIO.new("f".freeze, IO::TRUNC)`, "FrozenError"},
		{req + `StringIO.allocate.string`, "uninitialized stream"},
		{req + `StringIO.new(+"", "w:UTF-8", encoding: "UTF-8")`, "encoding specified twice"},
		{req + `StringIO.new(+"", "wb", binmode: true)`, "mode specified twice"},
		{req + `StringIO.new(+"", "w", binmode: true, textmode: true)`, "both textmode and binmode"},
		{req + `StringIO.new("x", "w").each_codepoint { |c| }`, "not opened for reading"},
		{req + `StringIO.new("λ").tap { |s| s.pos = 1 }.each_codepoint { |c| }`, "invalid byte sequence"},
		{req + `StringIO.new(+"x", "w").close_read`, "not opened for reading"},
		{req + `StringIO.new(+"x", "r").close_write`, "not opened for writing"},
		{req + `StringIO.new("x").fcntl(1, 1)`, "unimplemented"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
