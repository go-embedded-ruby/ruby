// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math/big"
	stdtime "time"

	date "github.com/go-ruby-date/date"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and github.com/go-ruby-date/date — an MRI-4.0.5-byte-exact Date / DateTime, a
// sibling of go-ruby-json / go-ruby-yaml / go-ruby-bigdecimal. The whole
// calendar lives in that library: the astronomical Julian-Day-Number core, the
// Julian->Gregorian reform (Date::ITALY), the full MRI strftime, parse /
// strptime, the week-date and ordinal constructors and the day / month / year
// arithmetic. rbgo only wraps a *date.Date in its Ruby Date object and re-raises
// the library's ErrInvalidDate as MRI's Date::Error.
//
// This replaces rbgo's former internal/vm/date.go — a thin shim over
// go-composites/date that was proleptic-Gregorian-only (no 1582 reform), had no
// strftime and rendered #inspect as the bare ISO string rather than MRI's
// "#<Date: ...>" form. The binding is therefore the MRI-faithful upgrade: a Ruby
// DateTime (a subclass of Date in MRI) is modelled over the very same *date.Date,
// distinguished by its IsDateTime flag, so the two share every accessor and only
// diverge where MRI does (to_s / inspect / the time-of-day fields).

// Date is the Ruby wrapper around a *date.Date. The same wrapper backs both Ruby
// Date and DateTime; classOf dispatches on d.IsDateTime() so a DateTime value
// reports the DateTime class (a subclass of Date) and renders its wall clock.
type Date struct {
	d *date.Date
}

func (d *Date) ToS() string     { return d.d.String() }
func (d *Date) Inspect() string { return dateInspect(d.d) }
func (d *Date) Truthy() bool    { return true }

// dateInspect renders MRI's Date#inspect / DateTime#inspect — the
// "#<Date: ISO ((JDj,Ss,Nn),+OFFs,STARTj)>" form. The bracketed triple is the
// value at UTC: the astronomical day count, the whole seconds of that UTC day
// and the leftover nanoseconds, then the offset (seconds east of UTC) and the
// reform sentinel (always Date::ITALY here, as every construction defaults to
// it). It is built from the library's public accessors so it stays a thin shell.
func dateInspect(d *date.Date) string {
	name := "Date"
	if d.IsDateTime() {
		name = "DateTime"
	}
	// Local time-of-day nanoseconds (NsecOfDay) shifted to UTC by the offset; the
	// borrow carries into the UTC day count (Jd is the local civil day).
	ns := d.NsecOfDay() - int64(d.Offset())*int64(1e9)
	jd := d.Jd()
	const nsPerDay = 86_400 * int64(1e9)
	for ns < 0 {
		ns += nsPerDay
		jd--
	}
	for ns >= nsPerDay {
		ns -= nsPerDay
		jd++
	}
	sec := ns / int64(1e9)
	frac := ns % int64(1e9)
	// MRI renders the offset with a leading "+" only when non-negative (a negative
	// offset already carries its own "-" via itoa, e.g. "-7200s").
	offSign := "+"
	if d.Offset() < 0 {
		offSign = ""
	}
	return "#<" + name + ": " + d.String() + " ((" +
		itoa(jd) + "j," + i64toa(sec) + "s," + i64toa(frac) + "n)," +
		offSign + itoa(d.Offset()) + "s," + itoa(date.ITALY) + "j)>"
}

// i64toa renders a signed int64 decimal (the nanosecond / seconds fields, which
// exceed int range) — the int64 companion of the package's itoa.
func i64toa(n int64) string { return object.IntValue(n).ToS() }

// dateArg asserts an argument is a Date, raising TypeError otherwise (the Date
// counterpart of timeArg / setArg).
func dateArg(v object.Value) *Date {
	d, ok := object.KindOK[*Date](v)
	if !ok {
		raise("TypeError", "value must be a Date")
	}
	return d
}

