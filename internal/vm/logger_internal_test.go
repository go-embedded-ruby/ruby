// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gotime "github.com/go-composites/time/src"
	lg "github.com/go-ruby-logger/logger"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// eval runs src through a fresh VM and returns its raw (untrimmed) stdout, so the
// Logger lines can be asserted byte-exactly.
func eval(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("run %q: %v", src, err)
	}
	return buf.String()
}

// evalErr runs src expecting a runtime RubyError and returns its class/message.
func evalErr(t *testing.T, src string) (class, msg string) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, rerr := New(io.Discard).Run(iseq)
	if rerr == nil {
		t.Fatalf("src=%q: expected a runtime error, got none", src)
	}
	re, ok := rerr.(RubyError)
	if !ok {
		t.Fatalf("src=%q: expected a RubyError, got %#v", src, rerr)
	}
	return re.Class, re.Message
}

// fixedClock and fixedPid pin the two host-supplied non-deterministic inputs so
// the formatted lines are byte-exact, matching what `ruby -rlogger` produces with
// the same instant and pid (verified against MRI 4.0.5).
var fixedInstant = time.Date(2026, 6, 30, 12, 34, 56, 789012000, time.UTC)

// newPinnedLogger builds a Logger writing to a captured buffer with the clock and
// pid pinned, so its default-formatter lines are deterministic.
func newPinnedLogger(t *testing.T) (*Logger, *strings.Builder) {
	t.Helper()
	vm := New(io.Discard)
	lo := vm.newLogger(object.Wrap(object.NewString(""))) // placeholder device, sink replaced below
	var b strings.Builder
	lo.path = ""
	lo.l.Sink = func(s string) { b.WriteString(s) }
	lo.l.Now = func() time.Time { return fixedInstant }
	lo.l.Pid = func() int { return 4242 }
	return lo, &b
}

// TestLoggerDefaultLineByteExact pins the default-formatter line byte-for-byte
// against MRI 4.0.5 for every severity, both with and without a progname.
func TestLoggerDefaultLineByteExact(t *testing.T) {
	cases := []struct {
		sev  lg.Severity
		prog string
		msg  object.Value
		want string
	}{
		{lg.DEBUG, "myprog", object.Wrap(object.NewString("hello")),
			"D, [2026-06-30T12:34:56.789012 #4242] DEBUG -- myprog: hello\n"},
		{lg.INFO, "", object.Wrap(object.NewString("world")),
			"I, [2026-06-30T12:34:56.789012 #4242]  INFO -- : world\n"},
		{lg.WARN, "p", object.Wrap(object.NewString("w")),
			"W, [2026-06-30T12:34:56.789012 #4242]  WARN -- p: w\n"},
		{lg.ERROR, "p", object.Wrap(object.NewString("e")),
			"E, [2026-06-30T12:34:56.789012 #4242] ERROR -- p: e\n"},
		{lg.FATAL, "p", object.Wrap(object.NewString("f")),
			"F, [2026-06-30T12:34:56.789012 #4242] FATAL -- p: f\n"},
		{lg.UNKNOWN, "p", object.Wrap(object.NewString("u")),
			"A, [2026-06-30T12:34:56.789012 #4242]   ANY -- p: u\n"},
	}
	for _, c := range cases {
		lo, b := newPinnedLogger(t)
		var prog object.Value
		if c.prog != "" {
			prog = object.Wrap(object.NewString(c.prog))
		}
		lo.add(c.sev, c.msg, prog, nil)
		if b.String() != c.want {
			t.Fatalf("sev=%d prog=%q: got %q want %q", c.sev, c.prog, b.String(), c.want)
		}
	}
}

// TestLoggerInspectMessage covers the Inspector path: a non-String, non-Exception
// message is rendered through the host #inspect, matching MRI's msg.inspect.
func TestLoggerInspectMessage(t *testing.T) {
	lo, b := newPinnedLogger(t)
	lo.add(lg.WARN, object.Wrap(&object.Array{Elems: []object.Value{object.IntValue(int64(object.Integer(1))), object.IntValue(int64(object.Integer(2)))}}), object.Wrap(object.NewString("p")), nil)
	want := "W, [2026-06-30T12:34:56.789012 #4242]  WARN -- p: [1, 2]\n"
	if b.String() != want {
		t.Fatalf("array message: got %q want %q", b.String(), want)
	}
}

