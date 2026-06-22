package vm_test

import (
	"strings"
	"testing"
)

// TestIO covers IO (the $stdout/$stderr/STDOUT/STDERR streams) and StringIO,
// asserted against MRI Ruby 4.0.5. The eval harness captures the VM's single
// output stream, so $stderr cases (which default to that stream) compare against
// what MRI writes to its stderr, and never mix stdout+stderr in one case.
func TestIO(t *testing.T) {
	cases := []struct{ src, want string }{
		// Standard streams.
		{`p [$stdout.class, STDOUT.class, $stderr.class]`, "[IO, IO, IO]\n"},
		{`p $stdout.equal?(STDOUT)`, "true\n"},
		{`p $stdout`, "#<IO:<STDOUT>>\n"},
		{`p STDOUT.write("hi")`, "hi2\n"},
		{`$stdout.puts("a", "b")`, "a\nb\n"},
		{`$stdout.puts`, "\n"},
		{`$stdout.puts(["x", ["y", "z"]])`, "x\ny\nz\n"},
		{`$stdout.print("a", "b")`, "ab"},
		{`$stdout << "x" << "y"`, "xy"},
		{`$stdout.printf("%d-%s\n", 3, "x")`, "3-x\n"},
		{`$stdout.putc(65); $stdout.putc("Z")`, "AZ"},
		{`p [$stdout.sync, ($stdout.sync = true), $stdout.sync]`, "[false, true, true]\n"},
		{`p [$stdout.flush.class, $stdout.binmode.class, $stdout.tty?, $stdout.isatty]`, "[IO, IO, false, false]\n"},
		{`p $stdout.closed?`, "false\n"},

		// $stderr (compared against MRI's stderr output).
		{`$stderr.puts("err")`, "err\n"},
		{`warn "w1", "w2"`, "w1\nw2\n"},
		{`$stderr.puts(warn("x").inspect)`, "x\nnil\n"},

		// StringIO — writing.
		{`require "stringio"; s = StringIO.new; s.write("ab"); s << "c"; p s.string`, "\"abc\"\n"},
		{`require "stringio"; s = StringIO.new; s.puts("x", "y"); p s.string`, "\"x\\ny\\n\"\n"},
		{`require "stringio"; s = StringIO.new; s.printf("%03d", 7); p s.string`, "\"007\"\n"},
		{`require "stringio"; p StringIO.new.fsync`, "0\n"},
		{`require "stringio"; p StringIO.new("ab").class`, "StringIO\n"},

		// StringIO — reading.
		{`require "stringio"; s = StringIO.new("hello"); p [s.read(3), s.read, s.read(1)]`, "[\"hel\", \"lo\", nil]\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.pos = 10; p s.read`, "\"\"\n"},
		{`require "stringio"; s = StringIO.new("a\nb\nc"); p [s.gets, s.gets, s.gets, s.gets]`, "[\"a\\n\", \"b\\n\", \"c\", nil]\n"},
		{`require "stringio"; s = StringIO.new("a;b"); p [s.gets(";"), s.gets(";")]`, "[\"a;\", \"b\"]\n"},
		{`require "stringio"; s = StringIO.new("héllo"); p [s.getc, s.getc]`, "[\"h\", \"é\"]\n"},
		{`require "stringio"; p StringIO.new("").getc`, "nil\n"},
		{`require "stringio"; s = StringIO.new("x\ny\n"); p s.readlines`, "[\"x\\n\", \"y\\n\"]\n"},
		{`require "stringio"; s = StringIO.new("p\nq\n"); a=[]; s.each_line { |l| a << l }; p a`, "[\"p\\n\", \"q\\n\"]\n"},
		{`require "stringio"; s = StringIO.new("ab"); r=[]; s.each_char { |c| r << c }; p r`, "[\"a\", \"b\"]\n"},
		{`require "stringio"; p StringIO.new("x").readline`, "\"x\"\n"},
		{`require "stringio"; s = StringIO.new("hi"); p [s.size, s.length, s.eof?, s.read, s.eof?, s.eof]`, "[2, 2, false, \"hi\", true, true]\n"},
		{`require "stringio"; s = StringIO.new("ab"); p [s.tell, s.pos]`, "[0, 0]\n"},
		{`require "stringio"; s = StringIO.new("hello"); s.pos = 2; p [s.pos, s.read]`, "[2, \"llo\"]\n"},
		{`require "stringio"; s = StringIO.new("hello"); s.seek(1); a=[s.pos]; s.seek(2,1); a<<s.pos; s.seek(-1,2); a<<s.pos; p a`, "[1, 3, 4]\n"},
		{`require "stringio"; s = StringIO.new("hello"); s.read; s.rewind; p s.read`, "\"hello\"\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.truncate(1); p s.string`, "\"a\"\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.truncate(4); p s.size`, "4\n"},
		{`require "stringio"; s = StringIO.new; s.close; p s.closed?`, "true\n"},

		// Reassigning $stdout to a StringIO captures Kernel#puts/print output.
		{`require "stringio"; $stdout = StringIO.new; puts "captured"; print "p"; o = $stdout.string; STDOUT.print(o.inspect)`, "\"captured\\np\""},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Coverage of the curStdout fallback: a $stdout rebound to a non-IO still
	// writes (to the underlying stream) rather than crashing. MRI raises here, so
	// this is an implementation behavior, not a differential case.
	if got := eval(t, `$stdout = Object.new; puts "fallback"`); got != "fallback\n" {
		t.Errorf("curStdout fallback = %q, want %q", got, "fallback\n")
	}

	errs := []struct{ src, want string }{
		{`$stdout.printf`, "expected 1+"},
		{`$stdout.putc([])`, "into Integer"},
		{`require "stringio"; StringIO.new(5)`, "into String"},
		{`require "stringio"; s = StringIO.new("x"); s.pos = "a"`, "into Integer"},
		{`require "stringio"; s = StringIO.new; s.close; s.write("x")`, "closed stream"},
		{`require "stringio"; StringIO.new("").readline`, "end of file"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