// dateDays marshals a Ruby Integer to a whole number of days / months, raising
// TypeError for anything else (only an Integer is a meaningful whole count, so a
// Float is rejected) — the day/month-offset counterpart of intArg.
func dateDays(v object.Value) int {
	if n, ok := object.AsIntegerOK(v); ok {
		return int(n)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
	return 0
}

// payloadDate wraps a library constructor result, re-raising ErrInvalidDate as a
// Ruby Date::Error (MRI's own class for an out-of-range / invalid calendar date)
// and any other error as ArgumentError — mirroring the json / bigdecimal
// bindings re-raising a typed library error.
func payloadDate(d *date.Date, err error) *Date {
	if err != nil {
		if errors.Is(err, date.ErrInvalidDate) {
			raise("Date::Error", "invalid date")
		}
		raise("ArgumentError", "%s", err.Error())
	}
	return &Date{d: d}
}

// dateOp implements the Date operator fast path reached from binary(): d + n
// shifts forward by n days, d - n shifts back, and d - other yields the whole
// number of days between the two dates (an Integer; MRI's Date#- is a Rational we
// render as the plain day count — the library's Diff resolution). A non-Date,
// non-Integer right operand raises TypeError via dateDays.
func dateOp(op bytecode.Op, a *Date, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		return object.Wrap(&Date{d: a.d.Plus(dateDays(b))})
	case bytecode.OpSub:
		if other, ok := object.KindOK[*Date](b); ok {
			return object.IntValue(int64(a.d.Diff(other.d)))
		}
		return object.Wrap(&Date{d: a.d.Minus(dateDays(b))})
	}
	return raise("NoMethodError", "undefined method '%s' for a Date", op)
}

// dateCmp returns -1/0/1 ordering two Dates through the library's Cmp.
func dateCmp(a, b *Date) int64 { return int64(a.d.Cmp(b.d)) }

// dateEqual reports Date equality for valueEqual / the == operator fast path.
func dateEqual(a *Date, other object.Value) bool {
	b, ok := object.KindOK[*Date](other)
	return ok && a.d.Equal(b.d)
}

// offsetSeconds coerces a DateTime offset argument to seconds east of UTC: a
// String time-zone ("+02:00", "-0530", "Z", "UTC", "+09") via the library's own
// zone parser, or a numeric (Integer / Float / Rational) read as a fraction of a
// day, matching MRI's DateTime.new(..., offset) forms. An unparsable string
// raises Date::Error.
func offsetSeconds(v object.Value) int {
	{
		__sw45 := v
		switch {
		case object.IsKind[*object.String](__sw45):
			x := object.Kind[*object.String](__sw45)
			_ = x
			secs, ok := zoneOffsetSeconds(x.Str())
			if !ok {
				raise("Date::Error", "invalid date")
			}
			return secs
		case object.IsInt(__sw45):
			x := object.AsInteger(__sw45)
			_ = x
			return int(int64(x) * 86400)
		case object.IsFloat(__sw45):
			x := object.AsFloatV(__sw45)
			_ = x
			return int(float64(x) * 86400)
		case object.IsKind[*object.Rational](__sw45):
			x := object.Kind[*object.Rational](__sw45)
			_ = x
			f, _ := x.R.Float64()
			return int(f * 86400)
		}
	}
	raise("TypeError", "invalid offset: %s", v.Inspect())
	return 0
}

// offsetRational renders a seconds-east-of-UTC offset as MRI's Rational
// fraction-of-a-day (DateTime#offset), reduced (7200s -> 1/12).
func offsetRational(secs int) object.Value {
	return object.Wrap(&object.Rational{R: new(big.Rat).SetFrac64(int64(secs), 86400)})
}

// zoneOffsetSeconds parses a Ruby DateTime zone string to seconds east of UTC,
// accepting MRI's forms: "Z" / "UTC" / "GMT" (0), and a signed "+HH", "+HHMM" or
// "+HH:MM". ok is false for anything else.
func zoneOffsetSeconds(s string) (int, bool) {
	switch s {
	case "Z", "UTC", "GMT", "+00:00", "-00:00":
		return 0, true
	}
	if len(s) < 3 || (s[0] != '+' && s[0] != '-') {
		return 0, false
	}
	sign := 1
	if s[0] == '-' {
		sign = -1
	}
	body := s[1:]
	// Strip the optional ":" between hours and minutes.
	if len(body) == 5 && body[2] == ':' {
		body = body[:2] + body[3:]
	}
	if len(body) != 2 && len(body) != 4 {
		return 0, false
	}
	hh, ok := twoDigits(body[:2])
	if !ok || hh > 23 {
		return 0, false
	}
	mm := 0
	if len(body) == 4 {
		if mm, ok = twoDigits(body[2:]); !ok || mm > 59 {
			return 0, false
		}
	}
	return sign * (hh*3600 + mm*60), true
}

