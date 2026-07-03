// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"os"
	"time"

	gotime "github.com/go-composites/time/src"
	lg "github.com/go-ruby-logger/logger"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Logger binds github.com/go-ruby-logger/logger — the pure-Go, MRI-4.0.5-faithful
// port of the deterministic core of Ruby's stdlib Logger — into rbgo. The library
// owns every pure decision: the severity model, the default Formatter (byte-exact
// line format), level gating, and the rotation *policy* (which renames a size- or
// period-based rotation performs, and when). This file is the host glue MRI splits
// out into the LogDevice: it wires the IO sink (a $stdout / $stderr / IO object or
// an opened file), the wall clock (Logger.Now), the pid (Logger.Pid), the
// #inspect of a non-String message (the library's Inspector), the Exception shape,
// and the actual file open / write / rename-on-rotation.
//
// A custom Ruby formatter (Logger#formatter=) is a Proc whose #call(severity_label,
// time, progname, raw_msg) the host must run — so when one is set the library's
// Format is bypassed and the Proc drives the bytes, exactly as MRI does.

// Logger is the Ruby wrapper around a go-ruby-logger Logger plus the host-side
// rotation state and IO device the library deliberately omits.
type Logger struct {
	l   *lg.Logger   // the pure compute core (severity/format/gating)
	vm  *VM          // for #inspect of arbitrary messages and Proc formatters
	dev object.Value // the IO device passed to Logger.new ($stdout/$stderr/IO) or nil
	// File-backed device state. When path != "" the device is a file rbgo opens,
	// writes and rotates; otherwise writes go through dev (an IOObj).
	path        string
	shiftAge    int       // integer shift_age: size-based backups kept (0 = none)
	shiftSize   int64     // size threshold for a size-based rotation
	period      lg.Period // calendar cadence ("" = size-based)
	nextRotate  time.Time // next instant a period rotation is due
	progDefault string    // the @progname default (mirrored from l.Progname)
	closed      bool
	// formatter is a custom Ruby Proc formatter (Logger#formatter=). When set it
	// drives the bytes; the library Formatter is used only for its datetime/inspect
	// fallbacks via the default path.
	formatter *Proc
}

func (lo *Logger) ToS() string     { return "#<Logger>" }
func (lo *Logger) Inspect() string { return "#<Logger>" }
func (lo *Logger) Truthy() bool    { return true }

// LoggerFormatter is the Ruby wrapper around a standalone Logger::Formatter
// (Logger::Formatter.new). Its #call renders one line through the library exactly
// as the default formatter does.
type LoggerFormatter struct {
	f  *lg.Formatter
	vm *VM
}

func (lf *LoggerFormatter) ToS() string     { return "#<Logger::Formatter>" }
func (lf *LoggerFormatter) Inspect() string { return "#<Logger::Formatter>" }
func (lf *LoggerFormatter) Truthy() bool    { return true }

// loggerArg asserts an argument is a Logger, raising TypeError otherwise.
func loggerArg(v object.Value) *Logger {
	lo, ok := object.KindOK[*Logger](v)
	if !ok {
		raise("TypeError", "value must be a Logger")
	}
	return lo
}

// inspectMsg renders a Ruby value as msg.inspect would — the library's Inspector,
// wired from rbgo's object model so an Array/Hash/Integer message inspects exactly
// as MRI's Logger::Formatter#msg2str does.
func (lo *Logger) inspectMsg(msg any) string {
	if v, ok := msg.(object.Value); ok {
		return lo.vm.send(v, "inspect", nil, nil).ToS()
	}
	return "" // unreachable: the host only ever feeds object.Value messages
}

// newLogger builds the wrapper for Logger.new(logdev, …). It opens the device
// (or wires an IO sink), pins the clock and pid through the host, and seeds the
// rotation policy. logdev may be nil (no device), $stdout/$stderr/an IO object,
// or a path String.
func (vm *VM) newLogger(logdev object.Value) *Logger {
	lo := &Logger{
		l:         lg.New(nil),
		vm:        vm,
		shiftAge:  0,
		shiftSize: lg.DefaultShiftSize,
	}
	lo.l.Now = func() time.Time { return time.Now() }
	lo.l.Pid = func() int { return os.Getpid() }
	lo.l.Formatter = &lg.Formatter{Inspect: lo.inspectMsg}
	lo.bindDevice(logdev)
	return lo
}