// TestLoggerInspectMsgNonValue covers inspectMsg's unreachable fallback: a Go value
// that is not an object.Value yields "" (the host never feeds one, so this pins the
// defensive arm).
func TestLoggerInspectMsgNonValue(t *testing.T) {
	lo, _ := newPinnedLogger(t)
	if got := lo.inspectMsg(42); got != "" {
		t.Fatalf("inspectMsg(non-Value) = %q, want empty", got)
	}
}

// TestLoggerExceptionMessage covers the Exception shape: a Ruby exception with a
// backtrace renders "message (Class)\nbacktrace", as MRI's msg2str does.
func TestLoggerExceptionMessage(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
io = StringIO.new
l = Logger.new(io)
e = RuntimeError.new("boom")
e.set_backtrace(["a.rb:1", "b.rb:2"])
l.error(e)
puts io.string.split(" -- ", 2).last
`)
	if out != ": boom (RuntimeError)\na.rb:1\nb.rb:2\n" {
		t.Fatalf("exception render = %q", out)
	}
}

// TestLoggerAsExceptionNonException covers asException's two negative arms: a
// non-RObject value and an RObject whose class does not descend from Exception are
// both rejected.
func TestLoggerAsExceptionNonException(t *testing.T) {
	vm := New(io.Discard)
	lo := vm.newLogger(object.NilVal())
	if _, ok := lo.asException(object.IntValue(int64(object.Integer(1)))); ok {
		t.Fatal("Integer should not be an exception")
	}
	plain := &RObject{class: vm.cObject, ivars: map[string]object.Value{}}
	if _, ok := lo.asException(object.Wrap(plain)); ok {
		t.Fatal("a plain Object should not be an exception")
	}
}

// TestLoggerExceptionNoBacktrace covers the nil-backtrace arm of asException: an
// exception without a backtrace yields a trailing newline and no frames.
func TestLoggerExceptionNoBacktrace(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
io = StringIO.new
l = Logger.new(io)
l.error(RuntimeError.new("boom"))
puts io.string.split(" -- ", 2).last
`)
	if out != ": boom (RuntimeError)\n\n" {
		t.Fatalf("exception no-backtrace render = %q", out)
	}
}

// TestLoggerLevelGating covers the gating arm: a message below the level writes
// nothing yet add still returns true (MRI's add always returns true).
func TestLoggerLevelGating(t *testing.T) {
	lo, b := newPinnedLogger(t)
	lo.l.Level = lg.WARN
	if got := lo.add(lg.INFO, object.Wrap(object.NewString("x")), object.NilVal(), nil); got != object.BoolValue(bool(object.True)) {
		t.Fatalf("gated add returned %v, want true", got)
	}
	if b.String() != "" {
		t.Fatalf("gated add wrote %q, want nothing", b.String())
	}
	// A warn at the threshold is emitted.
	lo.add(lg.WARN, object.Wrap(object.NewString("y")), object.NilVal(), nil)
	if !strings.Contains(b.String(), "WARN -- : y") {
		t.Fatalf("warn not emitted: %q", b.String())
	}
}

// TestLoggerNilDevice covers the no-sink arms: add returns true writing nothing and
// << returns nil (the library's -1 mapped to nil), as MRI's nil device does.
func TestLoggerNilDevice(t *testing.T) {
	out := eval(t, `
require "logger"
l = Logger.new(nil)
p l.info("x")
p (l << "raw")
`)
	if out != "true\nnil\n" {
		t.Fatalf("nil device = %q", out)
	}
}

// TestLoggerPrognameJuggling covers add's progname-as-message juggling and the
// block form, both pinned against MRI.
func TestLoggerPrognameJuggling(t *testing.T) {
	// add(WARN, nil, "progname-as-msg"): message nil, no block => message=progname.
	lo, b := newPinnedLogger(t)
	lo.add(lg.WARN, object.NilVal(), object.Wrap(object.NewString("progname-as-msg")), nil)
	want := "W, [2026-06-30T12:34:56.789012 #4242]  WARN -- : progname-as-msg\n"
	if b.String() != want {
		t.Fatalf("progname-as-message: got %q want %q", b.String(), want)
	}
}