// twoDigits parses a 2-character decimal field, returning ok=false on any
// non-digit — the building block of zoneOffsetSeconds.
func twoDigits(s string) (int, bool) {
	if len(s) != 2 || s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' {
		return 0, false
	}
	return int(s[0]-'0')*10 + int(s[1]-'0'), true
}

// withDeterministicClock installs the Ruby VM's pinned wall clock (nowUnix, the
// seam Time.now already uses) into the library's Today / Now clock for the
// duration of fn, so Date.today / DateTime.now stay deterministic under the same
// test seam. fn's result is returned; the library clock is always restored.
func withDeterministicClock(fn func() *Date) *Date {
	t := stdtime.Unix(nowUnix(), 0).UTC()
	restore := date.SetTodayInstant(t.Year(), int(t.Month()), t.Day(),
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond())
	defer restore()
	return fn()
}

// registerDate installs the Date and DateTime classes, their class constructors
// and instance methods, all delegating to the go-ruby-date library.
func (vm *VM) registerDate() {
	vm.cDate = newClass("Date", vm.cObject)
	vm.consts["Date"] = object.Wrap(vm.cDate)
	// DateTime is a subclass of Date (as in MRI), so it inherits every instance
	// method defined on Date below; only the constructors differ. The shared
	// *date.Date wrapper carries the IsDateTime flag that classOf reads.
	vm.cDateTime = newClass("DateTime", vm.cDate)
	vm.consts["DateTime"] = object.Wrap(vm.cDateTime)

	vm.registerDateConstructors()
	vm.registerDateAccessors()
	vm.registerDateArithmetic()
	vm.registerDateFormat()
	vm.registerDateCompare()
}

// registerDateErrors installs Date::Error < ArgumentError — MRI's class for an
// invalid calendar date or a failed parse. It is registered both as a nested
// constant of Date (so Ruby `Date::Error` resolves it) and under its qualified
// top-level name (so raise's exceptionObject lookup finds the same class),
// exactly as the JSON / Errno error classes are. It runs after the exception
// hierarchy is in place (registerDate itself runs before StandardError exists).
func (vm *VM) registerDateErrors() {
	dateErr := newClass("Date::Error", object.Kind[*RClass](vm.consts["ArgumentError"]))
	vm.cDate.consts["Error"] = object.Wrap(dateErr)
	vm.consts["Date::Error"] = object.Wrap(dateErr)
}