// bindDevice wires logdev as the Logger's IO sink. nil / Ruby nil leaves the sink
// nil (MRI's @logdev.nil?). A String is a file path rbgo opens for append; any
// other value (an IOObj such as $stdout/$stderr) is written through directly.
func (lo *Logger) bindDevice(logdev object.Value) {
	if object.IsNil(logdev) {
		lo.l.Sink = nil
		return
	}
	{
		__sw90 := logdev
		switch {
		case object.IsKind[*object.String](__sw90):
			d := object.Kind[*object.String](__sw90)
			_ = d
			lo.path = d.Str()
			lo.l.Sink = lo.fileSink
		default:
			d := __sw90
			_ = d
			lo.dev = logdev
			lo.l.Sink = lo.ioSink
		}
	}
}

// ioSink writes a formatted line to the bound IO object ($stdout/$stderr/an IO),
// after applying any due size/period rotation (a no-op for a non-file device).
func (lo *Logger) ioSink(s string) {
	if io, ok := object.KindOK[*IOObj](lo.dev); ok {
		io.writeStr(s)
		return
	}
	// A host that rebound the device to an arbitrary object: route through #write.
	lo.vm.send(lo.dev, "write", []object.Value{object.Wrap(object.NewString(s))}, nil)
}

// fileSink applies any due rotation, then appends s to the log file. The library
// decided what bytes s is and (via maybeRotate) whether a rotation is due; rbgo
// performs the open/write/rename.
func (lo *Logger) fileSink(s string) {
	lo.maybeRotate(int64(len(s)))
	f, err := os.OpenFile(lo.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", lo.path)
	}
	defer f.Close()
	writeLogFile(f, s)
}

// writeLogFile appends s to the open log file, raising IOError on a write failure
// (a defensive arm — a normal append never fails). Extracted so the failure path
// is testable with a failing writer.
func writeLogFile(w io.Writer, s string) {
	if _, err := w.Write([]byte(s)); err != nil {
		raise("IOError", "%s", err.Error())
	}
}

// maybeRotate consults the library's rotation policy for the file device and, when
// a rotation is due, performs the renames the library prescribes, then re-arms the
// period clock. addLen is the size the next write will add (MRI checks before
// writing). It is a no-op for a non-file device.
func (lo *Logger) maybeRotate(addLen int64) {
	if lo.path == "" {
		return
	}
	now := lo.l.Now()
	if lo.period != "" {
		if lg.ShouldRotateByPeriod(now, lo.nextRotate) {
			lo.rotatePeriod(now)
			lo.armPeriod(now)
		}
		return
	}
	var size int64
	if fi, err := os.Stat(lo.path); err == nil {
		size = fi.Size()
	}
	if lg.ShouldRotateBySize(size+addLen, lo.shiftSize, lo.shiftAge) {
		lo.rotateSize()
	}
}

// rotateSize applies the size-based rename sequence the library prescribes,
// skipping renames whose source does not exist (as MRI's FileTest.exist? guard).
func (lo *Logger) rotateSize() {
	for _, m := range lg.ShiftAgeSequence(lo.path, lo.shiftAge) {
		if _, err := os.Stat(m.From); err == nil {
			os.Rename(m.From, m.To)
		}
	}
}

// rotatePeriod renames the current log to the library-chosen period-suffixed name
// (the host supplies FileTest.exist? so the library can disambiguate collisions),
// leaving a fresh log to be created on the next write.
func (lo *Logger) rotatePeriod(now time.Time) {
	prevEnd, err := lg.PreviousPeriodEnd(now, lo.period)
	if err != nil {
		return
	}
	exists := func(name string) bool { _, e := os.Stat(name); return e == nil }
	age := lg.PeriodAgeFile(lo.path, prevEnd, lg.DefaultPeriodSuffix, exists)
	if _, e := os.Stat(lo.path); e == nil {
		os.Rename(lo.path, age)
	}
}

// armPeriod re-computes the next instant a period rotation becomes due.
func (lo *Logger) armPeriod(now time.Time) {
	if nr, err := lg.NextRotateTime(now, lo.period); err == nil {
		lo.nextRotate = nr
	}
}