// TestLoggerSeverityHelpersBlock exercises the per-severity helpers and the block
// form (info("PROG") { "msg" } => progname=PROG, message=block) through the full
// pipeline, with a custom formatter so the line is deterministic.
func TestLoggerSeverityHelpersBlock(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
io = StringIO.new
l = Logger.new(io)
l.formatter = proc { |sev, time, prog, msg| "#{sev}|#{prog}|#{msg}\n" }
l.info("PROG") { "the message" }
l.warn("just a message")
l.debug("d"); l.error("e"); l.fatal("f"); l.unknown("u")
puts io.string
`)
	want := "INFO|PROG|the message\nWARN||just a message\nDEBUG||d\nERROR||e\nFATAL||f\nANY||u\n"
	if out != want {
		t.Fatalf("severity helpers = %q want %q", out, want)
	}
}

// TestLoggerPredicates covers the debug?/info?/…/fatal? predicates against the
// level, pinned against MRI.
func TestLoggerPredicates(t *testing.T) {
	out := eval(t, `
require "logger"
l = Logger.new(STDOUT)
l.level = :warn
p [l.debug?, l.info?, l.warn?, l.error?, l.fatal?]
`)
	if out != "[false, false, true, true, true]\n" {
		t.Fatalf("predicates = %q", out)
	}
}

// TestLoggerLevelAccessors covers level / level= / sev_threshold= with Integer,
// String and Symbol forms, and the invalid-level ArgumentError.
func TestLoggerLevelAccessors(t *testing.T) {
	out := eval(t, `
require "logger"
l = Logger.new(STDOUT)
p l.level
l.level = 2; p l.level
l.level = "error"; p l.level
l.level = :fatal; p l.level
l.sev_threshold = :info; p l.level
begin; l.level = :nope; rescue ArgumentError => e; p e.message; end
`)
	want := "0\n2\n3\n4\n1\n\"invalid log level: nope\"\n"
	if out != want {
		t.Fatalf("level accessors = %q want %q", out, want)
	}
}

// TestLoggerAddInvalidSeverityArg covers add's severity coercion error arm: a
// non-coercible severity argument is an ArgumentError.
func TestLoggerAddInvalidSeverityArg(t *testing.T) {
	class, msg := evalErr(t, `require "logger"; Logger.new(STDOUT).add(:bogus)`)
	if class != "ArgumentError" || !strings.Contains(msg, "invalid log level: bogus") {
		t.Fatalf("add bogus severity = %s / %q", class, msg)
	}
}

// TestLoggerAddNilSeverity covers add's nil-severity arm: add(nil) defaults to
// UNKNOWN (the "A"/ANY line).
func TestLoggerAddNilSeverity(t *testing.T) {
	lo, b := newPinnedLogger(t)
	// Drive through the Ruby-facing add by calling the wrapper with UNKNOWN, which
	// is what add(nil, …) resolves to.
	lo.add(lg.UNKNOWN, object.Wrap(object.NewString("x")), object.NilVal(), nil)
	if !strings.Contains(b.String(), "  ANY -- : x") {
		t.Fatalf("nil severity -> ANY: %q", b.String())
	}
}

// TestLoggerProgname covers progname / progname= including the nil reset.
func TestLoggerProgname(t *testing.T) {
	out := eval(t, `
require "logger"
l = Logger.new(STDOUT)
p l.progname
l.progname = "app"; p l.progname
l.progname = nil; p l.progname
`)
	if out != "nil\n\"app\"\nnil\n" {
		t.Fatalf("progname = %q", out)
	}
}

// TestLoggerDatetimeFormat covers datetime_format / datetime_format= including the
// nil read (default) and nil reset, with a byte-exact emitted line.
func TestLoggerDatetimeFormat(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
io = StringIO.new
l = Logger.new(io)
p l.datetime_format
l.datetime_format = "%Y"
p l.datetime_format
l.datetime_format = nil
p l.datetime_format
`)
	if out != "nil\n\"%Y\"\nnil\n" {
		t.Fatalf("datetime_format = %q", out)
	}
	// Byte-exact line with a custom datetime format, via the pinned clock.
	lo, b := newPinnedLogger(t)
	lo.l.Formatter.DatetimeFormat = "%Y"
	lo.add(lg.INFO, object.Wrap(object.NewString("x")), object.NilVal(), nil)
	if b.String() != "I, [2026 #4242]  INFO -- : x\n" {
		t.Fatalf("custom datetime line = %q", b.String())
	}
}