// registerDateConstructors installs the Date / DateTime class methods.
func (vm *VM) registerDateConstructors() {
	sm := func(cls *RClass, name string, fn NativeFn) {
		cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
	}

	// Date.new(y, m, d) / Date.civil(y, m, d) — the default ITALY reform. An
	// invalid calendar date (Feb 30, month 13, a 1582 reform-gap day) raises
	// Date::Error. Omitted arguments default to the MRI epoch -4712-01-01 (jd 0),
	// matching Date.new with no arguments.
	newFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		y, m, d := -4712, 1, 1
		if len(args) > 0 {
			y = int(intArg(args[0]))
		}
		if len(args) > 1 {
			m = int(intArg(args[1]))
		}
		if len(args) > 2 {
			d = int(intArg(args[2]))
		}
		return object.Wrap(payloadDate(date.NewDate(y, m, d)))
	}
	sm(vm.cDate, "new", newFn)
	sm(vm.cDate, "civil", newFn)

	// Date.jd(n) — the Date for astronomical Julian Day Number n.
	sm(vm.cDate, "jd", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(&Date{d: date.DateJD(int(intArgOr(args, 0)))})
	})
	// Date.ordinal(y, yday) — the yday-th day of year y (1-based); an out-of-range
	// yday raises Date::Error.
	sm(vm.cDate, "ordinal", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		y := -4712
		if len(args) > 0 {
			y = int(intArg(args[0]))
		}
		return object.Wrap(payloadDate(date.Ordinal(y, int(intArgOr(args[1:], 1)))))
	})
	// Date.commercial(cwyear, cweek, cwday) — the ISO week-date constructor.
	sm(vm.cDate, "commercial", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		cwy, cw, cwd := -4712, 1, 1
		if len(args) > 0 {
			cwy = int(intArg(args[0]))
		}
		if len(args) > 1 {
			cw = int(intArg(args[1]))
		}
		if len(args) > 2 {
			cwd = int(intArg(args[2]))
		}
		return object.Wrap(payloadDate(date.Commercial(cwy, cw, cwd)))
	})
	// Date.today — the current local date, driven by the VM's pinned clock.
	sm(vm.cDate, "today", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return withDeterministicClock(func() *Date { return &Date{d: date.Today()} }).toDate()
	})
	// Date.parse(str[, comp]) — MRI's heuristic parser; an unrecognised input
	// raises Date::Error. comp (default true) expands a 2-digit year.
	sm(vm.cDate, "parse", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return payloadDate(date.Parse(strArg(args[0]), compArg(args, 1))).toDate()
	})
	// Date.strptime(str, fmt) — parse against an explicit format; a non-match
	// raises Date::Error.
	sm(vm.cDate, "strptime", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return payloadDate(date.Strptime(strArg(args[0]), strptimeFormat(args))).toDate()
	})
	// Date._strptime(str, fmt) — the lower-level form returning a Hash of the
	// parsed fields (year / mon / mday, plus the time-of-day and offset for a
	// datetime format) rather than a Date; a non-match returns nil (as MRI does).
	sm(vm.cDate, "_strptime", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		d, err := date.Strptime(strArg(args[0]), strptimeFormat(args))
		if err != nil {
			return object.NilVal()
		}
		return strptimeHash(d)
	})

	// DateTime.new(y, m, d, h, min, s, offset) — the calendar date plus a wall
	// clock; trailing arguments default (h/min/s to 0, offset to UTC). An invalid
	// field raises Date::Error.
	sm(vm.cDateTime, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		y, m, d, h, mi, s, off := -4712, 1, 1, 0, 0, 0, 0
		if len(args) > 0 {
			y = int(intArg(args[0]))
		}
		if len(args) > 1 {
			m = int(intArg(args[1]))
		}
		if len(args) > 2 {
			d = int(intArg(args[2]))
		}
		if len(args) > 3 {
			h = int(intArg(args[3]))
		}
		if len(args) > 4 {
			mi = int(intArg(args[4]))
		}
		if len(args) > 5 {
			s = int(intArg(args[5]))
		}
		if len(args) > 6 {
			off = offsetSeconds(args[6])
		}
		return object.Wrap(payloadDate(date.NewDateTime(y, m, d, h, mi, s, off)))
	})
	sm(vm.cDateTime, "civil", vm.cDateTime.smethods["new"].native)
	// DateTime.now — the current instant, driven by the VM's pinned clock.
	sm(vm.cDateTime, "now", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(withDeterministicClock(func() *Date { return &Date{d: date.Now()} }))
	})
	// DateTime.parse(str[, comp]) — the heuristic parser, returning a DateTime.
	sm(vm.cDateTime, "parse", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(payloadDate(date.ParseDateTime(strArg(args[0]), compArg(args, 1))))
	})
	sm(vm.cDateTime, "strptime", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(payloadDate(date.Strptime(strArg(args[0]), strptimeFormat(args))))
	})
}

// toDate downgrades a value the library may have produced as a DateTime (Parse /
// Strptime / Today all build through the datetime core) to a plain Date when the
// receiver was the Date class — so Date.parse returns a Date, DateTime.parse a
// DateTime. The library's ToDate strips the time-of-day.
func (d *Date) toDate() object.Value { return object.Wrap(&Date{d: d.d.ToDate()}) }

// compArg reads the optional `comp` boolean (Date.parse's second argument),
// defaulting to true (MRI's default — expand a 2-digit year).
func compArg(args []object.Value, i int) bool {
	if len(args) > i {
		return args[i].Truthy()
	}
	return true
}

// strptimeFormat reads the format argument of strptime / _strptime, defaulting to
// "%F" (the ISO date) as MRI does.
func strptimeFormat(args []object.Value) string {
	if len(args) > 1 {
		return strArg(args[1])
	}
	return "%F"
}