// applyShiftAge interprets the shift_age constructor argument: an Integer is a
// size-based backup count; a String/Symbol ("daily"/"weekly"/"monthly"/…) selects
// a calendar period and arms its clock.
func (lo *Logger) applyShiftAge(v object.Value) {
	{
		__sw91 := v
		switch {
		case object.IsInt(__sw91):
			x := object.AsInteger(__sw91)
			_ = x
			lo.shiftAge = int(x)
		case object.IsKind[*object.String](__sw91):
			x := object.Kind[*object.String](__sw91)
			_ = x
			lo.setPeriod(x.Str())
		case object.IsKind[object.Symbol](__sw91):
			x := object.Kind[object.Symbol](__sw91)
			_ = x
			lo.setPeriod(string(x))
		}
	}
}

// setPeriod parses a period spelling and arms the rotation clock, raising
// ArgumentError for an unrecognised cadence (mirroring MRI's Period coercion).
func (lo *Logger) setPeriod(s string) {
	p, err := lg.ParsePeriod(s)
	if err != nil {
		raise("ArgumentError", "invalid filename rotation period: %s", s)
	}
	lo.period = p
	lo.armPeriod(lo.l.Now())
}

// add is the MRI Logger#add core, performing the message/progname juggling the
// library leaves to the host: severity defaults to UNKNOWN; a gated-out or
// device-less call returns true writing nothing; an absent message is taken from
// the block (if any) else from the progname (with progname then falling back to
// the default), exactly as MRI's add does.
func (lo *Logger) add(sev lg.Severity, message object.Value, progname object.Value, blk *Proc) object.Value {
	if lo.l.Sink == nil || sev < lo.l.Level {
		return object.BoolValue(bool(object.True))
	}
	// progname ||= @progname (resolved first, as MRI does).
	prog := lo.progDefault
	if !isNilV(progname) {
		prog = progname.ToS()
	}
	// Resolve the raw message exactly as MRI's add does: an explicit non-nil
	// message wins; otherwise the block (if any) supplies it; otherwise the
	// progname becomes the message and progname falls back to the default.
	raw := message
	if isNilV(raw) {
		if blk != nil {
			raw = lo.vm.callBlock(blk, nil)
		} else {
			raw = object.Wrap(object.NewString(prog))
			prog = lo.progDefault
		}
	}
	if lo.formatter != nil {
		lo.emitCustom(sev, prog, raw)
		return object.BoolValue(bool(object.True))
	}
	lo.l.Add(sev, lo.coerce(raw), prog)
	return object.BoolValue(bool(object.True))
}

// emitCustom drives a custom Ruby Proc formatter: it is called with the severity
// label, a Time, the progname and the RAW (un-stringified) message value, and its
// String result is written to the sink, mirroring MRI handing the raw message to a
// user formatter.
func (lo *Logger) emitCustom(sev lg.Severity, prog string, raw object.Value) {
	now := lo.l.Now()
	t := &Time{t: gotime.FromUnix(now.Unix())}
	args := []object.Value{
		object.Wrap(object.NewString(lg.SeverityLabel(sev))),
		object.Wrap(t),
		object.Wrap(object.NewString(prog)),
		raw,
	}
	out := lo.vm.callBlock(lo.formatter, args)
	lo.l.Sink(out.ToS())
}

// coerce turns a Ruby message value into the shape the library's Format expects: a
// String passes its bytes through, an exception becomes a logger.Exception (so the
// library renders "message (Class)\nbacktrace"), and anything else stays an
// object.Value the Inspector renders via #inspect.
func (lo *Logger) coerce(v object.Value) any {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	if exc, ok := lo.asException(v); ok {
		return exc
	}
	return v
}

// asException recognises a Ruby exception object and projects it onto the library's
// Exception shape (message / class name / backtrace), so msg2str renders it exactly
// as MRI does.
func (lo *Logger) asException(v object.Value) (lg.Exception, bool) {
	if _, ok := object.KindOK[*RObject](v); !ok {
		return lg.Exception{}, false
	}
	cls := lo.vm.classOf(v)
	if cls == nil || lo.vm.cException == nil || !lo.vm.classLE(cls, lo.vm.cException) {
		return lg.Exception{}, false
	}
	exc := lg.Exception{
		Message: lo.vm.send(v, "message", nil, nil).ToS(),
		Class:   lo.vm.send(v, "class", nil, nil).ToS(),
	}
	if bt := lo.vm.send(v, "backtrace", nil, nil); !object.IsNil(bt) {
		if arr, ok := object.KindOK[*object.Array](bt); ok {
			lines := make([]string, len(arr.Elems))
			for i, e := range arr.Elems {
				lines[i] = e.ToS()
			}
			exc.Backtrace = lines
		}
	}
	return exc, true
}