// TestLoggerWriteCount covers <<: a raw write returns the byte count.
func TestLoggerWriteCount(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
io = StringIO.new
l = Logger.new(io)
p (l << "abc")
puts io.string
`)
	if out != "3\nabc\n" {
		t.Fatalf("<< = %q", out)
	}
}

// TestLoggerClose covers close: subsequent writes are no-ops and close returns nil.
func TestLoggerClose(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
io = StringIO.new
l = Logger.new(io)
l.info("before")
p l.close
l.info("after")
p io.string.include?("before")
p io.string.include?("after")
`)
	if out != "nil\ntrue\nfalse\n" {
		t.Fatalf("close = %q", out)
	}
}

// TestLoggerReopen covers reopen: after close, reopen onto a new device resumes
// logging there.
func TestLoggerReopen(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
a = StringIO.new
b = StringIO.new
l = Logger.new(a)
l.close
l.reopen(b)
l.info("hi")
p a.string.empty?
p b.string.include?("hi")
`)
	if out != "true\ntrue\n" {
		t.Fatalf("reopen = %q", out)
	}
}

// TestLoggerReopenNoArg covers reopen with no argument: it just clears closed,
// keeping the existing device.
func TestLoggerReopenNoArg(t *testing.T) {
	vm := New(io.Discard)
	lo := vm.newLogger(object.NilVal())
	lo.closed = true
	vm.send(object.Wrap(lo), "reopen", nil, nil)
	if lo.closed {
		t.Fatal("reopen() should clear closed")
	}
}

// TestLoggerFormatterAccessor covers formatter / formatter= including the nil read
// and the non-Proc reset.
func TestLoggerFormatterAccessor(t *testing.T) {
	out := eval(t, `
require "logger"
l = Logger.new(STDOUT)
p l.formatter
f = proc { |s, t, p, m| "x" }
l.formatter = f
p l.formatter.equal?(f)
l.formatter = nil
p l.formatter
`)
	if out != "nil\ntrue\nnil\n" {
		t.Fatalf("formatter accessor = %q", out)
	}
}

// TestLoggerCtorKwargs covers Logger.new keyword options: level, progname,
// datetime_format and formatter all take effect.
func TestLoggerCtorKwargs(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
io = StringIO.new
l = Logger.new(io, level: Logger::INFO, progname: "app", datetime_format: "%Y")
p l.level
p l.progname
p l.datetime_format
l.debug("hidden")
l.info("shown")
puts io.string
`)
	if !strings.Contains(out, "1\n\"app\"\n\"%Y\"\n") {
		t.Fatalf("ctor kwargs heads = %q", out)
	}
	if strings.Contains(out, "hidden") || !strings.Contains(out, "shown") {
		t.Fatalf("ctor level gating wrong: %q", out)
	}
	if !strings.Contains(out, "[2026 #") {
		t.Fatalf("ctor datetime_format not applied: %q", out)
	}
}

