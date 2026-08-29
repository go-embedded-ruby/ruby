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
		{with(`a = ARGF.class.new(f1); a.read; p a.read`), "\"\"\n"},
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
		{with(`a = ARGF.class.new(f1); a.read; p a.eof?`), "true\n"},
		{with(`a = ARGF.class.new(f1, f2); a.gets; p [a.filename == f1, a.path == f1]`), "[true, true]\n"},
		{with(`a = ARGF.class.new(f1); p a.to_io.is_a?(IO)`), "true\n"},
		{with(`a = ARGF.class.new(f1); a.lineno = 10; p a.lineno`), "10\n"},
		{with(`a = ARGF.class.new(f1, f2); a.gets; a.skip; p a.gets`), "\"l3\\n\"\n"},
		{with(`a = ARGF.class.new(f1); a.close; p a.eof?`), "true\n"},
		{with(`a = ARGF.class.new(f1); p a.binmode.equal?(a)`), "true\n"},
		{with(`a = ARGF.class.new(f1); p [a.inspect, a.to_s]`), "[\"ARGF\", \"ARGF\"]\n"},
		// The singleton ARGF draws from ARGV, and #argv returns it.
		{with(`ARGV.replace([f1, f2]); p [ARGF.read, ARGF.argv]`), "[\"l1\\nl2\\nl3\\n\", []]\n"},
		{`p ARGF.class.new.class.name`, "\"ARGF.class\"\n"},
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
		{with(`a = ARGF.class.new(f1); a.read; p a.to_io`), "nil\n"},
		{with(`p ARGF.class.new(f1).filename == f1`), "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
