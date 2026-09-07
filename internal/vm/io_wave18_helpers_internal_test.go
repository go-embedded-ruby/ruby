// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// assertIORaise runs fn and asserts it panics with a RubyError of the given class.
func assertIORaise(t *testing.T, wantClass string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("expected %s, got no panic", wantClass)
			return
		}
		re, ok := r.(RubyError)
		if !ok {
			panic(r) // not a Ruby raise — re-panic
		}
		if re.Class != wantClass {
			t.Errorf("raised %s, want %s", re.Class, wantClass)
		}
	}()
	fn()
}

// TestSeekWhenceMapping covers seekWhence: each symbolic whence (:SET/:CUR/:END/
// :DATA/:HOLE) maps to its io.c constant, an Integer whence is coerced through,
// and any other symbol raises TypeError (MRI: "no implicit conversion of Symbol
// into Integer"). Verified byte-for-byte vs MRI 4.0.5.
func TestSeekWhenceMapping(t *testing.T) {
	vm := New(io.Discard)
	for _, c := range []struct {
		sym  string
		want int
	}{{"SET", 0}, {"CUR", 1}, {"END", 2}, {"DATA", 3}, {"HOLE", 4}} {
		if got := vm.seekWhence(object.Symbol(c.sym)); got != c.want {
			t.Errorf("seekWhence(:%s) = %d, want %d", c.sym, got, c.want)
		}
	}
	if got := vm.seekWhence(object.IntValue(7)); got != 7 {
		t.Errorf("seekWhence(7) = %d, want 7", got)
	}
	assertIORaise(t, "TypeError", func() { vm.seekWhence(object.Symbol("NOPE")) })
}

// TestIOStringIOClosedGuards covers ioIsStringIO and ioClosedRealIO: a StringIO
// tolerates operations on a closed stream (MRI keeps its buffer), while a real
// IO/File raises IOError "closed stream"; an open real IO does not.
func TestIOStringIOClosedGuards(t *testing.T) {
	vm := New(io.Discard)
	sio := vm.consts["StringIO"].(*RClass)
	rio := vm.consts["IO"].(*RClass)

	if !ioIsStringIO(&IOObj{cls: sio}) {
		t.Error("ioIsStringIO(StringIO) = false, want true")
	}
	if ioIsStringIO(&IOObj{cls: rio}) {
		t.Error("ioIsStringIO(IO) = true, want false")
	}

	assertIORaise(t, "IOError", func() { ioClosedRealIO(&IOObj{cls: rio, closed: true}) })
	ioClosedRealIO(&IOObj{cls: sio, closed: true})  // StringIO closed: tolerated (no panic)
	ioClosedRealIO(&IOObj{cls: rio, closed: false}) // real IO open: no panic
}

// TestIOWave18RubyPaths exercises the dispatch-driven read/write/seek helpers
// (writeBytes append+sync, resolveGetsArgs separator/limit/chomp forms, the seek
// whence forms) through Ruby, asserting each result against MRI 4.0.5.
func TestIOWave18RubyPaths(t *testing.T) {
	cases := []struct{ src, want string }{
		// gets separator / limit / chomp / nil-separator (slurp) forms → resolveGetsArgs
		{`require "stringio"; io = StringIO.new("ab\ncd\nef"); p [io.gets, io.gets("c"), io.gets(2), io.gets(chomp: true)]`,
			"[\"ab\\n\", \"c\", \"d\\n\", \"ef\"]\n"},
		{`require "stringio"; p StringIO.new("a\nb\nc").gets(nil)`, "\"a\\nb\\nc\"\n"},
		// seek: default whence and an integer whence (StringIO rejects a symbolic
		// whence with TypeError — that path is covered directly in
		// TestSeekWhenceMapping via a real IO's whence table).
		{`require "stringio"; io = StringIO.new("0123456789"); io.seek(3); a = io.pos; io.seek(2, IO::SEEK_CUR); p [a, io.pos]`,
			"[3, 5]\n"},
		// StringIO append write then read back → writeBytes isStr/append branch
		{`require "stringio"; io = StringIO.new("", "a"); io.write("xy"); io.write("z"); p io.string`, "\"xyz\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestIOWave18ClosedAndSeekPaths covers the real-File edge branches the helpers
// added: sync=true flush on write, IOError on a closed File's pos/pos=/autoclose?/
// autoclose=, a symbolic seek whence on a real File, gets at EOF, an over-arity
// gets, and IO.binread's negative-offset Errno::EINVAL. Each result matches MRI 4.0.5.
func TestIOWave18ClosedAndSeekPaths(t *testing.T) {
	p := slash(t.TempDir()) + "/f.txt"
	q := func(s string) string { return "%q(" + s + ")" }
	seed := "File.write(" + q(p) + ", \"hello world\"); "
	cases := []struct{ src, want string }{
		// writeBytes sync=true branch: a File with #sync = true flushes each write.
		{seed + "f = File.open(" + q(p) + ", \"w\"); f.sync = true; f.write(\"xy\"); f.close; p File.read(" + q(p) + ")", "\"xy\"\n"},
		// closed real File raises IOError on pos / pos= / autoclose? / autoclose=
		// (the setter forms need begin/rescue: a `rescue` modifier on an assignment
		// guards the right-hand side, not the assignment).
		{seed + "f = File.open(" + q(p) + "); f.close; " +
			"a = (f.pos rescue $!.class); " +
			"b = (begin; f.pos = 0; rescue => e; e.class; end); " +
			"c = (f.autoclose? rescue $!.class); " +
			"d = (begin; f.autoclose = true; rescue => e; e.class; end); p [a, b, c, d]",
			"[IOError, IOError, IOError, IOError]\n"},
		// symbolic seek whence on a real File goes through seekWhence.
		{seed + "f = File.open(" + q(p) + "); f.seek(2, :SET); r = f.pos; f.close; p r", "2\n"},
		// gets at EOF returns nil (setLineGlobals' nil branch).
		{`require "stringio"; p StringIO.new("").gets`, "nil\n"},
		// gets with more than a separator and a limit raises ArgumentError.
		{`require "stringio"; p(StringIO.new("a").gets("x", 5, 9) rescue $!.class)`, "ArgumentError\n"},
		// IO.binread with a negative offset raises Errno::EINVAL (not ArgumentError).
		{seed + "p(IO.binread(" + q(p) + ", 1, -1) rescue $!.class)", "Errno::EINVAL\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