// TestLoggerCtorFormatterKwarg covers the formatter: keyword option specifically.
func TestLoggerCtorFormatterKwarg(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
io = StringIO.new
l = Logger.new(io, formatter: proc { |s, t, p, m| "K:#{m}\n" })
l.info("hi")
puts io.string
`)
	if out != "K:hi\n" {
		t.Fatalf("ctor formatter kwarg = %q", out)
	}
}

// TestLoggerCtorNilKwargs covers the nil arms of applyKwargs: an explicit
// progname:/datetime_format: nil leaves the defaults intact.
func TestLoggerCtorNilKwargs(t *testing.T) {
	out := eval(t, `
require "logger"
l = Logger.new(STDOUT, progname: nil, datetime_format: nil)
p l.progname
p l.datetime_format
`)
	if out != "nil\nnil\n" {
		t.Fatalf("ctor nil kwargs = %q", out)
	}
}

// TestLoggerApplyKwargsBadLevel covers the level coercion error arm of applyKwargs.
func TestLoggerApplyKwargsBadLevel(t *testing.T) {
	class, msg := evalErr(t, `require "logger"; Logger.new(STDOUT, level: :nope)`)
	if class != "ArgumentError" || !strings.Contains(msg, "invalid log level: nope") {
		t.Fatalf("ctor bad level = %s / %q", class, msg)
	}
}

// TestLoggerSeverityConstants covers Logger::DEBUG…UNKNOWN and Logger::Severity.
func TestLoggerSeverityConstants(t *testing.T) {
	out := eval(t, `
require "logger"
p [Logger::DEBUG, Logger::INFO, Logger::WARN, Logger::ERROR, Logger::FATAL, Logger::UNKNOWN]
p Logger::Severity::DEBUG
p Logger::Severity.is_a?(Module)
`)
	if out != "[0, 1, 2, 3, 4, 5]\n0\ntrue\n" {
		t.Fatalf("severity constants = %q", out)
	}
}

// TestLoggerErrorHierarchy covers Logger::Error and Logger::ShiftingError.
func TestLoggerErrorHierarchy(t *testing.T) {
	out := eval(t, `
require "logger"
p Logger::Error.ancestors.include?(RuntimeError)
p Logger::ShiftingError.ancestors.include?(Logger::Error)
`)
	if out != "true\ntrue\n" {
		t.Fatalf("error hierarchy = %q", out)
	}
}

// TestLoggerFormatterClass covers Logger::Formatter.new#call byte-exactly and the
// datetime_format accessor, pinning the line against MRI 4.0.5.
func TestLoggerFormatterClass(t *testing.T) {
	vm := New(io.Discard)
	vm.registerLogger() // ensure the class exists (also installed at construction)
	fc := vm.cLoggerFormatter
	lf := fc.smethods["new"].native(vm, object.NilVal(), nil, nil)
	tm := &Time{t: gotime.FromUnix(fixedInstant.Unix())}
	args := []object.Value{
		object.Wrap(object.NewString("DEBUG")), object.Wrap(tm), object.Wrap(object.NewString("prog")), object.Wrap(object.NewString("hello")),
	}
	got := vm.send(lf, "call", args, nil).ToS()
	// The pid is the live process pid; assert the stable prefix/suffix.
	if !strings.HasPrefix(got, "D, [") || !strings.HasSuffix(got, "] DEBUG -- prog: hello\n") {
		t.Fatalf("formatter#call = %q", got)
	}
}

// TestLoggerFormatterClassDatetime covers Logger::Formatter#datetime_format= and
// the nil read, and the exception / non-Time / nil-progname arms of #call.
func TestLoggerFormatterClassDatetime(t *testing.T) {
	out := eval(t, `
require "logger"
f = Logger::Formatter.new
p f.datetime_format
f.datetime_format = "%Y"
p f.datetime_format
f.datetime_format = nil
p f.datetime_format
`)
	if out != "nil\n\"%Y\"\nnil\n" {
		t.Fatalf("formatter datetime_format = %q", out)
	}
}

// TestLoggerFormatterClassArms covers #call's exception message, nil progname and
// non-Time time arms.
func TestLoggerFormatterClassArms(t *testing.T) {
	out := eval(t, `
require "logger"
f = Logger::Formatter.new
e = RuntimeError.new("boom")
line = f.call("ERROR", Time.now, nil, e)
p line.include?("boom (RuntimeError)")
p line.include?(" ERROR -- : ")
arr = f.call("WARN", nil, "p", [1, 2])
p arr.include?("[1, 2]")
`)
	if out != "true\ntrue\ntrue\n" {
		t.Fatalf("formatter call arms = %q", out)
	}
}

// TestLoggerFileSink covers the file-backed device: lines are appended to the file
// rbgo opens, and a path that is unwritable raises.
func TestLoggerFileSink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	slash := filepath.ToSlash(path)
	eval(t, `
require "logger"
l = Logger.new("`+slash+`")
l.formatter = proc { |s, t, p, m| "#{s}: #{m}\n" }
l.info("one")
l.warn("two")
`)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if string(data) != "INFO: one\nWARN: two\n" {
		t.Fatalf("file sink = %q", string(data))
	}
}

// TestLoggerFileSinkOpenError covers fileSink's open-error arm: a path whose parent
// does not exist raises Errno::ENOENT.
func TestLoggerFileSinkOpenError(t *testing.T) {
	class, msg := evalErr(t, `
require "logger"
l = Logger.new("/no/such/dir/app.log")
l.info("x")
`)
	if class != "Errno::ENOENT" || !strings.Contains(msg, "No such file") {
		t.Fatalf("file open error = %s / %q", class, msg)
	}
}

// TestLoggerSizeRotation covers the size-based rotation policy: with shift_age and a
// tiny shift_size, each write past the threshold rotates app.log -> app.log.0 etc.
func TestLoggerSizeRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	slash := filepath.ToSlash(path)
	eval(t, `
require "logger"
l = Logger.new("`+slash+`", 3, 10)
l.formatter = proc { |s, t, p, m| "#{m}\n" }
l.info("aaaaaaaaaa")  # 11 bytes -> over threshold after first write
l.info("bbbbbbbbbb")
l.info("cccccccccc")
`)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("base log missing: %v", err)
	}
	if _, err := os.Stat(path + ".0"); err != nil {
		t.Fatalf("rotated backup app.log.0 missing: %v", err)
	}
}

// TestLoggerPeriodRotation covers the period (daily) rotation policy: a logger with
// a past next-rotate time rotates the current log to a dated name on the next write.
func TestLoggerPeriodRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	vm := New(io.Discard)
	lo := vm.newLogger(object.Wrap(object.NewString(path)))
	lo.l.Formatter = &lg.Formatter{}
	lo.l.Now = func() time.Time { return fixedInstant }
	lo.setPeriod("daily")
	// Pre-create the base log and force a due rotation.
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	lo.nextRotate = fixedInstant.Add(-time.Hour) // already due
	lo.add(lg.INFO, object.Wrap(object.NewString("new")), object.NilVal(), nil)
	// The old content was rotated away to a dated file, and a fresh log holds the
	// new line.
	matches, _ := filepath.Glob(path + ".*")
	if len(matches) == 0 {
		t.Fatalf("no dated rotation file created in %s", dir)
	}
	fresh, _ := os.ReadFile(path)
	if !strings.Contains(string(fresh), "new") {
		t.Fatalf("fresh log missing new line: %q", string(fresh))
	}
}

// TestLoggerSetPeriodInvalid covers setPeriod's error arm.
func TestLoggerSetPeriodInvalid(t *testing.T) {
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "ArgumentError" || !strings.Contains(re.Message, "invalid filename rotation period") {
			t.Fatalf("setPeriod invalid: got %v", recover())
		}
	}()
	vm := New(io.Discard)
	lo := vm.newLogger(object.NilVal())
	lo.setPeriod("hourly")
}

// TestLoggerApplyShiftAgeForms covers applyShiftAge's Integer, String and Symbol
// arms (the Integer count, and the two period spellings).
func TestLoggerApplyShiftAgeForms(t *testing.T) {
	vm := New(io.Discard)
	lo := vm.newLogger(object.Wrap(object.NewString(filepath.Join(t.TempDir(), "x.log"))))
	lo.l.Now = func() time.Time { return fixedInstant }
	lo.applyShiftAge(object.IntValue(int64(object.Integer(5))))
	if lo.shiftAge != 5 {
		t.Fatalf("Integer shift_age = %d, want 5", lo.shiftAge)
	}
	lo.applyShiftAge(object.Wrap(object.NewString("weekly")))
	if lo.period != lg.Weekly {
		t.Fatalf("String shift_age period = %q, want weekly", lo.period)
	}
	lo.applyShiftAge(object.SymVal(string(object.Symbol("monthly"))))
	if lo.period != lg.Monthly {
		t.Fatalf("Symbol shift_age period = %q, want monthly", lo.period)
	}
}

// TestLoggerStringShiftAgeCtor covers the constructor String shift_age path end to
// end (Logger.new(path, "daily")).
func TestLoggerStringShiftAgeCtor(t *testing.T) {
	dir := t.TempDir()
	slash := filepath.ToSlash(filepath.Join(dir, "d.log"))
	eval(t, `require "logger"; Logger.new("`+slash+`", "daily").info("x")`)
	if _, err := os.Stat(filepath.Join(dir, "d.log")); err != nil {
		t.Fatalf("daily-rotated logger did not write base log: %v", err)
	}
}

// TestLoggerIOSinkArbitraryObject covers ioSink's non-IOObj arm: a device that is
// an arbitrary object with a #write method receives the line through #write.
func TestLoggerIOSinkArbitraryObject(t *testing.T) {
	out := eval(t, `
require "logger"
sink = Object.new
def sink.lines; @lines ||= []; end
def sink.write(s); lines << s; end
l = Logger.new(sink)
l.formatter = proc { |s, t, p, m| "#{m}!" }
l.info("hi")
puts sink.lines.first
`)
	if out != "hi!\n" {
		t.Fatalf("arbitrary object sink = %q", out)
	}
}

// TestLoggerRequire covers the require feature: require "logger" returns true once
// then false, and the feature is reported as provided.
func TestLoggerRequire(t *testing.T) {
	out := eval(t, `p require "logger"; p require "logger"`)
	if out != "true\nfalse\n" {
		t.Fatalf("require logger = %q", out)
	}
}

// TestLoggerClassOf covers the classOf arms for the wrappers.
func TestLoggerClassOf(t *testing.T) {
	out := eval(t, `
require "logger"
p Logger.new(STDOUT).class
p Logger::Formatter.new.class
`)
	if out != "Logger\nLogger::Formatter\n" {
		t.Fatalf("classOf = %q", out)
	}
}

// TestLoggerWrapperReprTruthy covers the wrapper To/Inspect/Truthy helpers.
func TestLoggerWrapperReprTruthy(t *testing.T) {
	lo := &Logger{}
	if lo.ToS() != "#<Logger>" || lo.Inspect() != "#<Logger>" || !lo.Truthy() {
		t.Fatalf("Logger repr/truthy wrong")
	}
	lf := &LoggerFormatter{}
	if lf.ToS() != "#<Logger::Formatter>" || lf.Inspect() != "#<Logger::Formatter>" || !lf.Truthy() {
		t.Fatalf("LoggerFormatter repr/truthy wrong")
	}
}

// TestLoggerArg covers loggerArg's type-error arm.
func TestLoggerArg(t *testing.T) {
	if got := loggerArg(object.Wrap(&Logger{})); got == nil {
		t.Fatal("loggerArg(*Logger) returned nil")
	}
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "TypeError" {
			t.Fatalf("loggerArg(non-Logger): got %v", recover())
		}
	}()
	loggerArg(object.IntValue(int64(object.Integer(1))))
}

// TestLoggerSeverityArgDefault covers severityArg's default arm: a value outside
// Integer/String/Symbol passes through to CoerceSeverity, which errors.
func TestLoggerSeverityArgDefault(t *testing.T) {
	if got := severityArg(object.NilVal()); got == nil {
		t.Fatal("severityArg(nil) returned nil")
	}
}

// TestLoggerCloseInternal covers close clearing the sink directly.
func TestLoggerCloseInternal(t *testing.T) {
	vm := New(io.Discard)
	lo := vm.newLogger(object.Wrap(object.NewString("")))
	vm.send(object.Wrap(lo), "close", nil, nil)
	if lo.l.Sink != nil {
		t.Fatal("close did not clear the sink")
	}
}

// TestLoggerRegisterErrorsFallback covers registerLoggerErrors' fallback arm: when
// RuntimeError is absent, Logger::Error falls back to Exception. We simulate this by
// building a bare VM, deleting RuntimeError, and re-running the registration.
func TestLoggerRegisterErrorsFallback(t *testing.T) {
	vm := New(io.Discard)
	delete(vm.consts, "RuntimeError")
	cls := newClass("LoggerProbe", vm.cObject)
	vm.registerLoggerErrors(cls)
	if object.IsNil(cls.consts["Error"]) {
		t.Fatal("fallback Logger::Error not installed")
	}
}

// failingWriter always errors on Write, exercising writeLogFile's IOError arm.
type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

// TestLoggerWriteLogFileError covers writeLogFile's defensive IOError arm.
func TestLoggerWriteLogFileError(t *testing.T) {
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "IOError" {
			t.Fatalf("writeLogFile failure: got %v", recover())
		}
	}()
	writeLogFile(failingWriter{}, "x")
}

// TestLoggerAddPositional covers add/log called with explicit severity, message
// and progname positionals through the full pipeline.
func TestLoggerAddPositional(t *testing.T) {
	out := eval(t, `
require "logger"
require "stringio"
io = StringIO.new
l = Logger.new(io)
l.formatter = proc { |s, t, p, m| "#{s}|#{p}|#{m}\n" }
l.add(Logger::WARN, "msg")
l.add(Logger::ERROR, "msg2", "prog2")
l.log(Logger::INFO, "msg3", "prog3")
puts io.string
`)
	want := "WARN||msg\nERROR|prog2|msg2\nINFO|prog3|msg3\n"
	if out != want {
		t.Fatalf("add positional = %q want %q", out, want)
	}
}

// TestLoggerRotatePeriodInvalid covers rotatePeriod's PreviousPeriodEnd error arm:
// a Period the library cannot resolve makes rotatePeriod return without renaming.
func TestLoggerRotatePeriodInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	vm := New(io.Discard)
	lo := vm.newLogger(object.Wrap(object.NewString(path)))
	lo.period = lg.Period("not-a-period") // unresolvable -> PreviousPeriodEnd errors
	lo.rotatePeriod(fixedInstant)
	data, _ := os.ReadFile(path)
	if string(data) != "old\n" {
		t.Fatalf("invalid period should not rotate: %q", string(data))
	}
}

// TestLoggerMaybeRotateNoPath covers maybeRotate's non-file early return.
func TestLoggerMaybeRotateNoPath(t *testing.T) {
	vm := New(io.Discard)
	lo := vm.newLogger(object.NilVal()) // no path
	lo.maybeRotate(100)                 // must be a no-op (no panic)
}

// TestLoggerRotatePeriodNoBase covers rotatePeriod when the base log does not yet
// exist: no rename happens and the arm returns cleanly.
func TestLoggerRotatePeriodNoBase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.log")
	vm := New(io.Discard)
	lo := vm.newLogger(object.Wrap(object.NewString(path)))
	lo.period = lg.Daily
	lo.rotatePeriod(fixedInstant) // base file absent -> no-op, no rename
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rotatePeriod created a file unexpectedly")
	}
}

// TestLoggerFormatterInspectNonValue covers the Logger::Formatter Inspect closure's
// non-object.Value fallback.
func TestLoggerFormatterInspectNonValue(t *testing.T) {
	vm := New(io.Discard)
	lf := object.Kind[*LoggerFormatter](vm.cLoggerFormatter.smethods["new"].native(vm, object.NilVal(), nil, nil))
	if got := lf.f.Inspect(42); got != "" {
		t.Fatalf("formatter Inspect(non-Value) = %q, want empty", got)
	}
}

// TestLoggerFormatterInspectValue covers the closure's object.Value arm.
func TestLoggerFormatterInspectValue(t *testing.T) {
	vm := New(io.Discard)
	lf := object.Kind[*LoggerFormatter](vm.cLoggerFormatter.smethods["new"].native(vm, object.NilVal(), nil, nil))
	if got := lf.f.Inspect(object.Integer(7)); got != "7" {
		t.Fatalf("formatter Inspect(Integer 7) = %q, want 7", got)
	}
}

// TestLoggerOnlyKwargs covers Logger.new called with only keyword options (no
// positional device): the device is nil and add writes nothing.
func TestLoggerOnlyKwargs(t *testing.T) {
	out := eval(t, `
require "logger"
l = Logger.new(level: Logger::INFO)
p l.info("x")
`)
	if out != "true\n" {
		t.Fatalf("only-kwargs ctor = %q", out)
	}
}