// classLE reports whether c is a or descends from anc (c <= anc) using the VM's
// ancestor chain.
func (vm *VM) classLE(c, anc *RClass) bool {
	for _, k := range vm.ancestors(c) {
		if k == anc {
			return true
		}
	}
	return false
}

// isNilV reports whether v is absent — a Go nil interface (an omitted argument) or
// the Ruby nil value. It delegates to object.IsNil, the shared nil seam.
func isNilV(v object.Value) bool {
	return object.IsNil(v)
}

// registerLogger installs the Logger class (require "logger") backed by
// go-ruby-logger: the constructor with its keyword options, the add/log core and
// the per-severity helpers and predicates, the level / progname / formatter /
// datetime_format accessors, <<, close, the Logger::Severity constant module, and
// the Logger::Formatter value class. It runs after the prelude so Logger::Error and
// friends can subclass the exception hierarchy (Logger::Error < RuntimeError).
func (vm *VM) registerLogger() {
	cls := newClass("Logger", vm.cObject)
	vm.cLogger = cls
	vm.consts["Logger"] = object.Wrap(cls)

	vm.registerLoggerSeverity(cls)
	vm.registerLoggerErrors(cls)
	vm.registerLoggerFormatterClass(cls)

	// Logger.new(logdev, shift_age = 0, shift_size = …, level:, progname:,
	// formatter:, datetime_format:)
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			var dev object.Value
			pos := args
			kw, _ := trailingHash(args)
			if kw != nil {
				pos = args[:len(args)-1]
			}
			if len(pos) > 0 {
				dev = pos[0]
			}
			lo := vm.newLogger(dev)
			if len(pos) > 1 {
				lo.applyShiftAge(pos[1])
			}
			if len(pos) > 2 {
				if n, ok := object.AsIntegerOK(pos[2]); ok {
					lo.shiftSize = int64(n)
				}
			}
			lo.applyKwargs(kw)
			return object.Wrap(lo)
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *Logger { return object.Kind[*Logger](v) }

	// add / log: the full Logger#add with the message/progname/block juggling.
	addFn := func(_ *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		lo := self(v)
		sev := lg.UNKNOWN
		if len(args) > 0 {
			if _, isNil := object.AsNilOK(args[0]); !isNil {
				s, err := lg.CoerceSeverity(severityArg(args[0]))
				if err != nil {
					raise("ArgumentError", "%s", err.Error())
				}
				sev = s
			}
		}
		var msg, prog object.Value
		if len(args) > 1 {
			msg = args[1]
		}
		if len(args) > 2 {
			prog = args[2]
		}
		return lo.add(sev, msg, prog, blk)
	}
	d("add", addFn)
	d("log", addFn)

	// The per-severity helpers: sev(progname = nil) { message }. With a block the
	// positional arg is the progname; without one it is the message (#add juggles).
	sevHelper := func(sev lg.Severity) NativeFn {
		return func(_ *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
			lo := self(v)
			var arg object.Value
			if len(args) > 0 {
				arg = args[0]
			}
			if blk != nil {
				return lo.add(sev, object.NilVal(), arg, blk)
			}
			return lo.add(sev, arg, object.NilVal(), nil)
		}
	}
	d("debug", sevHelper(lg.DEBUG))
	d("info", sevHelper(lg.INFO))
	d("warn", sevHelper(lg.WARN))
	d("error", sevHelper(lg.ERROR))
	d("fatal", sevHelper(lg.FATAL))
	d("unknown", sevHelper(lg.UNKNOWN))

	// The predicates: would a message at that severity currently be emitted?
	d("debug?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).l.DebugQ())))
	})
	d("info?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).l.InfoQ())))
	})
	d("warn?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).l.WarnQ())))
	})
	d("error?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).l.ErrorQ())))
	})
	d("fatal?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).l.FatalQ())))
	})

	// <<: a raw write with no formatting. Returns the byte count, or nil with no
	// device (the library returns -1, which the host maps to nil).
	d("<<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		lo := self(v)
		n := lo.l.Write(strArg(args[0]))
		if n < 0 {
			return object.NilVal()
		}
		return object.IntValue(int64(n))
	})

	// level / level= / sev_threshold= : the gating threshold, coercing a String /
	// Symbol / Integer like MRI's Severity.coerce.
	d("level", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).l.Level))
	})
	setLevel := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		lo := self(v)
		if err := lo.l.SetLevel(severityArg(args[0])); err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return args[0]
	}
	d("level=", setLevel)
	d("sev_threshold=", setLevel)

	// progname / progname= : the default program name.
	d("progname", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		lo := self(v)
		if lo.progDefault == "" {
			return object.NilVal()
		}
		return object.Wrap(object.NewString(lo.progDefault))
	})
	d("progname=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		lo := self(v)
		if _, isNil := object.AsNilOK(args[0]); isNil {
			lo.progDefault, lo.l.Progname = "", ""
		} else {
			lo.progDefault = strArg(args[0])
			lo.l.Progname = lo.progDefault
		}
		return args[0]
	})

	// datetime_format / datetime_format= : the strftime pattern for the timestamp.
	// Unset reads as nil (MRI), and setting nil restores the default pattern.
	d("datetime_format", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		lo := self(v)
		if lo.l.Formatter == nil || lo.l.Formatter.DatetimeFormat == "" {
			return object.NilVal()
		}
		return object.Wrap(object.NewString(lo.l.Formatter.DatetimeFormat))
	})
	d("datetime_format=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		lo := self(v)
		if _, isNil := object.AsNilOK(args[0]); isNil {
			lo.l.Formatter.DatetimeFormat = ""
		} else {
			lo.l.Formatter.DatetimeFormat = strArg(args[0])
		}
		return args[0]
	})

	// formatter / formatter= : a custom Proc formatter (nil restores the default).
	d("formatter", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		lo := self(v)
		if lo.formatter == nil {
			return object.NilVal()
		}
		return object.Wrap(lo.formatter)
	})
	d("formatter=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		lo := self(v)
		if p, ok := object.KindOK[*Proc](args[0]); ok {
			lo.formatter = p
		} else {
			lo.formatter = nil
		}
		return args[0]
	})

	// close: flush + mark the device closed. The library has no IO, so the host
	// drops its sink; subsequent writes are no-ops (MRI returns nil).
	d("close", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		lo := self(v)
		lo.closed = true
		lo.l.Sink = nil
		return object.NilVal()
	})
	d("reopen", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		lo := self(v)
		if len(args) > 0 {
			lo.bindDevice(args[0])
		}
		lo.closed = false
		return v
	})
}