// strptimeHash builds the Date._strptime result Hash from a parsed value: the
// calendar fields (year / mon / mday) always, plus the time-of-day (hour / min /
// sec) when the format carried a time directive — which the library signals by
// resolving to a DateTime. (MRI keys the hash by the directives present; rbgo
// keys it by the resolved value, which agrees for a date-only or a
// date-plus-time format; the zone / offset keys an explicit %z would add are not
// reconstructed from the resolved value.)
func strptimeHash(d *date.Date) object.Value {
	h := object.NewHash()
	h.Set(object.SymVal(string(object.Symbol("year"))), object.IntValue(int64(d.Year())))
	h.Set(object.SymVal(string(object.Symbol("mon"))), object.IntValue(int64(d.Month())))
	h.Set(object.SymVal(string(object.Symbol("mday"))), object.IntValue(int64(d.Day())))
	if d.IsDateTime() {
		h.Set(object.SymVal(string(object.Symbol("hour"))), object.IntValue(int64(d.Hour())))
		h.Set(object.SymVal(string(object.Symbol("min"))), object.IntValue(int64(d.Min())))
		h.Set(object.SymVal(string(object.Symbol("sec"))), object.IntValue(int64(d.Sec())))
	}
	return object.Wrap(h)
}

// registerDateAccessors installs the field-accessor instance methods, shared by
// Date and DateTime (DateTime inherits them).
func (vm *VM) registerDateAccessors() {
	d := func(name string, fn NativeFn) { vm.cDate.define(name, fn) }
	self := func(v object.Value) *date.Date { return object.Kind[*Date](v).d }
	intM := func(get func(*date.Date) int) NativeFn {
		return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.IntValue(int64(get(self(v))))
		}
	}

	d("year", intM((*date.Date).Year))
	monthFn := intM((*date.Date).Month)
	d("month", monthFn)
	d("mon", monthFn)
	dayFn := intM((*date.Date).Day)
	d("day", dayFn)
	d("mday", dayFn)
	d("wday", intM((*date.Date).Wday))
	d("yday", intM((*date.Date).Yday))
	d("cwday", intM((*date.Date).Cwday))
	d("cweek", intM((*date.Date).Cweek))
	d("cwyear", intM((*date.Date).Cwyear))
	d("jd", intM((*date.Date).Jd))
	d("mjd", intM((*date.Date).Mjd))
	// DateTime wall-clock fields (a plain Date reports them at midnight UTC, as MRI
	// does for a Date promoted to a DateTime context).
	d("hour", intM((*date.Date).Hour))
	d("min", intM((*date.Date).Min))
	d("sec", intM((*date.Date).Sec))

	d("leap?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).Leap())))
	})
	// offset — MRI's DateTime#offset, the Rational fraction of a day east of UTC.
	d("offset", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return offsetRational(self(v).Offset())
	})
	// to_date — the plain calendar date (strips the time-of-day); to_datetime
	// promotes to a DateTime at midnight UTC.
	d("to_date", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&Date{d: self(v).ToDate()})
	})
}