// applyKwargs applies the Logger.new keyword options (level / progname / formatter
// / datetime_format) from the trailing options Hash.
func (lo *Logger) applyKwargs(kw *object.Hash) {
	if kw == nil {
		return
	}
	if v, ok := kw.Get(object.SymVal(string(object.Symbol("level")))); ok {
		if err := lo.l.SetLevel(severityArg(v)); err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
	}
	if v, ok := kw.Get(object.SymVal(string(object.Symbol("progname")))); ok {
		if _, isNil := object.AsNilOK(v); !isNil {
			lo.progDefault = v.ToS()
			lo.l.Progname = lo.progDefault
		}
	}
	if v, ok := kw.Get(object.SymVal(string(object.Symbol("datetime_format")))); ok {
		if _, isNil := object.AsNilOK(v); !isNil {
			lo.l.Formatter.DatetimeFormat = v.ToS()
		}
	}
	if v, ok := kw.Get(object.SymVal(string(object.Symbol("formatter")))); ok {
		if p, ok := object.KindOK[*Proc](v); ok {
			lo.formatter = p
		}
	}
}

// severityArg marshals a level argument to the form CoerceSeverity accepts: an
// Integer passes through, a String / Symbol passes its name (case-insensitive).
func severityArg(v object.Value) any {
	{
		__sw92 := v
		switch {
		case object.IsInt(__sw92):
			x := object.AsInteger(__sw92)
			_ = x
			return int(x)
		case object.IsKind[*object.String](__sw92):
			x := object.Kind[*object.String](__sw92)
			_ = x
			return x.Str()
		case object.IsKind[object.Symbol](__sw92):
			x := object.Kind[object.Symbol](__sw92)
			_ = x
			return string(x)
		}
	}
	return v
}