// registerDateArithmetic installs the +/- operators, the day/month/year shifts
// and the iteration methods (step / upto / downto).
func (vm *VM) registerDateArithmetic() {
	d := func(name string, fn NativeFn) { vm.cDate.define(name, fn) }
	self := func(v object.Value) *Date { return object.Kind[*Date](v) }

	// + / - mirror the operator fast path (dateOp) so `send(:+, n)` agrees with the
	// `d + n` syntax.
	d("+", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return dateOp(bytecode.OpAdd, self(v), args[0])
	})
	d("-", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return dateOp(bytecode.OpSub, self(v), args[0])
	})

	// next / succ — the following day (Date#succ is the MRI alias for next).
	succFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&Date{d: self(v).d.Succ()})
	}
	d("next", succFn)
	d("succ", succFn)

	// next_day / prev_day / next_month / prev_month / next_year / prev_year, each
	// taking an optional count (default 1).
	shift := func(fn func(*date.Date, int) *date.Date) NativeFn {
		return func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			n := 1
			if len(args) > 0 {
				n = dateDays(args[0])
			}
			return object.Wrap(&Date{d: fn(self(v).d, n)})
		}
	}
	d("next_day", shift((*date.Date).NextDay))
	d("prev_day", shift((*date.Date).PrevDay))
	d("next_month", shift((*date.Date).NextMonth))
	d("prev_month", shift((*date.Date).PrevMonth))
	d("next_year", shift((*date.Date).NextYear))
	d("prev_year", shift((*date.Date).PrevYear))

	// >> n / << n — MRI's month-shift operators (dispatch as method sends; the VM
	// has no shift opcode). The right operand is an Integer month count.
	d(">>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(&Date{d: self(v).d.PlusMonths(dateDays(args[0]))})
	})
	d("<<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(&Date{d: self(v).d.PlusMonths(-dateDays(args[0]))})
	})

	// upto / downto / step — block iterators over the day range to a limit (a
	// Date); step's stride is a whole number of days. With no block they return an
	// Enumerator (enumFor), matching the rest of rbgo's iterators.
	iter := func(name string, run func(vm *VM, d *date.Date, limit *date.Date, blk *Proc)) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
			if blk == nil {
				return object.Wrap(enumFor(v, name, args...))
			}
			run(vm, self(v).d, dateArg(args[0]).d, blk)
			return v
		}
	}
	d("upto", iter("upto", func(vm *VM, dd, limit *date.Date, blk *Proc) {
		dd.Upto(limit, func(x *date.Date) { vm.callBlock(blk, []object.Value{object.Wrap(&Date{d: x})}) })
	}))
	d("downto", iter("downto", func(vm *VM, dd, limit *date.Date, blk *Proc) {
		dd.Downto(limit, func(x *date.Date) { vm.callBlock(blk, []object.Value{object.Wrap(&Date{d: x})}) })
	}))
	d("step", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return object.Wrap(enumFor(v, "step", args...))
		}
		step := 1
		if len(args) > 1 {
			step = dateDays(args[1])
		}
		self(v).d.Step(dateArg(args[0]).d, step,
			func(x *date.Date) { vm.callBlock(blk, []object.Value{object.Wrap(&Date{d: x})}) })
		return v
	})
}

// registerDateFormat installs strftime and the named string formats.
func (vm *VM) registerDateFormat() {
	d := func(name string, fn NativeFn) { vm.cDate.define(name, fn) }
	self := func(v object.Value) *date.Date { return object.Kind[*Date](v).d }
	strM := func(get func(*date.Date) string) NativeFn {
		return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Wrap(object.NewString(get(self(v))))
		}
	}

	d("strftime", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).Strftime(strArg(args[0]))))
	})
	d("iso8601", strM((*date.Date).Iso8601))
	d("rfc3339", strM((*date.Date).Rfc3339))
	d("rfc2822", strM((*date.Date).Rfc2822))
	d("rfc822", strM((*date.Date).Rfc2822))
	d("httpdate", strM((*date.Date).Httpdate))
	d("ctime", strM((*date.Date).Ctime))
	d("asctime", strM((*date.Date).Asctime))
	d("jisx0301", strM((*date.Date).Jisx0301))
	d("to_s", strM((*date.Date).String))
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(dateInspect(self(v))))
	})
}

// registerDateCompare installs <=> and the boolean comparison / equality
// operators.
func (vm *VM) registerDateCompare() {
	d := func(name string, fn NativeFn) { vm.cDate.define(name, fn) }
	self := func(v object.Value) *Date { return object.Kind[*Date](v) }

	d("<=>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := object.KindOK[*Date](args[0])
		if !ok {
			return object.NilVal()
		}
		return object.IntValue(dateCmp(self(v), other))
	})
	cmpBool := func(want func(int64) bool) NativeFn {
		return func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			return object.BoolValue(bool(object.Bool(want(dateCmp(self(v), dateArg(args[0]))))))
		}
	}
	d("<", cmpBool(func(c int64) bool { return c < 0 }))
	d(">", cmpBool(func(c int64) bool { return c > 0 }))
	d("<=", cmpBool(func(c int64) bool { return c <= 0 }))
	d(">=", cmpBool(func(c int64) bool { return c >= 0 }))
	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(dateEqual(self(v), args[0]))))
	})
}