// registerLoggerSeverity installs Logger::Severity (the severity-constant module)
// and lifts its constants onto Logger itself (Logger::DEBUG etc.), via the CSV::Row
// nested-constant pattern.
func (vm *VM) registerLoggerSeverity(cls *RClass) {
	sev := newClass("Logger::Severity", vm.cObject)
	sev.isModule = true
	cls.consts["Severity"] = object.Wrap(sev)
	vm.consts["Logger::Severity"] = object.Wrap(sev)
	vm.cLoggerSeverity = sev

	for name, val := range map[string]lg.Severity{
		"DEBUG": lg.DEBUG, "INFO": lg.INFO, "WARN": lg.WARN,
		"ERROR": lg.ERROR, "FATAL": lg.FATAL, "UNKNOWN": lg.UNKNOWN,
	} {
		sev.consts[name] = object.IntValue(int64(val))
		cls.consts[name] = object.IntValue(int64(val))
	}
}

// registerLoggerErrors installs Logger::Error and Logger::ShiftingError, matching
// MRI's hierarchy (Logger::Error < RuntimeError, ShiftingError < Logger::Error).
func (vm *VM) registerLoggerErrors(cls *RClass) {
	runtimeErr, _ := object.KindOK[*RClass](vm.consts["RuntimeError"])
	if runtimeErr == nil {
		runtimeErr = vm.cException
	}
	err := newClass("Logger::Error", runtimeErr)
	cls.consts["Error"] = object.Wrap(err)
	vm.consts["Logger::Error"] = object.Wrap(err)

	shift := newClass("Logger::ShiftingError", err)
	cls.consts["ShiftingError"] = object.Wrap(shift)
	vm.consts["Logger::ShiftingError"] = object.Wrap(shift)
}

// registerLoggerFormatterClass installs Logger::Formatter with its #call (rendering
// one line through the library) and the datetime_format accessor.
func (vm *VM) registerLoggerFormatterClass(cls *RClass) {
	fc := newClass("Logger::Formatter", vm.cObject)
	cls.consts["Formatter"] = object.Wrap(fc)
	vm.consts["Logger::Formatter"] = object.Wrap(fc)
	vm.cLoggerFormatter = fc

	fc.smethods["new"] = &Method{name: "new", owner: fc,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			lf := &LoggerFormatter{vm: vm}
			lf.f = &lg.Formatter{}
			lf.f.Inspect = func(msg any) string {
				if v, ok := msg.(object.Value); ok {
					return vm.send(v, "inspect", nil, nil).ToS()
				}
				return ""
			}
			return object.Wrap(lf)
		}}

	self := func(v object.Value) *LoggerFormatter { return object.Kind[*LoggerFormatter](v) }

	// call(severity_label, time, progname, msg) → the formatted line. The host
	// unwraps the Ruby Time to a Go time and projects an exception / String / other
	// message onto the library's msg2str input.
	fc.define("call", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		lf := self(v)
		label := args[0].ToS()
		t := loggerTimeOf(args[1])
		prog := ""
		if _, isNil := object.AsNilOK(args[2]); !isNil {
			prog = args[2].ToS()
		}
		msg := loggerMsg(vm, args[3])
		return object.Wrap(object.NewString(lf.f.Format(label, t, os.Getpid(), prog, msg)))
	})
	fc.define("datetime_format", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		lf := self(v)
		if lf.f.DatetimeFormat == "" {
			return object.NilVal()
		}
		return object.Wrap(object.NewString(lf.f.DatetimeFormat))
	})
	fc.define("datetime_format=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		lf := self(v)
		if _, isNil := object.AsNilOK(args[0]); isNil {
			lf.f.DatetimeFormat = ""
		} else {
			lf.f.DatetimeFormat = strArg(args[0])
		}
		return args[0]
	})
}

// loggerMsg projects a Ruby message onto the library's msg2str input (String /
// Exception / other), shared by Logger::Formatter#call.
func loggerMsg(vm *VM, v object.Value) any {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	lo := &Logger{vm: vm}
	if exc, ok := lo.asException(v); ok {
		return exc
	}
	return v
}

// loggerTimeOf unwraps a Ruby Time to a Go time.Time; a non-Time falls back to the
// current instant (the formatter is timestamp-tolerant).
func loggerTimeOf(v object.Value) time.Time {
	if t, ok := object.KindOK[*Time](v); ok {
		return time.Unix(t.t.ToUnix(), 0)
	}
	return time.Now()
}
